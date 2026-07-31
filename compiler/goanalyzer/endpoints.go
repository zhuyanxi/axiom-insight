package goanalyzer

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"strconv"
	"strings"

	"github.com/zhuyanxi/axiom-insight/compiler/semantic"
)

// EndpointAdapter recognizes one framework's registration calls.
type EndpointAdapter interface {
	Name() string
	MatchEndpoint(EndpointContext) ([]semantic.Endpoint, []semantic.Diagnostic, bool)
}

// EndpointContext contains typed call information for an adapter.
type EndpointContext struct {
	Caller            semantic.Function
	Call              *ast.CallExpr
	Parent            *ast.CallExpr
	Callee            *types.Func
	Selection         *types.Selection
	Info              *types.Info
	FileSet           *token.FileSet
	SourceLocation    semantic.SourceLocation
	ResolveFunction   func(ast.Expr) (semantic.Function, bool)
	FunctionForMethod func(types.Type, string) (semantic.Function, bool)
	TypeOf            func(ast.Expr) types.Type
	StaticString      func(ast.Expr) (string, bool)
}

// DefaultEndpointAdapters returns supported built-in endpoint recognizers.
func DefaultEndpointAdapters() []EndpointAdapter {
	return []EndpointAdapter{
		netHTTPAdapter{},
		gorillaMuxAdapter{},
		grpcAdapter{},
		robfigCronAdapter{},
		unknownHTTPRouterAdapter{},
	}
}

type endpointCallVisitor struct {
	parent *ast.CallExpr
	root   bool
	visit  func(*ast.CallExpr, *ast.CallExpr)
}

func (visitor *endpointCallVisitor) Visit(node ast.Node) ast.Visitor {
	if node == nil {
		return nil
	}
	if visitor.root {
		visitor.root = false
	} else if _, ok := node.(*ast.FuncLit); ok {
		return nil
	}
	if call, ok := node.(*ast.CallExpr); ok {
		visitor.visit(call, visitor.parent)
		return &endpointCallVisitor{parent: call, visit: visitor.visit}
	}
	return visitor
}

func (analyzer *functionAnalyzer) collectEndpoints(adapters []EndpointAdapter) {
	if adapters == nil {
		adapters = DefaultEndpointAdapters()
	}
	for _, record := range analyzer.records {
		if record.body == nil {
			continue
		}
		visitor := &endpointCallVisitor{
			root: true,
			visit: func(call, parent *ast.CallExpr) {
				callee, selection := callTarget(call, record.info)
				if callee == nil {
					return
				}
				context := EndpointContext{
					Caller:         record.value,
					Call:           call,
					Parent:         parent,
					Callee:         callee,
					Selection:      selection,
					Info:           record.info,
					FileSet:        record.fset,
					SourceLocation: nodeLocation(analyzer.root, record.fset, call),
					ResolveFunction: func(expression ast.Expr) (semantic.Function, bool) {
						return analyzer.resolveFunction(expression, record.info)
					},
					FunctionForMethod: func(typ types.Type, name string) (semantic.Function, bool) {
						return analyzer.functionForMethod(typ, name)
					},
					TypeOf: func(expression ast.Expr) types.Type {
						return typeOf(record.info, expression)
					},
					StaticString: func(expression ast.Expr) (string, bool) {
						return staticString(record.info, expression)
					},
				}
				for _, adapter := range adapters {
					endpoints, diagnostics, matched := adapter.MatchEndpoint(context)
					if !matched {
						continue
					}
					for _, endpoint := range endpoints {
						endpoint.SourceLocation = context.SourceLocation
						if endpoint.ID == "" {
							endpoint.ID = semantic.EndpointID(endpoint)
						}
						analyzer.endpoints = append(analyzer.endpoints, endpoint)
						if endpoint.FunctionID != "" {
							if index, ok := analyzer.functionIndex[endpoint.FunctionID]; ok {
								analyzer.functions[index].InputEndpointIDs = appendUnique(analyzer.functions[index].InputEndpointIDs, endpoint.ID)
							}
						}
					}
					analyzer.diagnostics = append(analyzer.diagnostics, diagnostics...)
					break
				}
			},
		}
		ast.Walk(visitor, record.body)
	}
}

