package observabilityv1

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// phase0TestDocument mirrors testdata/genfixture/main.go: the document is
// exactly what the Phase 0 analyzer would emit, with no GenerationPlan.
func phase0TestDocument() *ObservabilityDocument {
	return &ObservabilityDocument{
		SchemaVersion: "v1",
		Service: &Service{
			Name:       "orders",
			SourceRoot: "./",
			Language:   "go",
			ModulePath: "example.com/orders",
			Version:    "1.0.0",
			PackageIds: []string{"pkg:orders"},
		},
		Packages: []*Package{{
			Id:         "pkg:orders",
			Name:       "orders",
			ImportPath: "example.com/orders",
			Files:      []string{"handler.go", "store.go"},
		}},
		Functions: []*Function{
			{
				Id:                "fn:orders.CreateOrder",
				Name:              "CreateOrder",
				QualifiedName:     "orders.CreateOrder",
				PackageId:         "pkg:orders",
				PackagePath:       "example.com/orders",
				Receiver:          "Orders",
				Signature:         "func() error",
				SourceLocation:    &SourceLocation{RelativePath: "handler.go", StartLine: 10, StartColumn: 1, EndLine: 12, EndColumn: 2},
				Exported:          true,
				InputEndpointIds:  []string{"endpoint:http:create-order", "endpoint:grpc:create-order", "endpoint:cron:cleanup"},
				DependencyIds:     []string{"dependency:redis", "dependency:sql"},
				CalleeFunctionIds: []string{"fn:orders.StoreOrder"},
			},
			{
				Id:                "fn:orders.StoreOrder",
				Name:              "StoreOrder",
				QualifiedName:     "orders.StoreOrder",
				PackageId:         "pkg:orders",
				PackagePath:       "example.com/orders",
				Signature:         "func() error",
				SourceLocation:    &SourceLocation{RelativePath: "store.go", StartLine: 3, StartColumn: 1, EndLine: 9, EndColumn: 2},
				Exported:          true,
				CallerFunctionIds: []string{"fn:orders.CreateOrder"},
				InputEndpointIds:  []string{"endpoint:cron:cleanup"},
				DependencyIds:     []string{"dependency:sql"},
				OutputEndpointIds: []string{"endpoint:cron:cleanup"},
			},
		},
		Endpoints: []*Endpoint{
			{
				Id:             "endpoint:http:create-order",
				Kind:           EndpointKind_HTTP_HANDLER,
				Name:           "CreateOrder",
				FunctionId:     "fn:orders.CreateOrder",
				HttpMethod:     "POST",
				HttpPath:       "/orders",
				SourceLocation: &SourceLocation{RelativePath: "handler.go", StartLine: 11, StartColumn: 2, EndLine: 11, EndColumn: 33},
			},
			{
				Id:             "endpoint:grpc:create-order",
				Kind:           EndpointKind_GRPC_HANDLER,
				Name:           "CreateOrder",
				FunctionId:     "fn:orders.CreateOrder",
				GrpcService:    "orders.OrderService",
				GrpcMethod:     "CreateOrder",
				SourceLocation: &SourceLocation{RelativePath: "handler.go", StartLine: 15, StartColumn: 2, EndLine: 15, EndColumn: 60},
			},
			{
				Id:             "endpoint:cron:cleanup",
				Kind:           EndpointKind_CRON_JOB,
				Name:           "cleanup",
				FunctionId:     "fn:orders.StoreOrder",
				CronSchedule:   "0 3 * * *",
				SourceLocation: &SourceLocation{RelativePath: "store.go", StartLine: 5, StartColumn: 2, EndLine: 5, EndColumn: 31},
			},
		},
		Dependencies: []*Dependency{
			{
				Id:            "dependency:redis",
				Kind:          DependencyKind_REDIS,
				Name:          "redis.Client.Get",
				FunctionId:    "fn:orders.CreateOrder",
				Operation:     "get",
				TargetPackage: "github.com/redis/go-redis/v9",
				Resource:      "orders",
				Value:         "order:{id}",
				ValueIsStatic: true,
				SourceLocation: &SourceLocation{
					RelativePath: "handler.go",
					StartLine:    12,
					StartColumn:  3,
					EndLine:      12,
					EndColumn:    44,
				},
			},
			{
				Id:             "dependency:sql",
				Kind:           DependencyKind_SQL,
				Name:           "database/sql.Query",
				FunctionId:     "fn:orders.StoreOrder",
				Operation:      "query",
				TargetPackage:  "database/sql",
				Resource:       "orders",
				Value:          "INSERT INTO orders (id) VALUES (?)",
				ValueIsStatic:  true,
				SourceLocation: &SourceLocation{RelativePath: "store.go", StartLine: 6, StartColumn: 3, EndLine: 6, EndColumn: 62},
			},
		},
		CallEdges: []*CallEdge{{
			Id:               "edge:create-order-store",
			CallerFunctionId: "fn:orders.CreateOrder",
			CalleeFunctionId: "fn:orders.StoreOrder",
			Resolution:       CallResolution_RESOLVED,
			SourceLocation:   &SourceLocation{RelativePath: "handler.go", StartLine: 13, StartColumn: 3, EndLine: 13, EndColumn: 31},
		}},
		Diagnostics: []*Diagnostic{
			{
				Severity: DiagnosticSeverity_WARNING,
				Code:     "UNRESOLVED_CALL",
				Message:  "interface call target could not be resolved",
			},
			{
				Severity: DiagnosticSeverity_INFO,
				Code:     "DYNAMIC_REDIS_KEY",
				Message:  "redis key is dynamic",
			},
		},
	}
}

