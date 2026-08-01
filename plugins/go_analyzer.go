package plugins

import (
	"context"
	"fmt"
	"strings"

	"github.com/zhuyanxi/axiom-insight/compiler/goanalyzer"
	"github.com/zhuyanxi/axiom-insight/compiler/semantic"
	observabilityv1 "github.com/zhuyanxi/axiom-insight/ir/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// GoAnalyzer is the in-process Go language frontend.
type GoAnalyzer struct{}

func NewGoAnalyzer() *GoAnalyzer {
	return &GoAnalyzer{}
}

func (analyzer *GoAnalyzer) GetMetadata(_ context.Context, request *observabilityv1.GetMetadataRequest) (*observabilityv1.LanguageAnalyzerMetadata, error) {
	return metadataFor(request)
}

func (analyzer *GoAnalyzer) Analyze(ctx context.Context, request *observabilityv1.AnalyzeRequest) (*observabilityv1.AnalyzeResponse, error) {
	if request == nil {
		return nil, invalidRequest("analyze request is required")
	}
	if err := validateSchemaVersion(request.GetSchemaVersion()); err != nil {
		return nil, err
	}
	root := strings.TrimSpace(request.GetSourceRoot())
	if root == "" {
		return nil, invalidRequest("source_root is required")
	}

	document, err := goanalyzer.Analyze(ctx, root, goanalyzer.Options{
		IncludeTests: request.GetIncludeTests(),
		Include:      append([]string(nil), request.GetInclude()...),
		Exclude:      append([]string(nil), request.GetExclude()...),
		ConfigYAML:   request.GetConfig(),
		Env:          []string{"GOPROXY=off", "GOSUMDB=off"},
	})
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("analyze source root %q: %v", root, err))
	}

	irDocument, err := semantic.ToIR(document)
	if err != nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("convert semantic document to IR: %v", err))
	}
	return &observabilityv1.AnalyzeResponse{
		Document:    irDocument,
		Diagnostics: append([]*observabilityv1.Diagnostic(nil), irDocument.Diagnostics...),
	}, nil
}

var _ Analyzer = (*GoAnalyzer)(nil)
