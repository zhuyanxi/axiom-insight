package semantic

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	observabilityv1 "github.com/zhuyanxi/axiom-insight/ir/v1"
)

const defaultSchemaVersion = "v1"

func ToIR(document Document) (*observabilityv1.ObservabilityDocument, error) {
	packageIDs := make(map[int]string, len(document.Packages))
	functionIDs := make(map[int]string, len(document.Functions))
	endpointIDs := make(map[int]string, len(document.Endpoints))
	dependencyIDs := make(map[int]string, len(document.Dependencies))
	callEdgeIDs := make(map[int]string, len(document.CallEdges))

	if err := indexPackages(document.Packages, packageIDs); err != nil {
		return nil, err
	}
	if err := indexFunctions(document.Functions, functionIDs); err != nil {
		return nil, err
	}
	if err := indexEndpoints(document.Endpoints, endpointIDs); err != nil {
		return nil, err
	}
	if err := indexDependencies(document.Dependencies, dependencyIDs); err != nil {
		return nil, err
	}
	if err := indexCallEdges(document.CallEdges, callEdgeIDs); err != nil {
		return nil, err
	}

	diagnostics := append([]Diagnostic(nil), document.Diagnostics...)
	result := &observabilityv1.ObservabilityDocument{
		SchemaVersion: document.SchemaVersion,
		Service: &observabilityv1.Service{
			Name:       document.Service.Name,
			SourceRoot: filepath.ToSlash(document.Service.SourceRoot),
			Language:   document.Service.Language,
			ModulePath: document.Service.ModulePath,
			Version:    document.Service.Version,
			PackageIds: sortedStrings(document.Service.PackageIDs),
		},
	}
	if result.SchemaVersion == "" {
		result.SchemaVersion = defaultSchemaVersion
	}
	if len(result.Service.PackageIds) == 0 {
		for _, id := range packageIDs {
			result.Service.PackageIds = append(result.Service.PackageIds, id)
		}
		sort.Strings(result.Service.PackageIds)
	}

	for index, pkg := range document.Packages {
		files := make([]string, len(pkg.Files))
		for fileIndex, file := range pkg.Files {
			files[fileIndex] = filepath.ToSlash(file)
		}
		sort.Strings(files)
		result.Packages = append(result.Packages, &observabilityv1.Package{
			Id:         packageIDs[index],
			Name:       pkg.Name,
			ImportPath: pkg.ImportPath,
			Files:      files,
		})
	}
	sort.Slice(result.Packages, func(left, right int) bool {
		return result.Packages[left].Id < result.Packages[right].Id
	})

	for index, function := range document.Functions {
		location := convertLocation(function.SourceLocation, document.Service.SourceRoot, &diagnostics, "FUNCTION")
		result.Functions = append(result.Functions, &observabilityv1.Function{
			Id:                functionIDs[index],
			Name:              function.Name,
			QualifiedName:     function.QualifiedName,
			PackageId:         function.PackageID,
			PackagePath:       function.PackagePath,
			Receiver:          function.Receiver,
			Signature:         function.Signature,
			SourceLocation:    location,
			Exported:          function.Exported,
			InputEndpointIds:  sortedStrings(function.InputEndpointIDs),
			OutputEndpointIds: sortedStrings(function.OutputEndpointIDs),
			DependencyIds:     sortedStrings(function.DependencyIDs),
			CallerFunctionIds: sortedStrings(function.CallerFunctionIDs),
			CalleeFunctionIds: sortedStrings(function.CalleeFunctionIDs),
		})
	}
	sort.Slice(result.Functions, func(left, right int) bool {
		return result.Functions[left].Id < result.Functions[right].Id
	})

	for index, endpoint := range document.Endpoints {
		kind, known := endpointKind(endpoint.Kind)
		if !known {
			diagnostics = append(diagnostics, Diagnostic{
				Severity:       DiagnosticSeverityWarning,
				Code:           "UNKNOWN_ENDPOINT_KIND",
				Message:        fmt.Sprintf("endpoint %q has unsupported kind %q", endpoint.Name, endpoint.Kind),
				SourceLocation: endpoint.SourceLocation,
			})
		}
		result.Endpoints = append(result.Endpoints, &observabilityv1.Endpoint{
			Id:             endpointIDs[index],
			Kind:           kind,
			Name:           endpoint.Name,
			FunctionId:     endpoint.FunctionID,
			SourceLocation: convertLocation(endpoint.SourceLocation, document.Service.SourceRoot, &diagnostics, "ENDPOINT"),
			HttpMethod:     endpoint.HTTPMethod,
			HttpPath:       endpoint.HTTPPath,
			GrpcService:    endpoint.GRPCService,
			GrpcMethod:     endpoint.GRPCMethod,
			CronSchedule:   endpoint.CronSchedule,
		})
	}
	sort.Slice(result.Endpoints, func(left, right int) bool {
		return result.Endpoints[left].Id < result.Endpoints[right].Id
	})

	for index, dependency := range document.Dependencies {
		kind, known := dependencyKind(dependency.Kind)
		if !known {
			diagnostics = append(diagnostics, Diagnostic{
				Severity:       DiagnosticSeverityWarning,
				Code:           "UNKNOWN_DEPENDENCY_KIND",
				Message:        fmt.Sprintf("dependency %q has unsupported kind %q", dependency.Name, dependency.Kind),
				SourceLocation: dependency.SourceLocation,
			})
		}
		result.Dependencies = append(result.Dependencies, &observabilityv1.Dependency{
			Id:             dependencyIDs[index],
			Kind:           kind,
			Name:           dependency.Name,
			FunctionId:     dependency.FunctionID,
			SourceLocation: convertLocation(dependency.SourceLocation, document.Service.SourceRoot, &diagnostics, "DEPENDENCY"),
			Operation:      dependency.Operation,
			TargetService:  dependency.TargetService,
			TargetUrl:      dependency.TargetURL,
			TargetPackage:  dependency.TargetPackage,
			Resource:       dependency.Resource,
			Value:          dependency.Value,
			ValueIsStatic:  dependency.ValueIsStatic,
		})
	}
	sort.Slice(result.Dependencies, func(left, right int) bool {
		return result.Dependencies[left].Id < result.Dependencies[right].Id
	})

	for index, edge := range document.CallEdges {
		resolution, known := callResolution(edge.Resolution)
		if !known {
			diagnostics = append(diagnostics, Diagnostic{
				Severity:       DiagnosticSeverityWarning,
				Code:           "UNKNOWN_CALL_RESOLUTION",
				Message:        fmt.Sprintf("call edge %q has unsupported resolution %q", edge.ID, edge.Resolution),
				SourceLocation: edge.SourceLocation,
			})
		}
		if edge.CalleeFunctionID == "" || edge.Resolution == CallResolutionUnresolved {
			diagnostics = append(diagnostics, Diagnostic{
				Severity:       DiagnosticSeverityWarning,
				Code:           "UNRESOLVED_CALL",
				Message:        "call target could not be resolved statically",
				SourceLocation: edge.SourceLocation,
			})
		}
		result.CallEdges = append(result.CallEdges, &observabilityv1.CallEdge{
			Id:               callEdgeIDs[index],
			CallerFunctionId: edge.CallerFunctionID,
			CalleeFunctionId: edge.CalleeFunctionID,
			SourceLocation:   convertLocation(edge.SourceLocation, document.Service.SourceRoot, &diagnostics, "CALL_EDGE"),
			Resolution:       resolution,
		})
	}
	sort.Slice(result.CallEdges, func(left, right int) bool {
		return result.CallEdges[left].Id < result.CallEdges[right].Id
	})

	for _, diagnostic := range diagnostics {
		result.Diagnostics = append(result.Diagnostics, &observabilityv1.Diagnostic{
			Severity:       diagnosticSeverity(diagnostic.Severity),
			Code:           diagnostic.Code,
			Message:        diagnostic.Message,
			SourceLocation: convertLocation(diagnostic.SourceLocation, document.Service.SourceRoot, nil, "DIAGNOSTIC"),
		})
	}
	sort.SliceStable(result.Diagnostics, func(left, right int) bool {
		if result.Diagnostics[left].Code != result.Diagnostics[right].Code {
			return result.Diagnostics[left].Code < result.Diagnostics[right].Code
		}
		return result.Diagnostics[left].Message < result.Diagnostics[right].Message
	})

	return result, nil
}