// fullDocument exercises every IR entity kind the GenerationPlan target
// vocabulary supports.
func fullDocument() *ObservabilityDocument {
	document := phase0TestDocument()
	document.Dependencies = append(document.Dependencies,
		&Dependency{Id: "dependency:kafka-producer", Kind: DependencyKind_KAFKA_PRODUCER, Name: "sarama.Producer.SendMessage", FunctionId: "fn:orders.CreateOrder", Operation: "send"},
		&Dependency{Id: "dependency:kafka-consumer", Kind: DependencyKind_KAFKA_CONSUMER, Name: "sarama.Consumer.ConsumePartition", FunctionId: "fn:orders.StoreOrder", Operation: "consume"},
		&Dependency{Id: "dependency:http-client", Kind: DependencyKind_HTTP_CLIENT, Name: "http.Client.Do", FunctionId: "fn:orders.CreateOrder", Operation: "post"},
		&Dependency{Id: "dependency:rpc-client", Kind: DependencyKind_RPC_CLIENT, Name: "grpc.ClientConn.Invoke", FunctionId: "fn:orders.StoreOrder", Operation: "invoke"},
	)
	document.CallEdges = append(document.CallEdges,
		&CallEdge{Id: "edge:store-order-notify", CallerFunctionId: "fn:orders.StoreOrder", CalleeFunctionId: "fn:orders.CreateOrder", Resolution: CallResolution_UNRESOLVED},
	)
	return document
}

