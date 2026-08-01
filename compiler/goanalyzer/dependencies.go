package goanalyzer

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"github.com/zhuyanxi/axiom-insight/compiler/semantic"
)

// DependencyRule recognizes one external dependency from typed call data.
type DependencyRule interface {
	Name() string
	MatchDependency(DependencyContext) ([]semantic.Dependency, []semantic.Diagnostic, bool)
}

// DependencyContext contains typed call information for a dependency rule.
type DependencyContext struct {
	Caller         semantic.Function
	Call           *ast.CallExpr
	Callee         *types.Func
	Selection      *types.Selection
	Info           *types.Info
	FileSet        *token.FileSet
	SourceLocation semantic.SourceLocation
	Receiver       ast.Expr
	TypeOf         func(ast.Expr) types.Type
	StaticString   func(ast.Expr) (string, bool)
}

// DefaultDependencyRules returns built-in SQL, Redis, and Kafka recognizers.
func DefaultDependencyRules() []DependencyRule {
	return []DependencyRule{
		databaseSQLRule{},
		redisRule{},
		saramaKafkaRule{},
	}
}

func (analyzer *functionAnalyzer) collectDependencies(rules []DependencyRule) {
	if rules == nil {
		rules = DefaultDependencyRules()
	}
	seen := make(map[string]struct{})
	for _, record := range analyzer.records {
		if record.body == nil {
			continue
		}
		ast.Inspect(record.body, func(node ast.Node) bool {
			if _, ok := node.(*ast.FuncLit); ok {
				return false
			}
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			callee, selection := callTarget(call, record.info)
			if callee == nil {
				return true
			}
			context := DependencyContext{
				Caller:         record.value,
				Call:           call,
				Callee:         callee,
				Selection:      selection,
				Info:           record.info,
				FileSet:        record.fset,
				SourceLocation: nodeLocation(analyzer.root, record.fset, call),
				Receiver:       callReceiver(call),
				TypeOf: func(expression ast.Expr) types.Type {
					return typeOf(record.info, expression)
				},
				StaticString: func(expression ast.Expr) (string, bool) {
					return staticString(record.info, expression)
				},
			}
			for _, rule := range rules {
				dependencies, diagnostics, matched := rule.MatchDependency(context)
				if !matched {
					continue
				}
				for _, dependency := range dependencies {
					if dependency.FunctionID == "" {
						dependency.FunctionID = record.id
					}
					if dependency.SourceLocation.IsZero() {
						dependency.SourceLocation = context.SourceLocation
					}
					if dependency.ID == "" {
						dependency.ID = semantic.DependencyID(dependency)
					}
					if _, exists := seen[dependency.ID]; exists {
						continue
					}
					seen[dependency.ID] = struct{}{}
					analyzer.dependencies = append(analyzer.dependencies, dependency)
					if index, exists := analyzer.functionIndex[dependency.FunctionID]; exists {
						analyzer.functions[index].DependencyIDs = appendUnique(analyzer.functions[index].DependencyIDs, dependency.ID)
					}
				}
				analyzer.diagnostics = append(analyzer.diagnostics, diagnostics...)
				break
			}
			return true
		})
	}
}

func callReceiver(call *ast.CallExpr) ast.Expr {
	if call == nil {
		return nil
	}
	selector, ok := unwrapCallFunction(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return nil
	}
	return selector.X
}

func receiverIdentity(typ types.Type) (string, string) {
	if typ == nil {
		return "", ""
	}
	for {
		typ = types.Unalias(typ)
		switch current := typ.(type) {
		case *types.Pointer:
			typ = current.Elem()
		default:
			named, ok := typ.(*types.Named)
			if !ok || named.Obj() == nil || named.Obj().Pkg() == nil {
				return "", ""
			}
			return named.Obj().Name(), named.Obj().Pkg().Path()
		}
	}
}

func dependencyName(packagePath, receiver, method string) string {
	parts := []string{packagePath, receiver, method}
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			values = append(values, part)
		}
	}
	return strings.Join(values, ".")
}

func dependencyDiagnostic(code, message string, location semantic.SourceLocation) semantic.Diagnostic {
	return semantic.Diagnostic{
		Severity:       semantic.DiagnosticSeverityWarning,
		Code:           code,
		Message:        message,
		SourceLocation: location,
	}
}

type databaseSQLRule struct{}

func (databaseSQLRule) Name() string { return "database/sql" }