func indexPackages(packages []Package, ids map[int]string) error {
	return indexValues(len(packages), func(index int) string { return PackageID(packages[index]) }, ids, "package")
}

func indexFunctions(functions []Function, ids map[int]string) error {
	return indexValues(len(functions), func(index int) string { return FunctionID(functions[index]) }, ids, "function")
}

func indexEndpoints(endpoints []Endpoint, ids map[int]string) error {
	return indexValues(len(endpoints), func(index int) string { return EndpointID(endpoints[index]) }, ids, "endpoint")
}

func indexDependencies(dependencies []Dependency, ids map[int]string) error {
	return indexValues(len(dependencies), func(index int) string { return DependencyID(dependencies[index]) }, ids, "dependency")
}

func indexCallEdges(edges []CallEdge, ids map[int]string) error {
	return indexValues(len(edges), func(index int) string { return CallEdgeID(edges[index]) }, ids, "call edge")
}

func indexValues(length int, value func(int) string, ids map[int]string, kind string) error {
	seen := make(map[string]struct{}, length)
	for index := 0; index < length; index++ {
		id := value(index)
		if id == "" {
			return fmt.Errorf("%s at index %d has empty ID", kind, index)
		}
		if _, exists := seen[id]; exists {
			return fmt.Errorf("duplicate %s ID %q", kind, id)
		}
		seen[id] = struct{}{}
		ids[index] = id
	}
	return nil
}