// fullGenerationPlan covers all four metric types, all five span kinds, a
// log plan with correlation and redaction, every ValueSource, every
// cardinality class and both plan diagnostic severities.
func fullGenerationPlan() *GenerationPlan {
	statusBinding := func(path string, required bool) *ValueBinding {
		return &ValueBinding{
			Path:        path,
			Source:      ValueSource_VALUE_SOURCE_RUNTIME_RESULT,
			Type:        ValueType_VALUE_TYPE_STATUS,
			Required:    required,
			Cardinality: CardinalityClass_CARDINALITY_CLASS_LOW,
		}
	}
	serviceAttribute := &AttributeBinding{
		Key: "service",
		Value: &ValueBinding{
			Path:        "service.name",
			Source:      ValueSource_VALUE_SOURCE_IR_CONSTANT,
			Type:        ValueType_VALUE_TYPE_STRING,
			Cardinality: CardinalityClass_CARDINALITY_CLASS_CONSTANT,
		},
	}
	return &GenerationPlan{
		SchemaVersion:         GenerationPlanSchemaVersion,
		SourceIrSchemaVersion: "v1",
		ServiceName:           "orders",
		Metrics: []*MetricPlan{
			{
				Id:          "metric:http:create-order:requests_total",
				Name:        "http_requests_total",
				Type:        MetricType_METRIC_TYPE_COUNTER,
				Unit:        "{request}",
				Description: "Number of HTTP requests completed",
				Target:      &TargetRef{Kind: TargetKind_TARGET_KIND_ENDPOINT, Id: "endpoint:http:create-order"},
				Trigger:     &Trigger{Phase: TriggerPhase_TRIGGER_PHASE_END},
				Value: &ValueBinding{
					Path:        "plan.constant.one",
					Source:      ValueSource_VALUE_SOURCE_PLAN_CONSTANT,
					Type:        ValueType_VALUE_TYPE_INT64,
					Required:    true,
					Cardinality: CardinalityClass_CARDINALITY_CLASS_CONSTANT,
				},
				Attributes: []*AttributeBinding{
					serviceAttribute,
					{
						Key: "operation",
						Value: &ValueBinding{
							Path:        "endpoint.operation",
							Source:      ValueSource_VALUE_SOURCE_PLAN_CONSTANT,
							Type:        ValueType_VALUE_TYPE_STRING,
							Required:    true,
							Cardinality: CardinalityClass_CARDINALITY_CLASS_CONSTANT,
						},
					},
					{Key: "status", Value: statusBinding("runtime.operation.status", false)},
				},
			},
			{
				Id:          "metric:http:create-order:duration_seconds",
				Name:        "http_request_duration_seconds",
				Type:        MetricType_METRIC_TYPE_HISTOGRAM,
				Unit:        "s",
				Description: "Duration of HTTP requests completed",
				Target:      &TargetRef{Kind: TargetKind_TARGET_KIND_ENDPOINT, Id: "endpoint:http:create-order"},
				Trigger:     &Trigger{Phase: TriggerPhase_TRIGGER_PHASE_END},
				Value: &ValueBinding{
					Path:        "runtime.operation.duration_seconds",
					Source:      ValueSource_VALUE_SOURCE_RUNTIME_RESULT,
					Type:        ValueType_VALUE_TYPE_DOUBLE,
					Required:    true,
					Cardinality: CardinalityClass_CARDINALITY_CLASS_LOW,
				},
				Attributes: []*AttributeBinding{serviceAttribute, {Key: "status", Value: statusBinding("runtime.operation.status", false)}},
			},
			{
				Id:          "metric:http:create-order:in_flight",
				Name:        "http_requests_in_flight",
				Type:        MetricType_METRIC_TYPE_GAUGE,
				Unit:        "{request}",
				Description: "HTTP requests currently in flight",
				Target:      &TargetRef{Kind: TargetKind_TARGET_KIND_ENDPOINT, Id: "endpoint:http:create-order"},
				Trigger:     &Trigger{Phase: TriggerPhase_TRIGGER_PHASE_STATE_CHANGE},
				Value: &ValueBinding{
					Path:        "runtime.operation.in_flight",
					Source:      ValueSource_VALUE_SOURCE_RUNTIME_RESULT,
					Type:        ValueType_VALUE_TYPE_INT64,
					Required:    true,
					Cardinality: CardinalityClass_CARDINALITY_CLASS_LOW,
				},
				Attributes: []*AttributeBinding{serviceAttribute},
			},
			{
				Id:          "metric:sql:store:duration_seconds_summary",
				Name:        "sql_operations_duration_seconds_summary",
				Type:        MetricType_METRIC_TYPE_SUMMARY,
				Unit:        "s",
				Description: "Duration of SQL operations summarized",
				Target:      &TargetRef{Kind: TargetKind_TARGET_KIND_DEPENDENCY, Id: "dependency:sql"},
				Trigger:     &Trigger{Phase: TriggerPhase_TRIGGER_PHASE_END},
				Value: &ValueBinding{
					Path:        "runtime.operation.duration_seconds",
					Source:      ValueSource_VALUE_SOURCE_RUNTIME_RESULT,
					Type:        ValueType_VALUE_TYPE_DOUBLE,
					Required:    true,
					Cardinality: CardinalityClass_CARDINALITY_CLASS_LOW,
				},
				Attributes: []*AttributeBinding{serviceAttribute, {Key: "status", Value: statusBinding("runtime.operation.status", false)}},
			},
			{
				Id:          "metric:redis:get:operations_total",
				Name:        "redis_operations_total",
				Type:        MetricType_METRIC_TYPE_COUNTER,
				Unit:        "{operation}",
				Description: "Number of Redis operations completed",
				Target:      &TargetRef{Kind: TargetKind_TARGET_KIND_DEPENDENCY, Id: "dependency:redis"},
				Trigger:     &Trigger{Phase: TriggerPhase_TRIGGER_PHASE_END},
				Value: &ValueBinding{
					Path:        "plan.constant.one",
					Source:      ValueSource_VALUE_SOURCE_PLAN_CONSTANT,
					Type:        ValueType_VALUE_TYPE_INT64,
					Required:    true,
					Cardinality: CardinalityClass_CARDINALITY_CLASS_CONSTANT,
				},
				Attributes: []*AttributeBinding{
					serviceAttribute,
					{
						Key: "status",
						Value: &ValueBinding{
							Path:        "runtime.operation.status",
							Source:      ValueSource_VALUE_SOURCE_RUNTIME_RESULT,
							Type:        ValueType_VALUE_TYPE_STATUS,
							Required:    false,
							Fallback:    "unknown",
							Cardinality: CardinalityClass_CARDINALITY_CLASS_LOW,
						},
					},
				},
			},
		},
		Spans: []*SpanPlan{
			{
				Id:           "span:http:create-order:root",
				Name:         "POST /orders",
				Kind:         SpanKind_SPAN_KIND_SERVER,
				Target:       &TargetRef{Kind: TargetKind_TARGET_KIND_ENDPOINT, Id: "endpoint:http:create-order"},
				StartTrigger: &Trigger{Phase: TriggerPhase_TRIGGER_PHASE_START},
				EndTrigger:   &Trigger{Phase: TriggerPhase_TRIGGER_PHASE_END},
				Parent:       &ParentStrategy{Mode: ParentStrategyMode_PARENT_STRATEGY_MODE_EXTRACT_OR_ROOT, Carrier: CarrierType_CARRIER_TYPE_HTTP_HEADERS},
				Attributes: []*AttributeBinding{
					{
						Key: "http.request.method",
						Value: &ValueBinding{
							Path:        "endpoint.http_method",
							Source:      ValueSource_VALUE_SOURCE_IR_CONSTANT,
							Type:        ValueType_VALUE_TYPE_STRING,
							Required:    true,
							Cardinality: CardinalityClass_CARDINALITY_CLASS_CONSTANT,
						},
					},
					{
						Key: "http.route",
						Value: &ValueBinding{
							Path:        "endpoint.http_path",
							Source:      ValueSource_VALUE_SOURCE_IR_CONSTANT,
							Type:        ValueType_VALUE_TYPE_STRING,
							Required:    true,
							Cardinality: CardinalityClass_CARDINALITY_CLASS_CONSTANT,
						},
					},
					{
						Key: "service.version",
						Value: &ValueBinding{
							Path:        "runtime.resource.service.version",
							Source:      ValueSource_VALUE_SOURCE_RUNTIME_RESOURCE,
							Type:        ValueType_VALUE_TYPE_STRING,
							Required:    false,
							Fallback:    "unknown",
							Cardinality: CardinalityClass_CARDINALITY_CLASS_CONSTANT,
						},
					},
					{
						Key: "service.name",
						Value: &ValueBinding{
							Path:        "service.name",
							Source:      ValueSource_VALUE_SOURCE_IR_CONSTANT,
							Type:        ValueType_VALUE_TYPE_STRING,
							Required:    true,
							Cardinality: CardinalityClass_CARDINALITY_CLASS_CONSTANT,
						},
					},
					{
						Key: "server.address",
						Value: &ValueBinding{
							Path:        "runtime.context.host",
							Source:      ValueSource_VALUE_SOURCE_RUNTIME_CONTEXT,
							Type:        ValueType_VALUE_TYPE_STRING,
							Required:    false,
							Cardinality: CardinalityClass_CARDINALITY_CLASS_HIGH,
						},
					},
				},
				Status: &StatusPolicy{
					Ok:        StatusSetting_STATUS_SETTING_UNSET,
					Error:     StatusSetting_STATUS_SETTING_ERROR,
					Timeout:   StatusSetting_STATUS_SETTING_ERROR,
					Cancelled: StatusSetting_STATUS_SETTING_ERROR,
					Unknown:   StatusSetting_STATUS_SETTING_UNSET,
				},
				Events: []*SpanEvent{
					{
						Id:   "span:http:create-order:root:exception",
						Name: "exception",
						Statuses: []RuntimeStatus{
							RuntimeStatus_RUNTIME_STATUS_ERROR,
						},
						Attributes: []*AttributeBinding{{
							Key: "exception.type",
							Value: &ValueBinding{
								Path:        "runtime.operation.error_type",
								Source:      ValueSource_VALUE_SOURCE_RUNTIME_RESULT,
								Type:        ValueType_VALUE_TYPE_STRING,
								Required:    false,
								Cardinality: CardinalityClass_CARDINALITY_CLASS_LOW,
							},
						}},
					},
					{
						Id:       "span:http:create-order:root:timeout",
						Name:     "timeout",
						Statuses: []RuntimeStatus{RuntimeStatus_RUNTIME_STATUS_TIMEOUT},
					},
				},
			},
			{
				Id:           "span:sql:store:query",
				Name:         "db query",
				Kind:         SpanKind_SPAN_KIND_CLIENT,
				Target:       &TargetRef{Kind: TargetKind_TARGET_KIND_DEPENDENCY, Id: "dependency:sql"},
				StartTrigger: &Trigger{Phase: TriggerPhase_TRIGGER_PHASE_START},
				EndTrigger:   &Trigger{Phase: TriggerPhase_TRIGGER_PHASE_END},
				Parent:       &ParentStrategy{Mode: ParentStrategyMode_PARENT_STRATEGY_MODE_CURRENT_CONTEXT},
				Attributes: []*AttributeBinding{
					{Key: "db.system", Value: &ValueBinding{Path: "dependency.system", Source: ValueSource_VALUE_SOURCE_PLAN_CONSTANT, Type: ValueType_VALUE_TYPE_STRING, Required: true, Cardinality: CardinalityClass_CARDINALITY_CLASS_CONSTANT}},
					{Key: "db.operation", Value: &ValueBinding{Path: "dependency.operation", Source: ValueSource_VALUE_SOURCE_IR_CONSTANT, Type: ValueType_VALUE_TYPE_STRING, Required: true, Cardinality: CardinalityClass_CARDINALITY_CLASS_LOW}},
				},
				Status: &StatusPolicy{
					Ok:        StatusSetting_STATUS_SETTING_UNSET,
					Error:     StatusSetting_STATUS_SETTING_ERROR,
					Timeout:   StatusSetting_STATUS_SETTING_ERROR,
					Cancelled: StatusSetting_STATUS_SETTING_ERROR,
					Unknown:   StatusSetting_STATUS_SETTING_UNSET,
				},
			},
			{
				Id:           "span:kafka:create-order:send",
				Name:         "kafka send",
				Kind:         SpanKind_SPAN_KIND_PRODUCER,
				Target:       &TargetRef{Kind: TargetKind_TARGET_KIND_DEPENDENCY, Id: "dependency:kafka-producer"},
				StartTrigger: &Trigger{Phase: TriggerPhase_TRIGGER_PHASE_START},
				EndTrigger:   &Trigger{Phase: TriggerPhase_TRIGGER_PHASE_END},
				Parent:       &ParentStrategy{Mode: ParentStrategyMode_PARENT_STRATEGY_MODE_CURRENT_CONTEXT},
				Status:       &StatusPolicy{Ok: StatusSetting_STATUS_SETTING_UNSET, Error: StatusSetting_STATUS_SETTING_ERROR, Timeout: StatusSetting_STATUS_SETTING_ERROR, Cancelled: StatusSetting_STATUS_SETTING_ERROR, Unknown: StatusSetting_STATUS_SETTING_UNSET},
			},
			{
				Id:           "span:kafka:store:consume",
				Name:         "kafka consume",
				Kind:         SpanKind_SPAN_KIND_CONSUMER,
				Target:       &TargetRef{Kind: TargetKind_TARGET_KIND_DEPENDENCY, Id: "dependency:kafka-consumer"},
				StartTrigger: &Trigger{Phase: TriggerPhase_TRIGGER_PHASE_START},
				EndTrigger:   &Trigger{Phase: TriggerPhase_TRIGGER_PHASE_END},
				Parent:       &ParentStrategy{Mode: ParentStrategyMode_PARENT_STRATEGY_MODE_CURRENT_CONTEXT},
				Status:       &StatusPolicy{Ok: StatusSetting_STATUS_SETTING_UNSET, Error: StatusSetting_STATUS_SETTING_ERROR, Timeout: StatusSetting_STATUS_SETTING_ERROR, Cancelled: StatusSetting_STATUS_SETTING_ERROR, Unknown: StatusSetting_STATUS_SETTING_UNSET},
			},
			{
				Id:           "span:edge:create-order-store:internal",
				Name:         "orders.StoreOrder",
				Kind:         SpanKind_SPAN_KIND_INTERNAL,
				Target:       &TargetRef{Kind: TargetKind_TARGET_KIND_CALL_EDGE, Id: "edge:create-order-store"},
				StartTrigger: &Trigger{Phase: TriggerPhase_TRIGGER_PHASE_START},
				EndTrigger:   &Trigger{Phase: TriggerPhase_TRIGGER_PHASE_END},
				Parent:       &ParentStrategy{Mode: ParentStrategyMode_PARENT_STRATEGY_MODE_CURRENT_CONTEXT},
				Status:       &StatusPolicy{Ok: StatusSetting_STATUS_SETTING_UNSET, Error: StatusSetting_STATUS_SETTING_ERROR, Timeout: StatusSetting_STATUS_SETTING_ERROR, Cancelled: StatusSetting_STATUS_SETTING_ERROR, Unknown: StatusSetting_STATUS_SETTING_UNSET},
			},
		},
		Logs: []*LogPlan{
			{
				Id:                "log:http:create-order:completed",
				EventName:         "http.request.completed",
				Severity:          LogSeverity_LOG_SEVERITY_INFO,
				Target:            &TargetRef{Kind: TargetKind_TARGET_KIND_ENDPOINT, Id: "endpoint:http:create-order"},
				Trigger:           &Trigger{Phase: TriggerPhase_TRIGGER_PHASE_END},
				RedactionPolicyId: "default",
				CorrelationFields: []string{"request_id", "trace_id", "span_id"},
				Fields: []*FieldBinding{
					{Key: "event.name", Value: &ValueBinding{Path: "plan.constant.event", Source: ValueSource_VALUE_SOURCE_PLAN_CONSTANT, Type: ValueType_VALUE_TYPE_STRING, Required: true, Cardinality: CardinalityClass_CARDINALITY_CLASS_CONSTANT}},
					{Key: "service", Value: &ValueBinding{Path: "service.name", Source: ValueSource_VALUE_SOURCE_IR_CONSTANT, Type: ValueType_VALUE_TYPE_STRING, Required: true, Cardinality: CardinalityClass_CARDINALITY_CLASS_CONSTANT}},
					{Key: "status", Value: statusBinding("runtime.operation.status", false)},
					{Key: "version", Value: &ValueBinding{Path: "runtime.resource.service.version", Source: ValueSource_VALUE_SOURCE_RUNTIME_RESOURCE, Type: ValueType_VALUE_TYPE_STRING, Required: false, Fallback: "unknown", Cardinality: CardinalityClass_CARDINALITY_CLASS_CONSTANT}},
					{Key: "trace_id", Value: &ValueBinding{Path: "runtime.context.trace_id", Source: ValueSource_VALUE_SOURCE_RUNTIME_CONTEXT, Type: ValueType_VALUE_TYPE_STRING, Required: true, Cardinality: CardinalityClass_CARDINALITY_CLASS_LOW}},
				},
			},
			{
				Id:                "log:sql:store:failed",
				EventName:         "dependency.operation.failed",
				Severity:          LogSeverity_LOG_SEVERITY_ERROR,
				Target:            &TargetRef{Kind: TargetKind_TARGET_KIND_DEPENDENCY, Id: "dependency:sql"},
				Trigger:           &Trigger{Phase: TriggerPhase_TRIGGER_PHASE_END},
				RedactionPolicyId: "default",
				Fields: []*FieldBinding{
					{Key: "status", Value: statusBinding("runtime.operation.status", true)},
					{Key: "error.type", Value: &ValueBinding{Path: "runtime.operation.error_type", Source: ValueSource_VALUE_SOURCE_RUNTIME_RESULT, Type: ValueType_VALUE_TYPE_STRING, Required: false, Cardinality: CardinalityClass_CARDINALITY_CLASS_LOW}},
				},
			},
		},
		Diagnostics: []*PlanDiagnostic{
			{
				Severity: PlanSeverity_PLAN_SEVERITY_WARNING,
				Code:     "GEN_NAME_COLLISION",
				Message:  "normalized name collided; disambiguated with stable suffix",
			},
			{
				Severity: PlanSeverity_PLAN_SEVERITY_ERROR,
				Code:     "GEN_CARDINALITY_LIMIT_EXCEEDED",
				Message:  "estimated series exceeds policy limit",
			},
		},
	}
}