func (analyzer *functionAnalyzer) resolveFunction(expression ast.Expr, info *types.Info) (semantic.Function, bool) {
	for {
		switch current := expression.(type) {
		case *ast.ParenExpr:
			expression = current.X
		case *ast.IndexExpr:
			expression = current.X
		case *ast.IndexListExpr:
			expression = current.X
		default:
			goto unwrapped
		}
	}

unwrapped:
	if info == nil || expression == nil {
		return semantic.Function{}, false
	}
	var object types.Object
	switch current := expression.(type) {
	case *ast.FuncLit:
		id := analyzer.functionLiterals[current]
		return analyzer.functionByID(id)
	case *ast.CallExpr:
		if isTypeConversion(current.Fun, info) && len(current.Args) == 1 {
			return analyzer.resolveFunction(current.Args[0], info)
		}
		return semantic.Function{}, false
	case *ast.Ident:
		object = info.Uses[current]
		if object == nil {
			object = info.Defs[current]
		}
	case *ast.SelectorExpr:
		if selection := info.Selections[current]; selection != nil {
			object = selection.Obj()
		} else {
			object = info.Uses[current.Sel]
		}
	default:
		return semantic.Function{}, false
	}
	function, ok := object.(*types.Func)
	if !ok {
		return semantic.Function{}, false
	}
	return analyzer.functionByID(analyzer.functionObjects[function])
}

func isTypeConversion(expression ast.Expr, info *types.Info) bool {
	if info == nil {
		return false
	}
	switch current := unwrapCallFunction(expression).(type) {
	case *ast.Ident:
		_, ok := info.Uses[current].(*types.TypeName)
		return ok
	case *ast.SelectorExpr:
		_, ok := info.Uses[current.Sel].(*types.TypeName)
		return ok
	default:
		return false
	}
}

func (analyzer *functionAnalyzer) functionByID(id string) (semantic.Function, bool) {
	if id == "" {
		return semantic.Function{}, false
	}
	index, ok := analyzer.functionIndex[id]
	if !ok {
		return semantic.Function{}, false
	}
	return analyzer.functions[index], true
}

func (analyzer *functionAnalyzer) functionForMethod(typ types.Type, name string) (semantic.Function, bool) {
	if typ == nil {
		return semantic.Function{}, false
	}
	methods := types.NewMethodSet(typ)
	for index := 0; index < methods.Len(); index++ {
		method := methods.At(index).Obj()
		if method.Name() != name {
			continue
		}
		function, ok := method.(*types.Func)
		if !ok {
			continue
		}
		return analyzer.functionByID(analyzer.functionObjects[function])
	}
	return semantic.Function{}, false
}

func callTarget(call *ast.CallExpr, info *types.Info) (*types.Func, *types.Selection) {
	if call == nil || info == nil {
		return nil, nil
	}
	switch expression := unwrapCallFunction(call.Fun).(type) {
	case *ast.Ident:
		function, _ := info.Uses[expression].(*types.Func)
		return function, nil
	case *ast.SelectorExpr:
		if selection := info.Selections[expression]; selection != nil {
			function, _ := selection.Obj().(*types.Func)
			return function, selection
		}
		function, _ := info.Uses[expression.Sel].(*types.Func)
		return function, nil
	default:
		return nil, nil
	}
}

