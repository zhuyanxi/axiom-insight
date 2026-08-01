package semantic

// ScanSummary contains stable counts derived from a semantic document.
type ScanSummary struct {
	HTTPHandlers   int
	GRPCHandlers   int
	CronJobs       int
	KafkaConsumers int
	KafkaProducers int
	SQL            int
	Redis          int
	HTTPClients    int
	RPCClients     int
	Diagnostics    int
}

// SummaryItem is one fixed-order scan summary entry.
type SummaryItem struct {
	Name  string
	Count int
}

const (
	SummaryHTTPHandlers   = "http_handlers"
	SummaryGRPCHandlers   = "grpc_handlers"
	SummaryCronJobs       = "cron_jobs"
	SummaryKafkaConsumers = "kafka_consumers"
	SummaryKafkaProducers = "kafka_producers"
	SummarySQL            = "sql"
	SummaryRedis          = "redis"
	SummaryHTTPClients    = "http_clients"
	SummaryRPCClients     = "rpc_clients"
	SummaryDiagnostics    = "diagnostics"
)

// Summarize derives all scan counts from semantic entities and diagnostics.
func Summarize(document Document) ScanSummary {
	summary := ScanSummary{Diagnostics: len(document.Diagnostics)}
	for _, endpoint := range document.Endpoints {
		switch endpoint.Kind {
		case EndpointKindHTTPHandler:
			summary.HTTPHandlers++
		case EndpointKindGRPCHandler:
			summary.GRPCHandlers++
		case EndpointKindCronJob:
			summary.CronJobs++
		}
	}
	for _, dependency := range document.Dependencies {
		switch dependency.Kind {
		case DependencyKindKafkaConsumer:
			summary.KafkaConsumers++
		case DependencyKindKafkaProducer:
			summary.KafkaProducers++
		case DependencyKindSQL:
			summary.SQL++
		case DependencyKindRedis:
			summary.Redis++
		case DependencyKindHTTPClient:
			summary.HTTPClients++
		case DependencyKindRPCClient:
			summary.RPCClients++
		}
	}
	return summary
}

// Items returns every summary category in stable display order.
func (summary ScanSummary) Items() []SummaryItem {
	return []SummaryItem{
		{Name: SummaryHTTPHandlers, Count: summary.HTTPHandlers},
		{Name: SummaryGRPCHandlers, Count: summary.GRPCHandlers},
		{Name: SummaryCronJobs, Count: summary.CronJobs},
		{Name: SummaryKafkaConsumers, Count: summary.KafkaConsumers},
		{Name: SummaryKafkaProducers, Count: summary.KafkaProducers},
		{Name: SummarySQL, Count: summary.SQL},
		{Name: SummaryRedis, Count: summary.Redis},
		{Name: SummaryHTTPClients, Count: summary.HTTPClients},
		{Name: SummaryRPCClients, Count: summary.RPCClients},
		{Name: SummaryDiagnostics, Count: summary.Diagnostics},
	}
}