func TestGenerationPlanBinaryRoundTrip(t *testing.T) {
	want := fullGenerationPlan()
	binary, err := proto.MarshalOptions{Deterministic: true}.Marshal(want)
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}
	got := new(GenerationPlan)
	if err := proto.Unmarshal(binary, got); err != nil {
		t.Fatalf("unmarshal plan: %v", err)
	}
	if !proto.Equal(want, got) {
		t.Fatalf("binary round-trip mismatch:\nwant: %v\ngot:  %v", want, got)
	}
}

func TestGenerationPlanJSONRoundTrip(t *testing.T) {
	want := fullGenerationPlan()
	json, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(want)
	if err != nil {
		t.Fatalf("marshal plan JSON: %v", err)
	}
	got := new(GenerationPlan)
	if err := protojson.Unmarshal(json, got); err != nil {
		t.Fatalf("unmarshal plan JSON: %v", err)
	}
	if !proto.Equal(want, got) {
		t.Fatalf("JSON round-trip mismatch:\nwant: %v\ngot:  %v", want, got)
	}
}

func TestGenerationPlanDeterministicSerialization(t *testing.T) {
	plan := fullGenerationPlan()
	binary, err := proto.MarshalOptions{Deterministic: true}.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}
	for i := 0; i < 10; i++ {
		next, err := proto.MarshalOptions{Deterministic: true}.Marshal(plan)
		if err != nil {
			t.Fatalf("marshal plan on run %d: %v", i, err)
		}
		if !bytes.Equal(binary, next) {
			t.Fatalf("binary serialization differs on run %d", i)
		}
	}
	json, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal plan JSON: %v", err)
	}
	for i := 0; i < 10; i++ {
		next, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(plan)
		if err != nil {
			t.Fatalf("marshal plan JSON on run %d: %v", i, err)
		}
		if !bytes.Equal(json, next) {
			t.Fatalf("JSON serialization differs on run %d", i)
		}
	}
}