func convertLocation(location SourceLocation, sourceRoot string, diagnostics *[]Diagnostic, owner string) *observabilityv1.SourceLocation {
	if location.IsZero() {
		if diagnostics != nil {
			*diagnostics = append(*diagnostics, Diagnostic{
				Severity: DiagnosticSeverityWarning,
				Code:     "MISSING_SOURCE_LOCATION",
				Message:  owner + " has no source location",
			})
		}
		return nil
	}

	path, valid := relativePath(sourceRoot, location.RelativePath)
	if !valid {
		if diagnostics != nil {
			*diagnostics = append(*diagnostics, Diagnostic{
				Severity:       DiagnosticSeverityError,
				Code:           "INVALID_SOURCE_PATH",
				Message:        owner + " source path must be relative to source root",
				SourceLocation: SourceLocation{RelativePath: ""},
			})
		}
		path = ""
	}
	return &observabilityv1.SourceLocation{
		RelativePath: path,
		StartLine:    location.StartLine,
		StartColumn:  location.StartColumn,
		EndLine:      location.EndLine,
		EndColumn:    location.EndColumn,
	}
}

func relativePath(sourceRoot, sourcePath string) (string, bool) {
	path := filepath.Clean(sourcePath)
	if path == "." {
		path = ""
	}
	if filepath.IsAbs(path) {
		root := filepath.Clean(sourceRoot)
		if !filepath.IsAbs(root) {
			return "", false
		}
		relative, err := filepath.Rel(root, path)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return "", false
		}
		path = relative
	}
	path = filepath.ToSlash(path)
	if path == ".." || strings.HasPrefix(path, "../") {
		return "", false
	}
	return path, true
}

func sortedStrings(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	return result
}

func endpointKind(kind EndpointKind) (observabilityv1.EndpointKind, bool) {
	switch kind {
	case EndpointKindHTTPHandler:
		return observabilityv1.EndpointKind_HTTP_HANDLER, true
	case EndpointKindGRPCHandler:
		return observabilityv1.EndpointKind_GRPC_HANDLER, true
	case EndpointKindCronJob:
		return observabilityv1.EndpointKind_CRON_JOB, true
	default:
		return observabilityv1.EndpointKind_ENDPOINT_KIND_UNSPECIFIED, false
	}
}

func dependencyKind(kind DependencyKind) (observabilityv1.DependencyKind, bool) {
	switch kind {
	case DependencyKindKafkaProducer:
		return observabilityv1.DependencyKind_KAFKA_PRODUCER, true
	case DependencyKindKafkaConsumer:
		return observabilityv1.DependencyKind_KAFKA_CONSUMER, true
	case DependencyKindSQL:
		return observabilityv1.DependencyKind_SQL, true
	case DependencyKindRedis:
		return observabilityv1.DependencyKind_REDIS, true
	case DependencyKindHTTPClient:
		return observabilityv1.DependencyKind_HTTP_CLIENT, true
	case DependencyKindRPCClient:
		return observabilityv1.DependencyKind_RPC_CLIENT, true
	default:
		return observabilityv1.DependencyKind_DEPENDENCY_KIND_UNSPECIFIED, false
	}
}

func callResolution(resolution CallResolution) (observabilityv1.CallResolution, bool) {
	switch resolution {
	case CallResolutionResolved:
		return observabilityv1.CallResolution_RESOLVED, true
	case CallResolutionUnresolved:
		return observabilityv1.CallResolution_UNRESOLVED, true
	default:
		return observabilityv1.CallResolution_CALL_RESOLUTION_UNSPECIFIED, false
	}
}

func diagnosticSeverity(severity DiagnosticSeverity) observabilityv1.DiagnosticSeverity {
	switch severity {
	case DiagnosticSeverityInfo:
		return observabilityv1.DiagnosticSeverity_INFO
	case DiagnosticSeverityWarning:
		return observabilityv1.DiagnosticSeverity_WARNING
	case DiagnosticSeverityError:
		return observabilityv1.DiagnosticSeverity_ERROR
	default:
		return observabilityv1.DiagnosticSeverity_DIAGNOSTIC_SEVERITY_UNSPECIFIED
	}
}
