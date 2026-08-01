package plugins

import (
	"context"

	observabilityv1 "github.com/zhuyanxi/axiom-insight/ir/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// GRPCServer adapts transport-neutral Analyzer to generated gRPC service.
type GRPCServer struct {
	observabilityv1.UnimplementedLanguageAnalyzerServer
	analyzer Analyzer
}

func NewGRPCServer(analyzer Analyzer) *GRPCServer {
	if analyzer == nil {
		analyzer = NewGoAnalyzer()
	}
	return &GRPCServer{analyzer: analyzer}
}

func (server *GRPCServer) GetMetadata(ctx context.Context, request *observabilityv1.GetMetadataRequest) (*observabilityv1.LanguageAnalyzerMetadata, error) {
	if server == nil || server.analyzer == nil {
		return nil, status.Error(codes.Internal, "language analyzer is not configured")
	}
	return server.analyzer.GetMetadata(ctx, request)
}

func (server *GRPCServer) Analyze(ctx context.Context, request *observabilityv1.AnalyzeRequest) (*observabilityv1.AnalyzeResponse, error) {
	if server == nil || server.analyzer == nil {
		return nil, status.Error(codes.Internal, "language analyzer is not configured")
	}
	return server.analyzer.Analyze(ctx, request)
}

var _ observabilityv1.LanguageAnalyzerServer = (*GRPCServer)(nil)