// enumValue is any generated protobuf enum type.
type enumValue interface {
	~int32
	protoreflect.Enum
	fmt.Stringer
}

// assertEnumRoundTrip verifies every given enum value survives binary and
// JSON round-trips and keeps its stable name in JSON output.
func assertEnumRoundTrip[T enumValue](t *testing.T, label string, build func(value T) proto.Message, values []T) {
	t.Helper()
	for _, value := range values {
		want := build(value)
		binary, err := proto.MarshalOptions{Deterministic: true}.Marshal(want)
		if err != nil {
			t.Fatalf("%s: marshal %v: %v", label, value, err)
		}
		fromBinary := want.ProtoReflect().Type().New().Interface()
		if err := proto.Unmarshal(binary, fromBinary); err != nil {
			t.Fatalf("%s: unmarshal %v: %v", label, value, err)
		}
		if !proto.Equal(want, fromBinary) {
			t.Fatalf("%s: binary round-trip lost value %v", label, value)
		}

		json, err := protojson.Marshal(want)
		if err != nil {
			t.Fatalf("%s: marshal JSON %v: %v", label, value, err)
		}
		if !bytes.Contains(json, []byte(value.String())) {
			t.Fatalf("%s: JSON does not contain stable enum name %q: %s", label, value, json)
		}
		fromJSON := want.ProtoReflect().Type().New().Interface()
		if err := protojson.Unmarshal(json, fromJSON); err != nil {
			t.Fatalf("%s: unmarshal JSON %v: %v", label, value, err)
		}
		if !proto.Equal(want, fromJSON) {
			t.Fatalf("%s: JSON round-trip lost value %v", label, value)
		}
	}
}

