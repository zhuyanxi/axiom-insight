package plugins

import (
	"context"
	"fmt"
	"strings"

	observabilityv1 "github.com/zhuyanxi/axiom-insight/ir/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	LanguageGo           = "go"
	PluginVersion        = "v1"
	CurrentSchemaVersion = "v1"
	MinSchemaVersion     = CurrentSchemaVersion
	MaxSchemaVersion     = CurrentSchemaVersion
)

var supportedCapabilities = []string{
	"http_handlers",
	"grpc_handlers",
	"cron_jobs",
	"kafka_consumers",
	"kafka_producers",
	"sql",
	"redis",
	"http_clients",
	"rpc_clients",
}

// Analyzer is transport-neutral language frontend API.
type Analyzer interface {
	GetMetadata(context.Context, *observabilityv1.GetMetadataRequest) (*observabilityv1.LanguageAnalyzerMetadata, error)
	Analyze(context.Context, *observabilityv1.AnalyzeRequest) (*observabilityv1.AnalyzeResponse, error)
}

// Transport owns connection lifecycle for in-process or future external plugins.
type Transport interface {
	Connect(context.Context) (Analyzer, error)
	Close() error
}

// InProcessTransport exposes analyzer without process or network transport.
type InProcessTransport struct {
	analyzer Analyzer
}

func NewInProcessTransport(analyzer Analyzer) *InProcessTransport {
	if analyzer == nil {
		analyzer = NewGoAnalyzer()
	}
	return &InProcessTransport{analyzer: analyzer}
}

func (transport *InProcessTransport) Connect(ctx context.Context) (Analyzer, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return transport.analyzer, nil
}

func (*InProcessTransport) Close() error {
	return nil
}

func metadataFor(request *observabilityv1.GetMetadataRequest) (*observabilityv1.LanguageAnalyzerMetadata, error) {
	if err := validateSchemaVersion(request.GetSchemaVersion()); err != nil {
		return nil, err
	}
	return &observabilityv1.LanguageAnalyzerMetadata{
		Language:         LanguageGo,
		PluginVersion:    PluginVersion,
		Capabilities:     append([]string(nil), supportedCapabilities...),
		MinSchemaVersion: MinSchemaVersion,
		MaxSchemaVersion: MaxSchemaVersion,
	}, nil
}

func validateSchemaVersion(requested string) error {
	requested = strings.TrimSpace(requested)
	if requested == "" || requested == CurrentSchemaVersion {
		return nil
	}
	return status.Error(
		codes.FailedPrecondition,
		fmt.Sprintf("schema version %q is incompatible; supported range is %s..%s", requested, MinSchemaVersion, MaxSchemaVersion),
	)
}

func invalidRequest(message string) error {
	return status.Error(codes.InvalidArgument, message)
}