func (databaseSQLRule) MatchDependency(context DependencyContext) ([]semantic.Dependency, []semantic.Diagnostic, bool) {
	if context.Callee == nil || functionPackagePath(context.Callee) != "database/sql" || context.Selection == nil {
		return nil, nil, false
	}
	receiver, packagePath := receiverIdentity(context.Selection.Recv())
	if packagePath != "database/sql" || !isSQLReceiver(receiver) {
		return nil, nil, false
	}
	operation, argumentIndex, ok := sqlOperation(context.Callee.Name())
	if !ok {
		return nil, nil, false
	}
	dependency := semantic.Dependency{
		Kind:           semantic.DependencyKindSQL,
		Name:           dependencyName(packagePath, receiver, context.Callee.Name()),
		Operation:      operation,
		TargetPackage:  packagePath,
		Resource:       receiver,
		SourceLocation: context.SourceLocation,
	}
	if argumentIndex >= 0 {
		if len(context.Call.Args) <= argumentIndex {
			return []semantic.Dependency{dependency}, []semantic.Diagnostic{dependencyDiagnostic("MISSING_SQL_QUERY", "SQL call has no query argument", context.SourceLocation)}, true
		}
		value, static := context.StaticString(context.Call.Args[argumentIndex])
		dependency.Value = value
		dependency.ValueIsStatic = static
		if !static {
			return []semantic.Dependency{dependency}, []semantic.Diagnostic{dependencyDiagnostic("DYNAMIC_SQL", "SQL text is not a static string", context.SourceLocation)}, true
		}
	}
	return []semantic.Dependency{dependency}, nil, true
}

func isSQLReceiver(receiver string) bool {
	switch receiver {
	case "DB", "Tx", "Stmt", "Conn":
		return true
	default:
		return false
	}
}

func sqlOperation(method string) (string, int, bool) {
	switch method {
	case "Query", "QueryRow", "Exec", "Prepare":
		return strings.ToLower(method), 0, true
	case "QueryContext", "QueryRowContext", "ExecContext", "PrepareContext":
		return strings.ToLower(method), 1, true
	case "Begin", "BeginTx":
		return strings.ToLower(method), -1, true
	default:
		return "", -1, false
	}
}

type redisRule struct{}

func (redisRule) Name() string { return "redis" }

func (redisRule) MatchDependency(context DependencyContext) ([]semantic.Dependency, []semantic.Diagnostic, bool) {
	if context.Callee == nil || context.Selection == nil {
		return nil, nil, false
	}
	receiver, packagePath := receiverIdentity(context.Selection.Recv())
	if !isRedisReceiver(packagePath, receiver) || !redisMethod(context.Callee.Name()) {
		return nil, nil, false
	}
	dependency := semantic.Dependency{
		Kind:           semantic.DependencyKindRedis,
		Name:           dependencyName(packagePath, receiver, context.Callee.Name()),
		Operation:      strings.ToLower(context.Callee.Name()),
		TargetPackage:  packagePath,
		Resource:       "key",
		SourceLocation: context.SourceLocation,
	}
	key, static := redisKey(context)
	dependency.Value = key
	dependency.ValueIsStatic = static
	if !static {
		return []semantic.Dependency{dependency}, []semantic.Diagnostic{dependencyDiagnostic("DYNAMIC_REDIS_KEY", "Redis key is not a static string", context.SourceLocation)}, true
	}
	return []semantic.Dependency{dependency}, nil, true
}

func isRedisReceiver(packagePath, receiver string) bool {
	if packagePath != "github.com/redis/go-redis/v9" && packagePath != "github.com/go-redis/redis/v8" && packagePath != "github.com/go-redis/redis" {
		return false
	}
	switch receiver {
	case "Client", "Ring", "ClusterClient":
		return true
	default:
		return false
	}
}

func redisMethod(method string) bool {
	switch method {
	case "Get", "Set", "Del", "Exists", "Expire", "HGet", "HSet", "Incr", "LPush", "RPush", "SAdd", "Publish", "Subscribe":
		return true
	default:
		return false
	}
}

func redisKey(context DependencyContext) (string, bool) {
	for _, argument := range context.Call.Args {
		if context.TypeOf != nil {
			typ := context.TypeOf(argument)
			if typ != nil && !isStringType(typ) {
				continue
			}
		}
		if value, static := context.StaticString(argument); static {
			return value, true
		}
		if context.TypeOf != nil && isStringType(context.TypeOf(argument)) {
			return "", false
		}
	}
	return "", false
}

func isStringType(typ types.Type) bool {
	if typ == nil {
		return false
	}
	basic, ok := types.Unalias(typ).Underlying().(*types.Basic)
	return ok && basic.Kind() == types.String
}

type saramaKafkaRule struct{}

func (saramaKafkaRule) Name() string { return "sarama" }