func staticString(info *types.Info, expression ast.Expr) (string, bool) {
	if info != nil {
		if value, ok := info.Types[expression]; ok && value.Value != nil && value.Value.Kind() == constant.String {
			return constant.StringVal(value.Value), true
		}
	}
	literal, ok := expression.(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(literal.Value)
	return value, err == nil
}

func endpointDiagnostic(code, message string, location semantic.SourceLocation) semantic.Diagnostic {
	return semantic.Diagnostic{
		Severity:       semantic.DiagnosticSeverityWarning,
		Code:           code,
		Message:        message,
		SourceLocation: location,
	}
}

type netHTTPAdapter struct{}

func (netHTTPAdapter) Name() string { return "net/http" }

func (netHTTPAdapter) MatchEndpoint(context EndpointContext) ([]semantic.Endpoint, []semantic.Diagnostic, bool) {
	if !isFunction(context.Callee, "net/http", "Handle") && !isFunction(context.Callee, "net/http", "HandleFunc") {
		return nil, nil, false
	}
	if len(context.Call.Args) < 2 {
		return nil, []semantic.Diagnostic{endpointDiagnostic("INVALID_HTTP_REGISTRATION", "HTTP registration requires pattern and handler", context.SourceLocation)}, true
	}
	pattern, static := context.StaticString(context.Call.Args[0])
	var diagnostics []semantic.Diagnostic
	if !static {
		diagnostics = append(diagnostics, endpointDiagnostic("DYNAMIC_HTTP_PATTERN", "HTTP pattern is not a static string", context.SourceLocation))
	}
	function, ok := resolveHTTPHandler(context, context.Call.Args[1])
	if !ok {
		diagnostics = append(diagnostics, endpointDiagnostic("UNRESOLVED_HTTP_HANDLER", "HTTP handler could not be resolved statically", context.SourceLocation))
		return nil, diagnostics, true
	}
	method, path := parseHTTPPattern(pattern)
	endpoint := semantic.Endpoint{
		Kind:           semantic.EndpointKindHTTPHandler,
		Name:           endpointFunctionName(function, path),
		FunctionID:     function.ID,
		HTTPMethod:     method,
		HTTPPath:       path,
		SourceLocation: context.SourceLocation,
	}
	return []semantic.Endpoint{endpoint}, diagnostics, true
}

type gorillaMuxAdapter struct{}

func (gorillaMuxAdapter) Name() string { return "github.com/gorilla/mux" }

func (gorillaMuxAdapter) MatchEndpoint(context EndpointContext) ([]semantic.Endpoint, []semantic.Diagnostic, bool) {
	if !isFunction(context.Callee, "github.com/gorilla/mux", "Handle") && !isFunction(context.Callee, "github.com/gorilla/mux", "HandleFunc") {
		return nil, nil, false
	}
	if len(context.Call.Args) < 2 {
		return nil, []semantic.Diagnostic{endpointDiagnostic("INVALID_HTTP_REGISTRATION", "HTTP registration requires pattern and handler", context.SourceLocation)}, true
	}
	pattern, static := context.StaticString(context.Call.Args[0])
	var diagnostics []semantic.Diagnostic
	if !static {
		diagnostics = append(diagnostics, endpointDiagnostic("DYNAMIC_HTTP_PATTERN", "HTTP pattern is not a static string", context.SourceLocation))
	}
	function, ok := resolveHTTPHandler(context, context.Call.Args[1])
	if !ok {
		diagnostics = append(diagnostics, endpointDiagnostic("UNRESOLVED_HTTP_HANDLER", "HTTP handler could not be resolved statically", context.SourceLocation))
		return nil, diagnostics, true
	}
	method := gorillaMethod(context)
	_, path := parseHTTPPattern(pattern)
	endpoint := semantic.Endpoint{
		Kind:           semantic.EndpointKindHTTPHandler,
		Name:           endpointFunctionName(function, path),
		FunctionID:     function.ID,
		HTTPMethod:     method,
		HTTPPath:       path,
		SourceLocation: context.SourceLocation,
	}
	return []semantic.Endpoint{endpoint}, diagnostics, true
}

type grpcAdapter struct{}

func (grpcAdapter) Name() string { return "grpc" }

func (grpcAdapter) MatchEndpoint(context EndpointContext) ([]semantic.Endpoint, []semantic.Diagnostic, bool) {
	name := context.Callee.Name()
	if !strings.HasPrefix(name, "Register") || !strings.HasSuffix(name, "Server") {
		return nil, nil, false
	}
	service := strings.TrimSuffix(strings.TrimPrefix(name, "Register"), "Server")
	if service == "" || context.Callee.Type() == nil {
		return nil, nil, false
	}
	signature, ok := context.Callee.Type().(*types.Signature)
	if !ok || signature.Params().Len() < 2 || !isInterfaceType(signature.Params().At(1).Type()) {
		return nil, nil, false
	}
	if len(context.Call.Args) < 2 {
		return nil, []semantic.Diagnostic{endpointDiagnostic("INVALID_GRPC_REGISTRATION", "gRPC registration requires registrar and server", context.SourceLocation)}, true
	}
	serverType := context.TypeOf(context.Call.Args[1])
	serverInterface := types.Unalias(serverType)
	if serverInterface == nil || isInterfaceType(serverInterface) {
		return nil, []semantic.Diagnostic{endpointDiagnostic("UNRESOLVED_GRPC_SERVER", "gRPC server implementation could not be resolved statically", context.SourceLocation)}, true
	}
	interfaceType := types.Unalias(signature.Params().At(1).Type()).Underlying().(*types.Interface)
	endpoints := make([]semantic.Endpoint, 0, interfaceType.NumMethods())
	var diagnostics []semantic.Diagnostic
	for index := 0; index < interfaceType.NumMethods(); index++ {
		method := interfaceType.Method(index)
		function, found := context.FunctionForMethod(serverType, method.Name())
		if !found {
			diagnostics = append(diagnostics, endpointDiagnostic("UNRESOLVED_GRPC_METHOD", "gRPC server method could not be resolved statically: "+method.Name(), context.SourceLocation))
			continue
		}
		endpoints = append(endpoints, semantic.Endpoint{
			Kind:           semantic.EndpointKindGRPCHandler,
			Name:           service + "." + method.Name(),
			FunctionID:     function.ID,
			GRPCService:    service,
			GRPCMethod:     method.Name(),
			SourceLocation: context.SourceLocation,
		})
	}
	if len(endpoints) == 0 && len(diagnostics) == 0 {
		diagnostics = append(diagnostics, endpointDiagnostic("UNRESOLVED_GRPC_SERVER", "gRPC server implementation has no resolvable methods", context.SourceLocation))
	}
	return endpoints, diagnostics, true
}

type robfigCronAdapter struct{}

func (robfigCronAdapter) Name() string { return "github.com/robfig/cron" }

func (robfigCronAdapter) MatchEndpoint(context EndpointContext) ([]semantic.Endpoint, []semantic.Diagnostic, bool) {
	packagePath := functionPackagePath(context.Callee)
	if packagePath != "github.com/robfig/cron/v3" && packagePath != "github.com/robfig/cron" {
		return nil, nil, false
	}
	if context.Callee.Name() != "AddFunc" && context.Callee.Name() != "AddJob" {
		return nil, nil, false
	}
	if len(context.Call.Args) < 2 {
		return nil, []semantic.Diagnostic{endpointDiagnostic("INVALID_CRON_REGISTRATION", "cron registration requires schedule and callback", context.SourceLocation)}, true
	}
	function, ok := resolveCronCallback(context, context.Call.Args[1])
	if !ok {
		return nil, []semantic.Diagnostic{endpointDiagnostic("UNRESOLVED_CRON_CALLBACK", "cron callback could not be resolved statically", context.SourceLocation)}, true
	}
	schedule, static := context.StaticString(context.Call.Args[0])
	endpoint := semantic.Endpoint{
		Kind:           semantic.EndpointKindCronJob,
		Name:           endpointFunctionName(function, "cron"),
		FunctionID:     function.ID,
		CronSchedule:   schedule,
		SourceLocation: context.SourceLocation,
	}
	var diagnostics []semantic.Diagnostic
	if !static {
		diagnostics = append(diagnostics, endpointDiagnostic("DYNAMIC_CRON_SCHEDULE", "cron schedule is not a static string", context.SourceLocation))
	}
	return []semantic.Endpoint{endpoint}, diagnostics, true
}

type unknownHTTPRouterAdapter struct{}

func (unknownHTTPRouterAdapter) Name() string { return "unknown-http-router" }

func (unknownHTTPRouterAdapter) MatchEndpoint(context EndpointContext) ([]semantic.Endpoint, []semantic.Diagnostic, bool) {
	if context.Callee == nil || (context.Callee.Name() != "Handle" && context.Callee.Name() != "HandleFunc") {
		return nil, nil, false
	}
	packagePath := functionPackagePath(context.Callee)
	if packagePath == "" || packagePath == "net/http" || packagePath == "github.com/gorilla/mux" || !looksLikeHTTPRegistration(context.Callee.Type()) {
		return nil, nil, false
	}
	return nil, []semantic.Diagnostic{endpointDiagnostic("UNSUPPORTED_HTTP_ROUTER", "HTTP router is not supported by configured adapters: "+packagePath, context.SourceLocation)}, true
}

func isFunction(function *types.Func, packagePath, name string) bool {
	return function != nil && function.Name() == name && functionPackagePath(function) == packagePath
}

func looksLikeHTTPRegistration(typ types.Type) bool {
	signature, ok := typ.(*types.Signature)
	if !ok || signature.Params().Len() < 2 {
		return false
	}
	first := types.Unalias(signature.Params().At(0).Type())
	if first == nil {
		return false
	}
	firstBasic, ok := first.Underlying().(*types.Basic)
	if !ok || firstBasic.Kind() != types.String {
		return false
	}
	second := signature.Params().At(1).Type()
	if _, ok := types.Unalias(second).Underlying().(*types.Signature); ok {
		return true
	}
	return hasMethod(second, "ServeHTTP")
}

func hasMethod(typ types.Type, name string) bool {
	if typ == nil {
		return false
	}
	methods := types.NewMethodSet(typ)
	for index := 0; index < methods.Len(); index++ {
		if methods.At(index).Obj().Name() == name {
			return true
		}
	}
	return false
}

func functionPackagePath(function *types.Func) string {
	if function == nil || function.Pkg() == nil {
		return ""
	}
	return function.Pkg().Path()
}

func resolveHTTPHandler(context EndpointContext, expression ast.Expr) (semantic.Function, bool) {
	if function, ok := context.ResolveFunction(expression); ok {
		return function, true
	}
	if context.TypeOf != nil {
		if function, ok := context.FunctionForMethod(context.TypeOf(expression), "ServeHTTP"); ok {
			return function, true
		}
	}
	return semantic.Function{}, false
}

func resolveCronCallback(context EndpointContext, expression ast.Expr) (semantic.Function, bool) {
	if context.Callee.Name() == "AddFunc" {
		return context.ResolveFunction(expression)
	}
	if context.TypeOf == nil {
		return semantic.Function{}, false
	}
	return context.FunctionForMethod(context.TypeOf(expression), "Run")
}

func endpointFunctionName(function semantic.Function, fallback string) string {
	if function.Name != "" && function.Name != "<anonymous>" {
		return function.Name
	}
	return fallback
}

func parseHTTPPattern(pattern string) (string, string) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return "", ""
	}
	parts := strings.SplitN(pattern, " ", 2)
	if len(parts) == 2 && !strings.HasPrefix(parts[0], "/") {
		return parts[0], strings.TrimSpace(parts[1])
	}
	return "", pattern
}

func gorillaMethod(context EndpointContext) string {
	if context.Parent == nil || len(context.Parent.Args) == 0 {
		return ""
	}
	callee, _ := callTarget(context.Parent, context.Info)
	if !isFunction(callee, "github.com/gorilla/mux", "Methods") {
		return ""
	}
	method, ok := context.StaticString(context.Parent.Args[0])
	if !ok {
		return ""
	}
	return method
}
