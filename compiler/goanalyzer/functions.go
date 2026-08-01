package goanalyzer

import (
	"fmt"
	"go/ast"
	"go/format"
	"go/token"
	"go/types"
	"sort"
	"strings"

	"github.com/zhuyanxi/axiom-insight/compiler/semantic"
	"golang.org/x/tools/go/packages"
)

type functionAnalysis struct {
	functions    []semantic.Function
	endpoints    []semantic.Endpoint
	dependencies []semantic.Dependency
	callEdges    []semantic.CallEdge
	diagnostics  []semantic.Diagnostic
}

type syntaxFile struct {
	path string
	file *ast.File
}

type functionRecord struct {
	id    string
	value semantic.Function
	body  *ast.BlockStmt
	info  *types.Info
	fset  *token.FileSet
}

type functionAnalyzer struct {
	root             string
	includeTests     bool
	packageIDs       map[string]string
	projectPackages  map[string]struct{}
	functions        []semantic.Function
	records          []functionRecord
	functionIndex    map[string]int
	functionObjects  map[*types.Func]string
	functionLiterals map[*ast.FuncLit]string
	endpoints        []semantic.Endpoint
	dependencies     []semantic.Dependency
	callEdges        []semantic.CallEdge
	diagnostics      []semantic.Diagnostic
}

func analyzeFunctions(root string, loaded []*packages.Package, packageIDs map[string]string, includeTests bool, adapters []EndpointAdapter, dependencyRules []DependencyRule) functionAnalysis {
	analyzer := &functionAnalyzer{
		root:             root,
		includeTests:     includeTests,
		packageIDs:       packageIDs,
		projectPackages:  make(map[string]struct{}, len(packageIDs)),
		functionIndex:    make(map[string]int),
		functionObjects:  make(map[*types.Func]string),
		functionLiterals: make(map[*ast.FuncLit]string),
	}
	for packagePath := range packageIDs {
		if packagePath != "" {
			analyzer.projectPackages[packagePath] = struct{}{}
		}
	}

	for _, pkg := range loaded {
		if _, ok := packageIDs[pkg.PkgPath]; !ok {
			continue
		}
		analyzer.collectPackage(pkg)
	}
	analyzer.collectCalls()
	analyzer.collectEndpoints(adapters)
	analyzer.collectDependencies(dependencyRules)
	analyzer.sortResults()
	return functionAnalysis{
		functions:    analyzer.functions,
		endpoints:    analyzer.endpoints,
		dependencies: analyzer.dependencies,
		callEdges:    analyzer.callEdges,
		diagnostics:  analyzer.diagnostics,
	}
}

func (analyzer *functionAnalyzer) collectPackage(pkg *packages.Package) {
	files := syntaxFiles(analyzer.root, pkg, analyzer.includeTests)
	packageID := analyzer.packageIDs[pkg.PkgPath]
	for _, source := range files {
		for _, declaration := range source.file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Name == nil {
				continue
			}
			value := semantic.Function{
				Name:           function.Name.Name,
				QualifiedName:  pkg.Name + "." + function.Name.Name,
				PackageID:      packageID,
				PackagePath:    pkg.PkgPath,
				Receiver:       receiverText(pkg.Fset, function.Recv),
				Signature:      functionSignature(pkg.TypesInfo, function.Name),
				SourceLocation: nodeLocation(analyzer.root, pkg.Fset, function),
				Exported:       ast.IsExported(function.Name.Name),
			}
			record := analyzer.addFunction(value, function.Body, pkg.TypesInfo, pkg.Fset)
			if record == nil {
				continue
			}
			if pkg.TypesInfo != nil {
				if object, ok := pkg.TypesInfo.Defs[function.Name].(*types.Func); ok {
					analyzer.functionObjects[object] = record.id
				}
			}
			analyzer.collectLiterals(*record)
		}
	}
}

func (analyzer *functionAnalyzer) addFunction(value semantic.Function, body *ast.BlockStmt, info *types.Info, fset *token.FileSet) *functionRecord {
	value.ID = semantic.FunctionID(value)
	if _, exists := analyzer.functionIndex[value.ID]; exists {
		return nil
	}
	record := functionRecord{id: value.ID, value: value, body: body, info: info, fset: fset}
	analyzer.functionIndex[value.ID] = len(analyzer.records)
	analyzer.records = append(analyzer.records, record)
	analyzer.functions = append(analyzer.functions, value)
	return &analyzer.records[len(analyzer.records)-1]
}

func (analyzer *functionAnalyzer) collectLiterals(parent functionRecord) {
	if parent.body == nil {
		return
	}
	ast.Inspect(parent.body, func(node ast.Node) bool {
		literal, ok := node.(*ast.FuncLit)
		if !ok {
			return true
		}
		location := nodeLocation(analyzer.root, parent.fset, literal)
		id := fmt.Sprintf("%s:anonymous:%s:%d:%d", parent.id, location.RelativePath, location.StartLine, location.StartColumn)
		value := semantic.Function{
			ID:             "fn:" + id,
			Name:           "<anonymous>",
			QualifiedName:  parent.value.QualifiedName + ".<anonymous>",
			PackageID:      parent.value.PackageID,
			PackagePath:    parent.value.PackagePath,
			Signature:      signatureText(typeOf(parent.info, literal)),
			SourceLocation: location,
		}
		value.ID = "fn:" + id
		record := analyzer.addFunction(value, literal.Body, parent.info, parent.fset)
		if record != nil {
			analyzer.functionLiterals[literal] = record.id
			analyzer.collectLiterals(*record)
		}
		return false
	})
}