func TestGenerationPlanEnumsRoundTrip(t *testing.T) {
	assertEnumRoundTrip(t, "MetricType", func(value MetricType) proto.Message { return &MetricPlan{Type: value} }, []MetricType{
		MetricType_METRIC_TYPE_COUNTER,
		MetricType_METRIC_TYPE_HISTOGRAM,
		MetricType_METRIC_TYPE_GAUGE,
		MetricType_METRIC_TYPE_SUMMARY,
	})
	assertEnumRoundTrip(t, "SpanKind", func(value SpanKind) proto.Message { return &SpanPlan{Kind: value} }, []SpanKind{
		SpanKind_SPAN_KIND_SERVER,
		SpanKind_SPAN_KIND_CLIENT,
		SpanKind_SPAN_KIND_PRODUCER,
		SpanKind_SPAN_KIND_CONSUMER,
		SpanKind_SPAN_KIND_INTERNAL,
	})
	assertEnumRoundTrip(t, "TargetKind", func(value TargetKind) proto.Message { return &TargetRef{Kind: value} }, []TargetKind{
		TargetKind_TARGET_KIND_ENDPOINT,
		TargetKind_TARGET_KIND_FUNCTION,
		TargetKind_TARGET_KIND_DEPENDENCY,
		TargetKind_TARGET_KIND_CALL_EDGE,
	})
	assertEnumRoundTrip(t, "TriggerPhase", func(value TriggerPhase) proto.Message { return &Trigger{Phase: value} }, []TriggerPhase{
		TriggerPhase_TRIGGER_PHASE_START,
		TriggerPhase_TRIGGER_PHASE_END,
		TriggerPhase_TRIGGER_PHASE_STATE_CHANGE,
	})
	assertEnumRoundTrip(t, "ValueSource", func(value ValueSource) proto.Message { return &ValueBinding{Source: value} }, []ValueSource{
		ValueSource_VALUE_SOURCE_PLAN_CONSTANT,
		ValueSource_VALUE_SOURCE_IR_CONSTANT,
		ValueSource_VALUE_SOURCE_RUNTIME_RESOURCE,
		ValueSource_VALUE_SOURCE_RUNTIME_CONTEXT,
		ValueSource_VALUE_SOURCE_RUNTIME_RESULT,
	})
	assertEnumRoundTrip(t, "ValueType", func(value ValueType) proto.Message { return &ValueBinding{Type: value} }, []ValueType{
		ValueType_VALUE_TYPE_STRING,
		ValueType_VALUE_TYPE_INT64,
		ValueType_VALUE_TYPE_DOUBLE,
		ValueType_VALUE_TYPE_BOOL,
		ValueType_VALUE_TYPE_STATUS,
	})
	assertEnumRoundTrip(t, "CardinalityClass", func(value CardinalityClass) proto.Message { return &ValueBinding{Cardinality: value} }, []CardinalityClass{
		CardinalityClass_CARDINALITY_CLASS_CONSTANT,
		CardinalityClass_CARDINALITY_CLASS_LOW,
		CardinalityClass_CARDINALITY_CLASS_HIGH,
	})
	assertEnumRoundTrip(t, "ParentStrategyMode", func(value ParentStrategyMode) proto.Message { return &ParentStrategy{Mode: value} }, []ParentStrategyMode{
		ParentStrategyMode_PARENT_STRATEGY_MODE_EXTRACT_OR_ROOT,
		ParentStrategyMode_PARENT_STRATEGY_MODE_NEW_ROOT,
		ParentStrategyMode_PARENT_STRATEGY_MODE_CURRENT_CONTEXT,
		ParentStrategyMode_PARENT_STRATEGY_MODE_STATIC,
	})
	assertEnumRoundTrip(t, "StatusSetting", func(value StatusSetting) proto.Message { return &StatusPolicy{Ok: value} }, []StatusSetting{
		StatusSetting_STATUS_SETTING_UNSET,
		StatusSetting_STATUS_SETTING_ERROR,
		StatusSetting_STATUS_SETTING_OK,
	})
	assertEnumRoundTrip(t, "RuntimeStatus", func(value RuntimeStatus) proto.Message { return &SpanEvent{Statuses: []RuntimeStatus{value}} }, []RuntimeStatus{
		RuntimeStatus_RUNTIME_STATUS_OK,
		RuntimeStatus_RUNTIME_STATUS_ERROR,
		RuntimeStatus_RUNTIME_STATUS_CANCELLED,
		RuntimeStatus_RUNTIME_STATUS_TIMEOUT,
		RuntimeStatus_RUNTIME_STATUS_UNKNOWN,
	})
	assertEnumRoundTrip(t, "LogSeverity", func(value LogSeverity) proto.Message { return &LogPlan{Severity: value} }, []LogSeverity{
		LogSeverity_LOG_SEVERITY_INFO,
		LogSeverity_LOG_SEVERITY_WARN,
		LogSeverity_LOG_SEVERITY_ERROR,
	})
	assertEnumRoundTrip(t, "PlanSeverity", func(value PlanSeverity) proto.Message { return &PlanDiagnostic{Severity: value} }, []PlanSeverity{
		PlanSeverity_PLAN_SEVERITY_INFO,
		PlanSeverity_PLAN_SEVERITY_WARNING,
		PlanSeverity_PLAN_SEVERITY_ERROR,
	})
}