func (saramaKafkaRule) MatchDependency(context DependencyContext) ([]semantic.Dependency, []semantic.Diagnostic, bool) {
	packagePath := functionPackagePath(context.Callee)
	if packagePath != "github.com/IBM/sarama" && packagePath != "github.com/Shopify/sarama" {
		return nil, nil, false
	}
	if context.Selection == nil {
		return saramaFactoryDependency(context, packagePath)
	}
	receiver, receiverPackage := receiverIdentity(context.Selection.Recv())
	if receiverPackage != packagePath {
		return nil, nil, false
	}
	kind, resource, value, static, ok := saramaCall(context, receiver)
	if !ok {
		return nil, nil, false
	}
	dependency := semantic.Dependency{
		Kind:           kind,
		Name:           dependencyName(packagePath, receiver, context.Callee.Name()),
		Operation:      strings.ToLower(context.Callee.Name()),
		TargetPackage:  packagePath,
		Resource:       resource,
		Value:          value,
		ValueIsStatic:  static,
		SourceLocation: context.SourceLocation,
	}
	if !static {
		return []semantic.Dependency{dependency}, []semantic.Diagnostic{dependencyDiagnostic("DYNAMIC_KAFKA_VALUE", "Kafka topic or consumer group is not a static string", context.SourceLocation)}, true
	}
	return []semantic.Dependency{dependency}, nil, true
}

func saramaFactoryDependency(context DependencyContext, packagePath string) ([]semantic.Dependency, []semantic.Diagnostic, bool) {
	if context.Callee == nil || context.Callee.Name() != "NewConsumerGroup" || len(context.Call.Args) < 2 {
		return nil, nil, false
	}
	value, static := context.StaticString(context.Call.Args[1])
	dependency := semantic.Dependency{
		Kind:           semantic.DependencyKindKafkaConsumer,
		Name:           dependencyName(packagePath, "", context.Callee.Name()),
		Operation:      "new_consumer_group",
		TargetPackage:  packagePath,
		Resource:       "consumer_group",
		Value:          value,
		ValueIsStatic:  static,
		SourceLocation: context.SourceLocation,
	}
	if !static {
		return []semantic.Dependency{dependency}, []semantic.Diagnostic{dependencyDiagnostic("DYNAMIC_KAFKA_VALUE", "Kafka consumer group is not a static string", context.SourceLocation)}, true
	}
	return []semantic.Dependency{dependency}, nil, true
}

func saramaCall(context DependencyContext, receiver string) (semantic.DependencyKind, string, string, bool, bool) {
	method := context.Callee.Name()
	switch receiver {
	case "SyncProducer", "AsyncProducer":
		if method != "SendMessage" && method != "SendMessages" {
			return "", "", "", false, false
		}
		value, static := kafkaMessageTopic(context)
		return semantic.DependencyKindKafkaProducer, "topic", value, static, true
	case "Consumer":
		if method != "ConsumePartition" || len(context.Call.Args) == 0 {
			return "", "", "", false, false
		}
		value, static := context.StaticString(context.Call.Args[0])
		return semantic.DependencyKindKafkaConsumer, "topic", value, static, true
	case "ConsumerGroup":
		if method != "Consume" || len(context.Call.Args) < 2 {
			return "", "", "", false, false
		}
		value, static := staticStringSlice(context, context.Call.Args[1])
		return semantic.DependencyKindKafkaConsumer, "topic", value, static, true
	default:
		return "", "", "", false, false
	}
}

func kafkaMessageTopic(context DependencyContext) (string, bool) {
	if len(context.Call.Args) == 0 {
		return "", false
	}
	return compositeFieldString(context.Call.Args[0], "Topic", context)
}

func compositeFieldString(expression ast.Expr, field string, context DependencyContext) (string, bool) {
	for {
		switch current := expression.(type) {
		case *ast.UnaryExpr:
			expression = current.X
		case *ast.ParenExpr:
			expression = current.X
		default:
			goto unwrapped
		}
	}

unwrapped:
	literal, ok := expression.(*ast.CompositeLit)
	if !ok {
		return "", false
	}
	for _, element := range literal.Elts {
		keyValue, ok := element.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		identifier, ok := keyValue.Key.(*ast.Ident)
		if !ok || identifier.Name != field {
			continue
		}
		return context.StaticString(keyValue.Value)
	}
	return "", false
}

func staticStringSlice(context DependencyContext, expression ast.Expr) (string, bool) {
	literal, ok := expression.(*ast.CompositeLit)
	if !ok {
		return "", false
	}
	values := make([]string, 0, len(literal.Elts))
	for _, element := range literal.Elts {
		value, static := context.StaticString(element)
		if !static {
			return "", false
		}
		values = append(values, value)
	}
	return strings.Join(values, ","), true
}