func (analyzer *functionAnalyzer) collectCalls() {
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
			calleeID, unresolved, tracked := analyzer.resolveCall(call, record.info)
			if !tracked {
				return true
			}
			resolution := semantic.CallResolutionResolved
			if unresolved {
				resolution = semantic.CallResolutionUnresolved
			}
			edge := semantic.CallEdge{
				CallerFunctionID: record.id,
				CalleeFunctionID: calleeID,
				SourceLocation:   nodeLocation(analyzer.root, record.fset, call),
				Resolution:       resolution,
			}
			analyzer.callEdges = append(analyzer.callEdges, edge)
			if unresolved {
				analyzer.diagnostics = append(analyzer.diagnostics, semantic.Diagnostic{
					Severity:       semantic.DiagnosticSeverityWarning,
					Code:           "UNRESOLVED_CALL",
					Message:        "call target could not be resolved statically",
					SourceLocation: edge.SourceLocation,
				})
				return true
			}
			analyzer.addFunctionRelation(record.id, calleeID)
			return true
		})
	}
}

func (analyzer *functionAnalyzer) resolveCall(call *ast.CallExpr, info *types.Info) (string, bool, bool) {
	function := unwrapCallFunction(call.Fun)
	if literal, ok := function.(*ast.FuncLit); ok {
		calleeID := analyzer.functionLiterals[literal]
		if calleeID == "" {
			return "", true, true
		}
		return calleeID, false, true
	}
	if info == nil {
		return "", true, true
	}

	switch expression := function.(type) {
	case *ast.Ident:
		return analyzer.resolveObject(info.Uses[expression])
	case *ast.SelectorExpr:
		if selection := info.Selections[expression]; selection != nil {
			if isInterfaceType(selection.Recv()) {
				return "", true, true
			}
			return analyzer.resolveObject(selection.Obj())
		}
		return analyzer.resolveObject(info.Uses[expression.Sel])
	default:
		return "", true, true
	}
}

func (analyzer *functionAnalyzer) resolveObject(object types.Object) (string, bool, bool) {
	switch object := object.(type) {
	case *types.Builtin, *types.TypeName:
		return "", false, false
	case *types.Func:
		if functionID, ok := analyzer.functionObjects[object]; ok {
			return functionID, false, true
		}
		if object.Pkg() == nil {
			return "", false, false
		}
		if _, ok := analyzer.projectPackages[object.Pkg().Path()]; ok {
			return "", true, true
		}
		return "", false, false
	default:
		return "", true, true
	}
}

func (analyzer *functionAnalyzer) addFunctionRelation(callerID, calleeID string) {
	callerIndex, callerOK := analyzer.functionIndex[callerID]
	calleeIndex, calleeOK := analyzer.functionIndex[calleeID]
	if !callerOK || !calleeOK {
		return
	}
	analyzer.functions[callerIndex].CalleeFunctionIDs = appendUnique(analyzer.functions[callerIndex].CalleeFunctionIDs, calleeID)
	analyzer.functions[calleeIndex].CallerFunctionIDs = appendUnique(analyzer.functions[calleeIndex].CallerFunctionIDs, callerID)
}

func (analyzer *functionAnalyzer) sortResults() {
	sort.Slice(analyzer.functions, func(left, right int) bool {
		return semantic.FunctionID(analyzer.functions[left]) < semantic.FunctionID(analyzer.functions[right])
	})
	for index := range analyzer.functions {
		sort.Strings(analyzer.functions[index].CallerFunctionIDs)
		sort.Strings(analyzer.functions[index].CalleeFunctionIDs)
	}
	sort.Slice(analyzer.endpoints, func(left, right int) bool {
		return semantic.EndpointID(analyzer.endpoints[left]) < semantic.EndpointID(analyzer.endpoints[right])
	})
	sort.Slice(analyzer.dependencies, func(left, right int) bool {
		return semantic.DependencyID(analyzer.dependencies[left]) < semantic.DependencyID(analyzer.dependencies[right])
	})
	sort.Slice(analyzer.callEdges, func(left, right int) bool {
		leftID := semantic.CallEdgeID(analyzer.callEdges[left])
		rightID := semantic.CallEdgeID(analyzer.callEdges[right])
		if leftID != rightID {
			return leftID < rightID
		}
		return analyzer.callEdges[left].SourceLocation.StartColumn < analyzer.callEdges[right].SourceLocation.StartColumn
	})
	sort.SliceStable(analyzer.diagnostics, func(left, right int) bool {
		if analyzer.diagnostics[left].Code != analyzer.diagnostics[right].Code {
			return analyzer.diagnostics[left].Code < analyzer.diagnostics[right].Code
		}
		if analyzer.diagnostics[left].SourceLocation.RelativePath != analyzer.diagnostics[right].SourceLocation.RelativePath {
			return analyzer.diagnostics[left].SourceLocation.RelativePath < analyzer.diagnostics[right].SourceLocation.RelativePath
		}
		if analyzer.diagnostics[left].SourceLocation.StartLine != analyzer.diagnostics[right].SourceLocation.StartLine {
			return analyzer.diagnostics[left].SourceLocation.StartLine < analyzer.diagnostics[right].SourceLocation.StartLine
		}
		return analyzer.diagnostics[left].SourceLocation.StartColumn < analyzer.diagnostics[right].SourceLocation.StartColumn
	})
}