// TestPhase0BinaryFixtureBackwardCompatible implements AC1 for the binary
// contract: a Phase 0 document without generation_plan unmarshals, keeps
// every original field, keeps the plan absent and re-serializes to the
// exact fixture bytes.
func TestPhase0BinaryFixtureBackwardCompatible(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("testdata", "phase0-document.bin"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	document := new(ObservabilityDocument)
	if err := proto.Unmarshal(fixture, document); err != nil {
		t.Fatalf("unmarshal Phase 0 fixture: %v", err)
	}
	if document.GetGenerationPlan() != nil {
		t.Fatalf("Phase 0 fixture must not contain a generation plan")
	}
	if !proto.Equal(document, phase0TestDocument()) {
		t.Fatalf("Phase 0 fixture fields changed: want %v, got %v", phase0TestDocument(), document)
	}
	reSerialized, err := proto.MarshalOptions{Deterministic: true}.Marshal(document)
	if err != nil {
		t.Fatalf("re-marshal Phase 0 fixture: %v", err)
	}
	if !bytes.Equal(reSerialized, fixture) {
		t.Fatalf("Phase 0 fixture re-serialization differs from original bytes")
	}
}

// TestPhase0JSONFixtureBackwardCompatible implements AC1 for the JSON
// contract. The comparison is semantic, not byte-wise: protobuf-go's
// protojson encoder deliberately varies whitespace per binary build, so the
// JSON contract is defined by parse result, never by raw bytes.
func TestPhase0JSONFixtureBackwardCompatible(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("testdata", "phase0-document.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	document := new(ObservabilityDocument)
	if err := protojson.Unmarshal(fixture, document); err != nil {
		t.Fatalf("unmarshal Phase 0 JSON fixture: %v", err)
	}
	if document.GetGenerationPlan() != nil {
		t.Fatalf("Phase 0 JSON fixture must not contain a generation plan")
	}
	if !proto.Equal(document, phase0TestDocument()) {
		t.Fatalf("Phase 0 JSON fixture fields changed: want %v, got %v", phase0TestDocument(), document)
	}
	reSerialized, err := protojson.MarshalOptions{UseProtoNames: true, Indent: "  "}.Marshal(document)
	if err != nil {
		t.Fatalf("re-marshal Phase 0 JSON fixture: %v", err)
	}
	reparsed := new(ObservabilityDocument)
	if err := protojson.Unmarshal(reSerialized, reparsed); err != nil {
		t.Fatalf("re-parse Phase 0 JSON fixture: %v", err)
	}
	if !proto.Equal(document, reparsed) {
		t.Fatalf("Phase 0 JSON fixture re-serialization changed semantics")
	}
}

// TestScanJSONContractOmitsPlan implements AC4: the scan JSON contract
// emits no generation content when the plan is absent.
func TestScanJSONContractOmitsPlan(t *testing.T) {
	document := phase0TestDocument()
	json, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(document)
	if err != nil {
		t.Fatalf("marshal document JSON: %v", err)
	}
	for _, token := range []string{"generation", "generationPlan", "generation_plan"} {
		if strings.Contains(string(json), token) {
			t.Fatalf("scan JSON contract leaks generation token %q: %s", token, json)
		}
	}
}

// TestGenerationPlanForbidsTimeAndMachineSpecificFields guards the schema
// against fields that would break determinism: timestamps, random
// identifiers, machine identities and absolute paths must never exist on
// the GenerationPlan contract.
func TestGenerationPlanForbidsTimeAndMachineSpecificFields(t *testing.T) {
	forbidden := []string{
		"timestamp", "created", "updated",
		"uuid", "random", "nonce",
		"hostname", "machine", "host",
		"absolute", "username", "user",
	}
	var visit func(message protoreflect.MessageDescriptor)
	visit = func(message protoreflect.MessageDescriptor) {
		fields := message.Fields()
		for i := 0; i < fields.Len(); i++ {
			name := strings.ToLower(string(fields.Get(i).Name()))
			for _, token := range forbidden {
				if strings.Contains(name, token) {
					t.Errorf("GenerationPlan field %q contains forbidden token %q", fields.Get(i).Name(), token)
				}
			}
		}
		messages := message.Messages()
		for i := 0; i < messages.Len(); i++ {
			visit(messages.Get(i))
		}
	}
	// Every message in generation.proto is a top-level declaration, so the
	// whole contract is covered by iterating the file's messages.
	messages := File_ir_v1_generation_proto.Messages()
	for i := 0; i < messages.Len(); i++ {
		visit(messages.Get(i))
	}
}
