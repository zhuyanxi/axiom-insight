package semantic

import (
	"fmt"
	"path/filepath"
	"strings"
)

type Document struct {
	SchemaVersion string
	Service       Service
	Packages      []Package
	Functions     []Function
	Endpoints     []Endpoint
	Dependencies  []Dependency
	CallEdges     []CallEdge
	Diagnostics   []Diagnostic
}

type Service struct {
	Name       string
	SourceRoot string
	Language   string
	ModulePath string
	Version    string
	PackageIDs []string
}

type Package struct {
	ID         string
	Name       string
	ImportPath string
	Files      []string
}

type Function struct {
	ID                string
	Name              string
	QualifiedName     string
	PackageID         string
	PackagePath       string
	Receiver          string
	Signature         string
	SourceLocation    SourceLocation
	Exported          bool
	InputEndpointIDs  []string
	OutputEndpointIDs []string
	DependencyIDs     []string
	CallerFunctionIDs []string
	CalleeFunctionIDs []string
}

type Endpoint struct {
	ID             string
	Kind           EndpointKind
	Name           string
	FunctionID     string
	SourceLocation SourceLocation
	HTTPMethod     string
	HTTPPath       string
	GRPCService    string
	GRPCMethod     string
	CronSchedule   string
}

type Dependency struct {
	ID             string
	Kind           DependencyKind
	Name           string
	FunctionID     string
	SourceLocation SourceLocation
	Operation      string
	TargetService  string
	TargetURL      string
	TargetPackage  string
	Resource       string
	Value          string
	ValueIsStatic  bool
}

type CallEdge struct {
	ID               string
	CallerFunctionID string
	CalleeFunctionID string
	SourceLocation   SourceLocation
	Resolution       CallResolution
}

type Diagnostic struct {
	Severity       DiagnosticSeverity
	Code           string
	Message        string
	SourceLocation SourceLocation
}

type SourceLocation struct {
	RelativePath string
	StartLine    int32
	StartColumn  int32
	EndLine      int32
	EndColumn    int32
}

func (location SourceLocation) IsZero() bool {
	return location.RelativePath == "" && location.StartLine == 0 && location.StartColumn == 0 && location.EndLine == 0 && location.EndColumn == 0
}

type EndpointKind string

const (
	EndpointKindHTTPHandler EndpointKind = "HTTP_HANDLER"
	EndpointKindGRPCHandler EndpointKind = "GRPC_HANDLER"
	EndpointKindCronJob     EndpointKind = "CRON_JOB"
)

type DependencyKind string

const (
	DependencyKindKafkaProducer DependencyKind = "KAFKA_PRODUCER"
	DependencyKindKafkaConsumer DependencyKind = "KAFKA_CONSUMER"
	DependencyKindSQL           DependencyKind = "SQL"
	DependencyKindRedis         DependencyKind = "REDIS"
	DependencyKindHTTPClient    DependencyKind = "HTTP_CLIENT"
	DependencyKindRPCClient     DependencyKind = "RPC_CLIENT"
)

type CallResolution string

const (
	CallResolutionResolved   CallResolution = "RESOLVED"
	CallResolutionUnresolved CallResolution = "UNRESOLVED"
)

type DiagnosticSeverity string

const (
	DiagnosticSeverityInfo    DiagnosticSeverity = "INFO"
	DiagnosticSeverityWarning DiagnosticSeverity = "WARNING"
	DiagnosticSeverityError   DiagnosticSeverity = "ERROR"
)

func PackageID(pkg Package) string {
	if pkg.ID != "" {
		return pkg.ID
	}
	if pkg.ImportPath != "" {
		return "pkg:" + pkg.ImportPath
	}
	return "pkg:" + pkg.Name
}

func FunctionID(function Function) string {
	if function.ID != "" {
		return function.ID
	}
	identity := function.QualifiedName
	if identity == "" {
		identity = function.Name
		if function.PackagePath != "" {
			identity = function.PackagePath + "." + identity
		}
	}
	if function.Receiver != "" {
		identity = function.Receiver + "." + identity
	}
	if identity == "" {
		identity = sourceIdentity(function.SourceLocation)
	}
	return "fn:" + identity
}

func EndpointID(endpoint Endpoint) string {
	if endpoint.ID != "" {
		return endpoint.ID
	}
	identity := []string{string(endpoint.Kind), endpoint.FunctionID, endpoint.Name, endpoint.HTTPMethod, endpoint.HTTPPath, endpoint.GRPCService, endpoint.GRPCMethod, endpoint.CronSchedule}
	return "endpoint:" + nonEmptyIdentity(identity...)
}

func DependencyID(dependency Dependency) string {
	if dependency.ID != "" {
		return dependency.ID
	}
	identity := []string{dependency.FunctionID, string(dependency.Kind), dependency.Name, dependency.Operation, dependency.Resource, dependency.Value, sourceIdentity(dependency.SourceLocation)}
	return "dependency:" + nonEmptyIdentity(identity...)
}

func CallEdgeID(edge CallEdge) string {
	if edge.ID != "" {
		return edge.ID
	}
	identity := []string{edge.CallerFunctionID, edge.CalleeFunctionID, string(edge.Resolution), sourceIdentity(edge.SourceLocation)}
	return "edge:" + nonEmptyIdentity(identity...)
}

func nonEmptyIdentity(parts ...string) string {
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			values = append(values, part)
		}
	}
	return strings.Join(values, ":")
}

func sourceIdentity(location SourceLocation) string {
	if location.IsZero() {
		return "unknown"
	}
	return fmt.Sprintf("%s:%d:%d", filepath.ToSlash(location.RelativePath), location.StartLine, location.StartColumn)
}
