package semantic

import "testing"

func TestSummarizeCountsAllCategories(t *testing.T) {
	document := Document{
		Endpoints: []Endpoint{
			{Kind: EndpointKindCronJob},
			{Kind: EndpointKindHTTPHandler},
			{Kind: EndpointKindGRPCHandler},
			{Kind: EndpointKindHTTPHandler},
			{Kind: EndpointKind("UNKNOWN")},
		},
		Dependencies: []Dependency{
			{Kind: DependencyKindKafkaConsumer},
			{Kind: DependencyKindKafkaProducer},
			{Kind: DependencyKindSQL},
			{Kind: DependencyKindRedis},
			{Kind: DependencyKindHTTPClient},
			{Kind: DependencyKindRPCClient},
			{Kind: DependencyKind("UNKNOWN")},
		},
		Diagnostics: []Diagnostic{{Code: "FIRST"}, {Code: "SECOND"}},
	}

	got := Summarize(document)
	want := ScanSummary{
		HTTPHandlers:   2,
		GRPCHandlers:   1,
		CronJobs:       1,
		KafkaConsumers: 1,
		KafkaProducers: 1,
		SQL:            1,
		Redis:          1,
		HTTPClients:    1,
		RPCClients:     1,
		Diagnostics:    2,
	}
	if got != want {
		t.Fatalf("summary = %+v, want %+v", got, want)
	}
}

func TestScanSummaryItemsHaveStableOrderAndZeroValues(t *testing.T) {
	items := (ScanSummary{}).Items()
	wantNames := []string{
		SummaryHTTPHandlers,
		SummaryGRPCHandlers,
		SummaryCronJobs,
		SummaryKafkaConsumers,
		SummaryKafkaProducers,
		SummarySQL,
		SummaryRedis,
		SummaryHTTPClients,
		SummaryRPCClients,
		SummaryDiagnostics,
	}
	if len(items) != len(wantNames) {
		t.Fatalf("summary item count = %d, want %d", len(items), len(wantNames))
	}
	for index, item := range items {
		if item.Name != wantNames[index] {
			t.Fatalf("summary item %d name = %q, want %q", index, item.Name, wantNames[index])
		}
		if item.Count != 0 {
			t.Fatalf("summary item %q count = %d, want 0", item.Name, item.Count)
		}
	}
}

func TestScanSummaryItemsReturnIndependentSlice(t *testing.T) {
	summary := ScanSummary{SQL: 2}
	first := summary.Items()
	first[0].Name = "changed"
	second := summary.Items()
	if second[0].Name != SummaryHTTPHandlers {
		t.Fatalf("summary items share mutable state: %+v", second)
	}
}