func syntaxFiles(root string, pkg *packages.Package, includeTests bool) []syntaxFile {
	if pkg == nil || pkg.Fset == nil {
		return nil
	}
	files := make([]syntaxFile, 0, len(pkg.Syntax))
	seen := make(map[string]struct{}, len(pkg.Syntax))
	for _, file := range pkg.Syntax {
		if file == nil {
			continue
		}
		position := pkg.Fset.PositionFor(file.Pos(), false)
		relative, ok := relativeSourcePath(root, position.Filename)
		if !ok || isExcludedPath(relative) || (!includeTests && strings.HasSuffix(relative, "_test.go")) {
			continue
		}
		if _, exists := seen[relative]; exists {
			continue
		}
		seen[relative] = struct{}{}
		files = append(files, syntaxFile{path: relative, file: file})
	}
	sort.Slice(files, func(left, right int) bool { return files[left].path < files[right].path })
	return files
}

func receiverText(fset *token.FileSet, receiver *ast.FieldList) string {
	if receiver == nil || len(receiver.List) == 0 {
		return ""
	}
	return nodeText(fset, receiver.List[0].Type)
}

func functionSignature(info *types.Info, name *ast.Ident) string {
	if info == nil || name == nil {
		return ""
	}
	if object, ok := info.Defs[name].(*types.Func); ok {
		return signatureText(object.Type())
	}
	return signatureText(info.TypeOf(name))
}

func signatureText(typ types.Type) string {
	if typ == nil {
		return ""
	}
	return types.TypeString(typ, func(pkg *types.Package) string {
		if pkg == nil {
			return ""
		}
		return pkg.Path()
	})
}

func typeOf(info *types.Info, expression ast.Expr) types.Type {
	if info == nil {
		return nil
	}
	return info.TypeOf(expression)
}

func nodeText(fset *token.FileSet, node ast.Node) string {
	if fset == nil || node == nil {
		return ""
	}
	var builder strings.Builder
	if err := format.Node(&builder, fset, node); err != nil {
		return ""
	}
	return builder.String()
}

func nodeLocation(root string, fset *token.FileSet, node ast.Node) semantic.SourceLocation {
	if fset == nil || node == nil || node.Pos() == token.NoPos {
		return semantic.SourceLocation{}
	}
	start := fset.PositionFor(node.Pos(), false)
	end := fset.PositionFor(node.End(), false)
	relative, ok := relativeSourcePath(root, start.Filename)
	if !ok {
		return semantic.SourceLocation{}
	}
	return semantic.SourceLocation{
		RelativePath: relative,
		StartLine:    int32(start.Line),
		StartColumn:  int32(start.Column),
		EndLine:      int32(end.Line),
		EndColumn:    int32(end.Column),
	}
}

func unwrapCallFunction(expression ast.Expr) ast.Expr {
	for {
		switch current := expression.(type) {
		case *ast.ParenExpr:
			expression = current.X
		case *ast.IndexExpr:
			expression = current.X
		case *ast.IndexListExpr:
			expression = current.X
		default:
			return expression
		}
	}
}

func isInterfaceType(typ types.Type) bool {
	if typ == nil {
		return false
	}
	returnType := types.Unalias(typ)
	_, ok := returnType.Underlying().(*types.Interface)
	return ok
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func FindFunction(document semantic.Document, functionID string) (semantic.Function, bool) {
	for _, function := range document.Functions {
		if semantic.FunctionID(function) == functionID {
			return function, true
		}
	}
	return semantic.Function{}, false
}

func IncomingCalls(document semantic.Document, functionID string) []semantic.CallEdge {
	return filterCallEdges(document.CallEdges, func(edge semantic.CallEdge) bool {
		return edge.CalleeFunctionID == functionID
	})
}

func OutgoingCalls(document semantic.Document, functionID string) []semantic.CallEdge {
	return filterCallEdges(document.CallEdges, func(edge semantic.CallEdge) bool {
		return edge.CallerFunctionID == functionID
	})
}

func filterCallEdges(edges []semantic.CallEdge, match func(semantic.CallEdge) bool) []semantic.CallEdge {
	result := make([]semantic.CallEdge, 0)
	for _, edge := range edges {
		if match(edge) {
			result = append(result, edge)
		}
	}
	sort.Slice(result, func(left, right int) bool {
		return semantic.CallEdgeID(result[left]) < semantic.CallEdgeID(result[right])
	})
	return result
}
