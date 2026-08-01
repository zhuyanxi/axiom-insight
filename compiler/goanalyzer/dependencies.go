package goanalyzer

import (
	"go/ast"
	"go/token"
	"go/types"
	"net/url"
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
	Caller             semantic.Function
	Call               *ast.CallExpr
	Callee             *types.Func
	Selection          *types.Selection
	Info               *types.Info
	FileSet            *token.FileSet
	SourceLocation     semantic.SourceLocation
	Receiver           ast.Expr
	TypeOf             func(ast.Expr) types.Type
	StaticString       func(ast.Expr) (string, bool)
	ResolveHTTPRequest func(ast.Expr) (HTTPRequest, bool)
	ResolveGRPCTarget  func(ast.Expr) (GRPCTarget, bool)
	IsProjectPackage   func(string) bool
}

type HTTPRequest struct {
	Method         string
	URL            string
	MethodIsStatic bool
	URLIsStatic    bool
}

type GRPCTarget struct {
	Value    string
	IsStatic bool
	Known    bool
}

// DefaultDependencyRules returns built-in SQL, Redis, and Kafka recognizers.
func DefaultDependencyRules() []DependencyRule {
	return []DependencyRule{
		databaseSQLRule{},
		redisRule{},
		saramaKafkaRule{},
		netHTTPClientRule{},
		grpcClientRule{},
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
		httpRequests := newHTTPRequestResolver(record.body, record.info)
		grpcTargets := newGRPCTargetResolver(record.body, record.info)
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
				ResolveHTTPRequest: httpRequests.Resolve,
				ResolveGRPCTarget:  grpcTargets.Resolve,
				IsProjectPackage: func(packagePath string) bool {
					_, exists := analyzer.projectPackages[packagePath]
					return exists
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
			return []semantic.Dependency{dependency}, []semantic.Diagnostic{dependencyDiagnostic(semantic.DiagnosticCodeMissingSQLQuery, "SQL call has no query argument", context.SourceLocation)}, true
		}
		value, static := context.StaticString(context.Call.Args[argumentIndex])
		dependency.Value = value
		dependency.ValueIsStatic = static
		if !static {
			return []semantic.Dependency{dependency}, []semantic.Diagnostic{dependencyDiagnostic(semantic.DiagnosticCodeDynamicSQL, "SQL text is not a static string", context.SourceLocation)}, true
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
		return []semantic.Dependency{dependency}, []semantic.Diagnostic{dependencyDiagnostic(semantic.DiagnosticCodeDynamicRedisKey, "Redis key is not a static string", context.SourceLocation)}, true
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
		return []semantic.Dependency{dependency}, []semantic.Diagnostic{dependencyDiagnostic(semantic.DiagnosticCodeDynamicKafkaValue, "Kafka topic or consumer group is not a static string", context.SourceLocation)}, true
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
		return []semantic.Dependency{dependency}, []semantic.Diagnostic{dependencyDiagnostic(semantic.DiagnosticCodeDynamicKafkaValue, "Kafka consumer group is not a static string", context.SourceLocation)}, true
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

type httpRequestResolver struct {
	info     *types.Info
	bindings map[*types.Var]HTTPRequest
}

func newHTTPRequestResolver(body *ast.BlockStmt, info *types.Info) *httpRequestResolver {
	resolver := &httpRequestResolver{info: info, bindings: make(map[*types.Var]HTTPRequest)}
	if body == nil {
		return resolver
	}
	ast.Inspect(body, func(node ast.Node) bool {
		if _, ok := node.(*ast.FuncLit); ok {
			return false
		}
		switch current := node.(type) {
		case *ast.AssignStmt:
			resolver.bindAssignments(current.Lhs, current.Rhs)
		case *ast.ValueSpec:
			left := make([]ast.Expr, len(current.Names))
			for index, name := range current.Names {
				left[index] = name
			}
			resolver.bindAssignments(left, current.Values)
		}
		return true
	})
	return resolver
}

func (resolver *httpRequestResolver) bindAssignments(left []ast.Expr, right []ast.Expr) {
	if len(right) == 1 && len(left) > 0 {
		resolver.bind(left[0], right[0])
		return
	}
	for index := 0; index < len(left) && index < len(right); index++ {
		resolver.bind(left[index], right[index])
	}
}

func (resolver *httpRequestResolver) bind(left ast.Expr, right ast.Expr) {
	variable := variableObject(left, resolver.info)
	if variable == nil {
		return
	}
	request, ok := resolver.resolve(right)
	if ok {
		resolver.bindings[variable] = request
	}
}

func (resolver *httpRequestResolver) Resolve(expression ast.Expr) (HTTPRequest, bool) {
	return resolver.resolve(expression)
}

func (resolver *httpRequestResolver) resolve(expression ast.Expr) (HTTPRequest, bool) {
	for {
		switch current := expression.(type) {
		case *ast.ParenExpr:
			expression = current.X
		case *ast.UnaryExpr:
			expression = current.X
		default:
			goto unwrapped
		}
	}

unwrapped:
	switch current := expression.(type) {
	case *ast.Ident:
		if variable := variableObject(current, resolver.info); variable != nil {
			request, ok := resolver.bindings[variable]
			return request, ok
		}
	case *ast.CallExpr:
		return httpRequestFromCall(current, resolver.info)
	}
	return HTTPRequest{}, false
}

func httpRequestFromCall(call *ast.CallExpr, info *types.Info) (HTTPRequest, bool) {
	callee, _ := callTarget(call, info)
	if callee == nil || functionPackagePath(callee) != "net/http" {
		return HTTPRequest{}, false
	}
	methodIndex, urlIndex := -1, -1
	switch callee.Name() {
	case "NewRequest":
		methodIndex, urlIndex = 0, 1
	case "NewRequestWithContext":
		methodIndex, urlIndex = 1, 2
	default:
		return HTTPRequest{}, false
	}
	if len(call.Args) <= urlIndex {
		return HTTPRequest{}, false
	}
	method, methodStatic := staticString(info, call.Args[methodIndex])
	targetURL, urlStatic := staticString(info, call.Args[urlIndex])
	return HTTPRequest{
		Method:         method,
		URL:            targetURL,
		MethodIsStatic: methodStatic,
		URLIsStatic:    urlStatic,
	}, true
}

type grpcTargetResolver struct {
	info     *types.Info
	bindings map[*types.Var]GRPCTarget
}

func newGRPCTargetResolver(body *ast.BlockStmt, info *types.Info) *grpcTargetResolver {
	resolver := &grpcTargetResolver{info: info, bindings: make(map[*types.Var]GRPCTarget)}
	if body == nil {
		return resolver
	}
	ast.Inspect(body, func(node ast.Node) bool {
		if _, ok := node.(*ast.FuncLit); ok {
			return false
		}
		switch current := node.(type) {
		case *ast.AssignStmt:
			resolver.bindAssignments(current.Lhs, current.Rhs)
		case *ast.ValueSpec:
			left := make([]ast.Expr, len(current.Names))
			for index, name := range current.Names {
				left[index] = name
			}
			resolver.bindAssignments(left, current.Values)
		}
		return true
	})
	return resolver
}

func (resolver *grpcTargetResolver) bindAssignments(left []ast.Expr, right []ast.Expr) {
	if len(right) == 1 && len(left) > 0 {
		resolver.bind(left[0], right[0])
		return
	}
	for index := 0; index < len(left) && index < len(right); index++ {
		resolver.bind(left[index], right[index])
	}
}

func (resolver *grpcTargetResolver) bind(left ast.Expr, right ast.Expr) {
	variable := variableObject(left, resolver.info)
	if variable == nil {
		return
	}
	target, ok := resolver.resolve(right)
	if ok {
		resolver.bindings[variable] = target
	}
}

func (resolver *grpcTargetResolver) Resolve(expression ast.Expr) (GRPCTarget, bool) {
	return resolver.resolve(expression)
}

func (resolver *grpcTargetResolver) resolve(expression ast.Expr) (GRPCTarget, bool) {
	for {
		switch current := expression.(type) {
		case *ast.ParenExpr:
			expression = current.X
		case *ast.UnaryExpr:
			expression = current.X
		default:
			goto unwrapped
		}
	}

unwrapped:
	switch current := expression.(type) {
	case *ast.Ident:
		if variable := variableObject(current, resolver.info); variable != nil {
			target, ok := resolver.bindings[variable]
			return target, ok
		}
	case *ast.CallExpr:
		return grpcTargetFromCall(current, resolver.info, resolver)
	}
	return GRPCTarget{}, false
}

func grpcTargetFromCall(call *ast.CallExpr, info *types.Info, resolver *grpcTargetResolver) (GRPCTarget, bool) {
	callee, _ := callTarget(call, info)
	if callee == nil {
		return GRPCTarget{}, false
	}
	if functionPackagePath(callee) == "google.golang.org/grpc" {
		argumentIndex := -1
		switch callee.Name() {
		case "Dial", "NewClient":
			argumentIndex = 0
		case "DialContext":
			argumentIndex = 1
		}
		if argumentIndex >= 0 && len(call.Args) > argumentIndex {
			value, static := staticString(info, call.Args[argumentIndex])
			return GRPCTarget{Value: value, IsStatic: static, Known: true}, true
		}
	}
	if strings.HasPrefix(callee.Name(), "New") && strings.HasSuffix(callee.Name(), "Client") && len(call.Args) > 0 {
		return resolver.resolve(call.Args[0])
	}
	return GRPCTarget{}, false
}

func variableObject(expression ast.Expr, info *types.Info) *types.Var {
	if info == nil {
		return nil
	}
	identifier, ok := expression.(*ast.Ident)
	if !ok {
		return nil
	}
	if variable, ok := info.Defs[identifier].(*types.Var); ok {
		return variable
	}
	variable, _ := info.Uses[identifier].(*types.Var)
	return variable
}

type netHTTPClientRule struct{}

func (netHTTPClientRule) Name() string { return "net/http-client" }

func (netHTTPClientRule) MatchDependency(context DependencyContext) ([]semantic.Dependency, []semantic.Diagnostic, bool) {
	if context.Callee == nil || functionPackagePath(context.Callee) != "net/http" {
		return nil, nil, false
	}
	method := context.Callee.Name()
	receiver := ""
	if context.Selection == nil {
		if method != "Get" && method != "Post" {
			return nil, nil, false
		}
	} else {
		var packagePath string
		receiver, packagePath = receiverIdentity(context.Selection.Recv())
		if packagePath != "net/http" || receiver != "Client" || (method != "Do" && method != "Get" && method != "Post") {
			return nil, nil, false
		}
	}

	request, diagnostics := httpClientRequest(context, method)
	dependency := semantic.Dependency{
		Kind:           semantic.DependencyKindHTTPClient,
		Name:           dependencyName("net/http", receiver, method),
		Operation:      strings.ToLower(request.Method),
		TargetPackage:  "net/http",
		TargetURL:      request.URL,
		TargetService:  httpTargetService(request.URL),
		Resource:       "url",
		ValueIsStatic:  request.MethodIsStatic && request.URLIsStatic,
		SourceLocation: context.SourceLocation,
	}
	if dependency.Operation == "" {
		dependency.Operation = strings.ToLower(method)
	}
	if !request.URLIsStatic {
		diagnostics = append(diagnostics, dependencyDiagnostic(semantic.DiagnosticCodeDynamicHTTPURL, "HTTP client URL is not a static string", context.SourceLocation))
	}
	if !request.MethodIsStatic {
		diagnostics = append(diagnostics, dependencyDiagnostic(semantic.DiagnosticCodeDynamicHTTPMethod, "HTTP client method is not a static string", context.SourceLocation))
	}
	return []semantic.Dependency{dependency}, diagnostics, true
}

func httpClientRequest(context DependencyContext, method string) (HTTPRequest, []semantic.Diagnostic) {
	if method == "Get" || method == "Post" {
		request := HTTPRequest{Method: method, MethodIsStatic: true}
		if len(context.Call.Args) == 0 {
			request.URLIsStatic = false
			return request, []semantic.Diagnostic{dependencyDiagnostic(semantic.DiagnosticCodeInvalidHTTPClientCall, "HTTP client call has no URL argument", context.SourceLocation)}
		}
		request.URL, request.URLIsStatic = context.StaticString(context.Call.Args[0])
		return request, nil
	}
	if method == "Do" && len(context.Call.Args) > 0 && context.ResolveHTTPRequest != nil {
		if request, ok := context.ResolveHTTPRequest(context.Call.Args[0]); ok {
			return request, nil
		}
	}
	return HTTPRequest{Method: "do", MethodIsStatic: false, URLIsStatic: false}, nil
}

func httpTargetService(targetURL string) string {
	parsed, err := url.Parse(targetURL)
	if err != nil {
		return ""
	}
	return parsed.Host
}

type grpcClientRule struct{}

func (grpcClientRule) Name() string { return "grpc-client" }

func (grpcClientRule) MatchDependency(context DependencyContext) ([]semantic.Dependency, []semantic.Diagnostic, bool) {
	if context.Callee == nil || context.Selection == nil {
		return nil, nil, false
	}
	packagePath := functionPackagePath(context.Callee)
	if packagePath == "" || (context.IsProjectPackage != nil && context.IsProjectPackage(packagePath)) {
		return nil, nil, false
	}
	receiver, receiverPackage := receiverIdentity(context.Selection.Recv())
	if receiverPackage != packagePath || !isGeneratedGRPCClient(context.Callee, receiver) {
		return nil, nil, false
	}
	target := GRPCTarget{}
	knownTarget := false
	if context.ResolveGRPCTarget != nil {
		target, knownTarget = context.ResolveGRPCTarget(context.Receiver)
	}
	dependency := semantic.Dependency{
		Kind:           semantic.DependencyKindRPCClient,
		Name:           dependencyName(packagePath, receiver, context.Callee.Name()),
		Operation:      strings.ToLower(context.Callee.Name()),
		TargetService:  target.Value,
		TargetPackage:  packagePath,
		Resource:       receiver,
		ValueIsStatic:  knownTarget && target.IsStatic,
		SourceLocation: context.SourceLocation,
	}
	var diagnostics []semantic.Diagnostic
	if knownTarget && !target.IsStatic {
		diagnostics = append(diagnostics, dependencyDiagnostic(semantic.DiagnosticCodeDynamicGRPCTarget, "gRPC client target is not a static string", context.SourceLocation))
	}
	return []semantic.Dependency{dependency}, diagnostics, true
}

func isGeneratedGRPCClient(function *types.Func, receiver string) bool {
	if function == nil || !strings.HasSuffix(receiver, "Client") {
		return false
	}
	signature, ok := function.Type().(*types.Signature)
	if !ok || signature.Params().Len() == 0 || signature.Results().Len() < 2 {
		return false
	}
	if !isContextType(signature.Params().At(0).Type()) {
		return false
	}
	return isErrorType(signature.Results().At(signature.Results().Len() - 1).Type())
}

func isContextType(typ types.Type) bool {
	named, ok := types.Unalias(typ).(*types.Named)
	if !ok || named.Obj() == nil || named.Obj().Pkg() == nil {
		return false
	}
	return named.Obj().Pkg().Path() == "context" && named.Obj().Name() == "Context"
}

func isErrorType(typ types.Type) bool {
	errorObject := types.Universe.Lookup("error")
	if errorObject == nil {
		return false
	}
	errorInterface, ok := types.Unalias(errorObject.Type()).Underlying().(*types.Interface)
	if !ok {
		return false
	}
	return types.Implements(types.Unalias(typ), errorInterface)
}
