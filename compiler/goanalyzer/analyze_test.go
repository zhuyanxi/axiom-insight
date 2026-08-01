package goanalyzer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zhuyanxi/axiom-insight/compiler/semantic"
)

func TestAnalyzeLoadsMultiplePackagesAndResolvesService(t *testing.T) {
	root := writeProject(t, map[string]string{
		"go.mod":                          "module example.com/orders\n\ngo 1.26.1\n",
		"si.yaml":                         "service:\n  name: order-service\n",
		"main.go":                         "package orders\n\nfunc CreateOrder() {}\n",
		"internal/inventory/inventory.go": "package inventory\n\nfunc Reserve() {}\n",
		".hidden/ignored.go":              "package ignored\n\nfunc Hidden() {}\n",
		"vendor/thirdparty/ignored.go":    "package thirdparty\n\nfunc Ignored() {}\n",
		"main_test.go":                    "package orders\n\nfunc TestCreateOrder() {}\n",
	})

	document, err := Analyze(context.Background(), root, Options{})
	if err != nil {
		t.Fatalf("analyze project: %v", err)
	}
	if document.Service.Name != "order-service" {
		t.Fatalf("service name = %q, want order-service", document.Service.Name)
	}
	if document.Service.ModulePath != "example.com/orders" {
		t.Fatalf("module path = %q, want example.com/orders", document.Service.ModulePath)
	}
	if len(document.Packages) != 2 {
		t.Fatalf("package count = %d, want 2: %+v", len(document.Packages), document.Packages)
	}
	for _, pkg := range document.Packages {
		for _, file := range pkg.Files {
			if file == "main_test.go" || file == ".hidden/ignored.go" || file == "vendor/thirdparty/ignored.go" {
				t.Fatalf("excluded file loaded: %q", file)
			}
			if filepath.IsAbs(file) {
				t.Fatalf("package file is absolute: %q", file)
			}
		}
	}
}

func TestLoadPackagesRequestsSyntaxTypesAndImports(t *testing.T) {
	root := writeProject(t, map[string]string{
		"go.mod": "module example.com/typed\n\ngo 1.26.1\n",
		"run.go": "package typed\n\nimport \"fmt\"\n\nfunc Run() { fmt.Println(\"ok\") }\n",
	})

	loaded, err := loadPackages(context.Background(), root, Options{})
	if err != nil {
		t.Fatalf("load packages: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("package count = %d, want 1", len(loaded))
	}
	pkg := loaded[0]
	if len(pkg.Syntax) == 0 {
		t.Fatal("syntax was not loaded")
	}
	if pkg.Types == nil {
		t.Fatal("types were not loaded")
	}
	if pkg.TypesInfo == nil {
		t.Fatal("types info was not loaded")
	}
	if len(pkg.Imports) == 0 {
		t.Fatal("imports were not loaded")
	}
}

func TestAnalyzeIncludesTestFilesWhenRequested(t *testing.T) {
	root := writeProject(t, map[string]string{
		"go.mod":      "module example.com/testable\n\ngo 1.26.1\n",
		"run.go":      "package testable\n\nfunc Run() {}\n",
		"run_test.go": "package testable\n\nimport \"testing\"\n\nfunc TestRun(t *testing.T) {}\n",
	})

	withoutTests, err := Analyze(context.Background(), root, Options{})
	if err != nil {
		t.Fatalf("analyze without tests: %v", err)
	}
	withTests, err := Analyze(context.Background(), root, Options{IncludeTests: true})
	if err != nil {
		t.Fatalf("analyze with tests: %v", err)
	}
	if hasPackageFile(withoutTests, "run_test.go") {
		t.Fatal("test file included by default")
	}
	if !hasPackageFile(withTests, "run_test.go") {
		t.Fatal("test file missing when IncludeTests is true")
	}
	if hasFunction(withoutTests, "TestRun") {
		t.Fatal("test function included by default")
	}
	if !hasFunction(withTests, "TestRun") {
		t.Fatal("test function missing when IncludeTests is true")
	}
}

func TestAnalyzeReportsGoParseErrors(t *testing.T) {
	root := writeProject(t, map[string]string{
		"go.mod":    "module example.com/broken\n\ngo 1.26.1\n",
		"broken.go": "package broken\n\nfunc Broken( {\n",
	})

	document, err := Analyze(context.Background(), root, Options{})
	if err != nil {
		t.Fatalf("analyze malformed project: %v", err)
	}
	if !hasDiagnostic(document, "GO_PARSE_ERROR") && !hasDiagnostic(document, "PACKAGE_LOAD_ERROR") {
		t.Fatalf("missing parse diagnostic: %+v", document.Diagnostics)
	}
}

func TestAnalyzeLoadsProjectWithoutGoModule(t *testing.T) {
	root := writeProject(t, map[string]string{
		"run.go": "package standalone\n\nfunc Run() {}\n",
	})

	document, err := Analyze(context.Background(), root, Options{})
	if err != nil {
		t.Fatalf("analyze project without go.mod: %v", err)
	}
	if document.Service.Name != filepath.Base(root) {
		t.Fatalf("service name = %q, want directory name %q", document.Service.Name, filepath.Base(root))
	}
	if len(document.Packages) != 1 {
		t.Fatalf("package count = %d, want 1: %+v", len(document.Packages), document.Packages)
	}
}

func TestAnalyzeSummaryReturnsStableZeroCategories(t *testing.T) {
	root := writeProject(t, map[string]string{
		"go.mod": "module example.com/summaryfixture\n\ngo 1.26.1\n",
		"run.go": "package summaryfixture\n\nfunc Run() {}\n",
	})

	document, summary, err := AnalyzeSummary(context.Background(), root, Options{})
	if err != nil {
		t.Fatalf("analyze summary fixture: %v", err)
	}
	if len(document.Functions) != 1 {
		t.Fatalf("function count = %d, want 1", len(document.Functions))
	}
	for _, item := range summary.Items() {
		if item.Name == semantic.SummaryDiagnostics {
			if item.Count != len(document.Diagnostics) {
				t.Fatalf("diagnostic summary = %d, want %d", item.Count, len(document.Diagnostics))
			}
			continue
		}
		if item.Count != 0 {
			t.Fatalf("summary item %q = %d, want 0", item.Name, item.Count)
		}
	}
}

func TestAnalyzeRejectsInvalidSourceRoot(t *testing.T) {
	_, err := Analyze(context.Background(), filepath.Join(t.TempDir(), "missing"), Options{})
	if err == nil {
		t.Fatal("Analyze succeeded for missing source root")
	}
}

func TestAnalyzeBuildsFunctionDirectoryAndCallGraph(t *testing.T) {
	root := writeProject(t, map[string]string{
		"go.mod": "module example.com/graph\n\ngo 1.26.1\n",
		"graph.go": `package graph

type Runner interface {
	Run()
}

type worker struct{}

func (worker) Run() {}

func A() {
	B()
}

func B() {
	C()
}

func C() {
	A()
}

func UseInterface(r Runner) {
	r.Run()
}

func UseFunctionValue() {
	var callback func()
	callback()
}

func WithClosure() {
	func() {
		C()
	}()
}
`,
	})

	document, err := Analyze(context.Background(), root, Options{})
	if err != nil {
		t.Fatalf("analyze graph fixture: %v", err)
	}
	a := functionNamed(t, document, "A")
	b := functionNamed(t, document, "B")
	c := functionNamed(t, document, "C")
	useInterface := functionNamed(t, document, "UseInterface")
	useFunctionValue := functionNamed(t, document, "UseFunctionValue")
	withClosure := functionNamed(t, document, "WithClosure")
	method := functionNamed(t, document, "Run")
	var anonymous semantic.Function
	for _, function := range document.Functions {
		if function.Name == "<anonymous>" && strings.Contains(function.ID, withClosure.ID+":anonymous:") {
			anonymous = function
			break
		}
	}
	if anonymous.ID == "" {
		t.Fatalf("anonymous closure not found for %s", withClosure.ID)
	}

	assertHasCall(t, document, a.ID, b.ID, semantic.CallResolutionResolved)
	assertHasCall(t, document, b.ID, c.ID, semantic.CallResolutionResolved)
	assertHasCall(t, document, c.ID, a.ID, semantic.CallResolutionResolved)
	assertHasCall(t, document, useInterface.ID, "", semantic.CallResolutionUnresolved)
	assertHasCall(t, document, useFunctionValue.ID, "", semantic.CallResolutionUnresolved)
	assertHasCall(t, document, withClosure.ID, anonymous.ID, semantic.CallResolutionResolved)
	if len(method.CallerFunctionIDs) != 0 {
		t.Fatalf("interface method unexpectedly has callers: %+v", method.CallerFunctionIDs)
	}
	if method.Receiver != "worker" {
		t.Fatalf("method receiver = %q, want worker", method.Receiver)
	}
	if len(IncomingCalls(document, c.ID)) != 2 {
		t.Fatalf("incoming calls for C = %d, want 2", len(IncomingCalls(document, c.ID)))
	}
	if len(OutgoingCalls(document, a.ID)) != 1 {
		t.Fatalf("outgoing calls for A = %d, want 1", len(OutgoingCalls(document, a.ID)))
	}
	if !hasDiagnostic(document, "UNRESOLVED_CALL") {
		t.Fatalf("missing unresolved call diagnostic: %+v", document.Diagnostics)
	}
	for _, function := range document.Functions {
		if function.Name == "<anonymous>" && !strings.Contains(function.ID, withClosure.ID+":anonymous:") {
			t.Fatalf("anonymous function ID = %q does not include parent %q", function.ID, withClosure.ID)
		}
	}
}

func TestAnalyzeRecognizesStandardHTTPHandlers(t *testing.T) {
	root := writeProject(t, map[string]string{
		"go.mod": "module example.com/httpfixture\n\ngo 1.26.1\n",
		"http.go": `package httpfixture

import "net/http"

func Orders(http.ResponseWriter, *http.Request) {}

func register(pattern string, handler http.HandlerFunc) {
	http.HandleFunc("GET /orders", Orders)
	http.HandleFunc(pattern, handler)
	mux := http.NewServeMux()
	mux.Handle("/users", http.HandlerFunc(Orders))
}
`,
	})

	document, err := Analyze(context.Background(), root, Options{})
	if err != nil {
		t.Fatalf("analyze HTTP fixture: %v", err)
	}
	orders := functionNamed(t, document, "Orders")
	endpoints := endpointsForKind(document, semantic.EndpointKindHTTPHandler)
	if len(endpoints) != 2 {
		t.Fatalf("HTTP endpoint count = %d, want 2: %+v", len(endpoints), endpoints)
	}
	if !hasEndpoint(endpoints, orders.ID, "GET", "/orders") {
		t.Fatalf("missing GET /orders endpoint: %+v", endpoints)
	}
	if !hasEndpoint(endpoints, orders.ID, "", "/users") {
		t.Fatalf("missing ServeMux /users endpoint: %+v", endpoints)
	}
	if !hasDiagnostic(document, "DYNAMIC_HTTP_PATTERN") {
		t.Fatalf("missing dynamic HTTP pattern diagnostic: %+v", document.Diagnostics)
	}
	if !hasDiagnostic(document, "UNRESOLVED_HTTP_HANDLER") {
		t.Fatalf("missing unresolved HTTP handler diagnostic: %+v", document.Diagnostics)
	}
}

func TestAnalyzeRecognizesSupportedHTTPRouter(t *testing.T) {
	root := writeProject(t, map[string]string{
		"go.mod": `module example.com/routerfixture

go 1.26.1

require github.com/gorilla/mux v1.8.1

replace github.com/gorilla/mux => ./stubs/mux
`,
		"stubs/mux/go.mod": "module github.com/gorilla/mux\n\ngo 1.26.1\n",
		"stubs/mux/mux.go": `package mux

import "net/http"

type Router struct{}
type Route struct{}

func (router *Router) HandleFunc(string, func(http.ResponseWriter, *http.Request)) *Route { return &Route{} }
func (route *Route) Methods(...string) *Route { return route }
`,
		"router.go": `package routerfixture

import (
	"net/http"

	"github.com/gorilla/mux"
)

func Health(http.ResponseWriter, *http.Request) {}

func Register(router *mux.Router) {
	router.HandleFunc("/health", Health).Methods("POST")
}
`,
	})

	document, err := Analyze(context.Background(), root, Options{})
	if err != nil {
		t.Fatalf("analyze router fixture: %v", err)
	}
	health := functionNamed(t, document, "Health")
	endpoints := endpointsForKind(document, semantic.EndpointKindHTTPHandler)
	if !hasEndpoint(endpoints, health.ID, "POST", "/health") {
		t.Fatalf("missing gorilla/mux endpoint: %+v", endpoints)
	}
}

func TestAnalyzeReportsUnsupportedHTTPRouter(t *testing.T) {
	root := writeProject(t, map[string]string{
		"go.mod": "module example.com/unknownrouter\n\ngo 1.26.1\n",
		"router.go": `package unknownrouter

import "net/http"

type Router struct{}

func (router *Router) HandleFunc(string, func(http.ResponseWriter, *http.Request)) {}

func Health(http.ResponseWriter, *http.Request) {}

func Register(router *Router) {
	router.HandleFunc("/health", Health)
}
`,
	})

	document, err := Analyze(context.Background(), root, Options{})
	if err != nil {
		t.Fatalf("analyze unsupported router fixture: %v", err)
	}
	if endpoints := endpointsForKind(document, semantic.EndpointKindHTTPHandler); len(endpoints) != 0 {
		t.Fatalf("unsupported router produced endpoints: %+v", endpoints)
	}
	if !hasDiagnostic(document, "UNSUPPORTED_HTTP_ROUTER") {
		t.Fatalf("missing unsupported router diagnostic: %+v", document.Diagnostics)
	}
}

func TestAnalyzeRecognizesGRPCRegistration(t *testing.T) {
	root := writeProject(t, map[string]string{
		"go.mod": `module example.com/grpcfixture

go 1.26.1

require example.com/api v0.0.0

replace example.com/api => ./stubs/api
`,
		"stubs/api/go.mod": "module example.com/api\n\ngo 1.26.1\n",
		"stubs/api/api.go": `package api

type Registrar interface{}
type GreeterServer interface {
	SayHello()
}

func RegisterGreeterServer(Registrar, GreeterServer) {}
`,
		"server.go": `package grpcfixture

import "example.com/api"

type server struct{}

func (server) SayHello() {}

func register() {
	api.RegisterGreeterServer(nil, &server{})
}
`,
	})

	document, err := Analyze(context.Background(), root, Options{})
	if err != nil {
		t.Fatalf("analyze gRPC fixture: %v", err)
	}
	sayHello := functionNamed(t, document, "SayHello")
	endpoints := endpointsForKind(document, semantic.EndpointKindGRPCHandler)
	if len(endpoints) != 1 {
		t.Fatalf("gRPC endpoint count = %d, want 1: %+v", len(endpoints), endpoints)
	}
	endpoint := endpoints[0]
	if endpoint.FunctionID != sayHello.ID || endpoint.GRPCService != "Greeter" || endpoint.GRPCMethod != "SayHello" {
		t.Fatalf("gRPC endpoint = %+v", endpoint)
	}
}

func TestAnalyzeRecognizesCronRegistration(t *testing.T) {
	root := writeProject(t, map[string]string{
		"go.mod": `module example.com/cronfixture

go 1.26.1

require github.com/robfig/cron/v3 v3.0.0

replace github.com/robfig/cron/v3 => ./stubs/cron
`,
		"stubs/cron/go.mod": "module github.com/robfig/cron/v3\n\ngo 1.26.1\n",
		"stubs/cron/cron.go": `package cron

type Cron struct{}
type Job interface {
	Run()
}

func (cron *Cron) AddFunc(string, func()) {}
func (cron *Cron) AddJob(string, Job) {}
`,
		"jobs.go": `package cronfixture

import "github.com/robfig/cron/v3"

func Cleanup() {}

func register(cron *cron.Cron, schedule string, job cron.Job) {
	cron.AddFunc("@hourly", Cleanup)
	cron.AddFunc(schedule, Cleanup)
	cron.AddJob("@daily", job)
}
`,
	})

	document, err := Analyze(context.Background(), root, Options{})
	if err != nil {
		t.Fatalf("analyze cron fixture: %v", err)
	}
	cleanup := functionNamed(t, document, "Cleanup")
	endpoints := endpointsForKind(document, semantic.EndpointKindCronJob)
	if len(endpoints) != 2 {
		t.Fatalf("cron endpoint count = %d, want 2: %+v", len(endpoints), endpoints)
	}
	if !hasCronEndpoint(endpoints, cleanup.ID, "@hourly") || !hasCronEndpoint(endpoints, cleanup.ID, "") {
		t.Fatalf("cron endpoints missing static/dynamic schedules: %+v", endpoints)
	}
	if !hasDiagnostic(document, "DYNAMIC_CRON_SCHEDULE") {
		t.Fatalf("missing dynamic cron schedule diagnostic: %+v", document.Diagnostics)
	}
	if !hasDiagnostic(document, "UNRESOLVED_CRON_CALLBACK") {
		t.Fatalf("missing unresolved cron callback diagnostic: %+v", document.Diagnostics)
	}
}

func TestAnalyzeRecognizesSQLDependencies(t *testing.T) {
	root := writeProject(t, map[string]string{
		"go.mod": "module example.com/sqlfixture\n\ngo 1.26.1\n",
		"sql.go": `package sqlfixture

import (
	"context"
	"database/sql"
)

func Run(db *sql.DB, tx *sql.Tx, stmt *sql.Stmt, conn *sql.Conn, query string) {
	db.Query("SELECT id FROM orders")
	db.QueryContext(context.Background(), "SELECT id FROM users")
	db.Exec(query)
	tx.Prepare("UPDATE orders SET status = 1")
	stmt.Query("SELECT 1")
	db.Begin()
	conn.BeginTx(context.Background(), nil)
}
`,
	})

	document, err := Analyze(context.Background(), root, Options{})
	if err != nil {
		t.Fatalf("analyze SQL fixture: %v", err)
	}
	run := functionNamed(t, document, "Run")
	dependencies := dependenciesForKind(document, semantic.DependencyKindSQL)
	if len(dependencies) != 7 {
		t.Fatalf("SQL dependency count = %d, want 7: %+v", len(dependencies), dependencies)
	}
	if len(run.DependencyIDs) != len(dependencies) {
		t.Fatalf("Run dependency IDs = %d, want %d: %+v", len(run.DependencyIDs), len(dependencies), run.DependencyIDs)
	}
	if !hasDependencyValue(dependencies, "SELECT id FROM orders", true) ||
		!hasDependencyValue(dependencies, "SELECT id FROM users", true) ||
		!hasDependencyValue(dependencies, "UPDATE orders SET status = 1", true) ||
		!hasDependencyValue(dependencies, "SELECT 1", true) {
		t.Fatalf("missing static SQL dependency: %+v", dependencies)
	}
	if !hasDependencyValue(dependencies, "", false) || !hasDiagnostic(document, "DYNAMIC_SQL") {
		t.Fatalf("missing dynamic SQL dependency or diagnostic: %+v / %+v", dependencies, document.Diagnostics)
	}
	for _, dependency := range dependencies {
		if dependency.FunctionID != run.ID {
			t.Fatalf("SQL dependency caller = %q, want %q", dependency.FunctionID, run.ID)
		}
	}
}

func TestAnalyzeRecognizesRedisDependenciesByPackageAndType(t *testing.T) {
	root := writeProject(t, map[string]string{
		"go.mod": `module example.com/redisfixture

go 1.26.1

require github.com/redis/go-redis/v9 v9.0.0

replace github.com/redis/go-redis/v9 => ./stubs/redis
`,
		"stubs/redis/go.mod": "module github.com/redis/go-redis/v9\n\ngo 1.26.1\n",
		"stubs/redis/redis.go": `package redis

import (
	"context"
	"time"
)

type Client struct{}
type Cmd struct{}

func (*Client) Get(context.Context, string) *Cmd { return nil }
func (*Client) Set(context.Context, string, interface{}, time.Duration) *Cmd { return nil }
`,
		"redis.go": `package redisfixture

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

type localClient struct{}

func (localClient) Get(context.Context, string) {}

func Run(client *redis.Client, key string) {
	client.Get(context.Background(), "orders")
	client.Set(context.Background(), "orders", "value", time.Minute)
	client.Get(context.Background(), key)
	localClient{}.Get(context.Background(), "not-redis")
}
`,
	})

	document, err := Analyze(context.Background(), root, Options{})
	if err != nil {
		t.Fatalf("analyze Redis fixture: %v", err)
	}
	dependencies := dependenciesForKind(document, semantic.DependencyKindRedis)
	if len(dependencies) != 3 {
		t.Fatalf("Redis dependency count = %d, want 3: %+v", len(dependencies), dependencies)
	}
	if !hasDependencyValue(dependencies, "orders", true) || !hasDependencyValue(dependencies, "", false) {
		t.Fatalf("missing static/dynamic Redis dependency: %+v", dependencies)
	}
	if hasDependencyValue(dependencies, "not-redis", true) || !hasDiagnostic(document, "DYNAMIC_REDIS_KEY") {
		t.Fatalf("local same-name method was recognized or dynamic diagnostic missing: %+v / %+v", dependencies, document.Diagnostics)
	}
}

func TestAnalyzeRecognizesSaramaKafkaDependencies(t *testing.T) {
	root := writeProject(t, map[string]string{
		"go.mod": `module example.com/kafkafixture

go 1.26.1

require github.com/IBM/sarama v0.0.0

replace github.com/IBM/sarama => ./stubs/sarama
`,
		"stubs/sarama/go.mod": "module github.com/IBM/sarama\n\ngo 1.26.1\n",
		"stubs/sarama/sarama.go": `package sarama

import "context"

type ProducerMessage struct { Topic string }
type SyncProducer interface { SendMessage(*ProducerMessage) (int32, int64, error) }
type Consumer interface { ConsumePartition(string, int32, int64) (PartitionConsumer, error) }
type PartitionConsumer interface{}
type ConsumerGroup interface { Consume(context.Context, []string, ConsumerGroupHandler) error }
type ConsumerGroupHandler interface{}
type Config struct{}

func NewConsumerGroup([]string, string, *Config) (ConsumerGroup, error) { return nil, nil }
`,
		"kafka.go": `package kafkafixture

import (
	"context"

	"github.com/IBM/sarama"
)

func Run(producer sarama.SyncProducer, consumer sarama.Consumer, group sarama.ConsumerGroup, topic, groupName string) {
	producer.SendMessage(&sarama.ProducerMessage{Topic: "orders"})
	producer.SendMessage(&sarama.ProducerMessage{Topic: topic})
	consumer.ConsumePartition("payments", 0, 0)
	consumer.ConsumePartition(topic, 0, 0)
	group.Consume(context.Background(), []string{"orders"}, nil)
	sarama.NewConsumerGroup(nil, "billing", nil)
	sarama.NewConsumerGroup(nil, groupName, nil)
}
`,
	})

	document, err := Analyze(context.Background(), root, Options{})
	if err != nil {
		t.Fatalf("analyze Kafka fixture: %v", err)
	}
	producers := dependenciesForKind(document, semantic.DependencyKindKafkaProducer)
	consumers := dependenciesForKind(document, semantic.DependencyKindKafkaConsumer)
	if len(producers) != 2 {
		t.Fatalf("Kafka producer dependency count = %d, want 2: %+v", len(producers), producers)
	}
	if len(consumers) != 5 {
		t.Fatalf("Kafka consumer dependency count = %d, want 5: %+v", len(consumers), consumers)
	}
	if !hasDependencyValue(producers, "orders", true) || !hasDependencyValue(producers, "", false) ||
		!hasDependencyValue(consumers, "payments", true) || !hasDependencyValue(consumers, "billing", true) ||
		!hasDependencyValue(consumers, "", false) {
		t.Fatalf("missing Kafka static/dynamic values: producers=%+v consumers=%+v", producers, consumers)
	}
	if !hasDiagnostic(document, "DYNAMIC_KAFKA_VALUE") {
		t.Fatalf("missing dynamic Kafka diagnostic: %+v", document.Diagnostics)
	}
}

func TestAnalyzeRecognizesHTTPClientDependencies(t *testing.T) {
	root := writeProject(t, map[string]string{
		"go.mod": "module example.com/httpclientfixture\n\ngo 1.26.1\n",
		"http.go": `package httpclientfixture

import (
	"context"
	"net/http"
)

func Handler(http.ResponseWriter, *http.Request) {}

func Run(client *http.Client, target, method string) {
	http.Get("https://orders.example.test/orders")
	http.Post(target, "application/json", nil)
	client.Get("https://users.example.test/users")
	request, _ := http.NewRequest("POST", "https://billing.example.test/payments", nil)
	client.Do(request)
	dynamicRequest, _ := http.NewRequestWithContext(context.Background(), method, target, nil)
	client.Do(dynamicRequest)
	http.HandleFunc("/orders", Handler)
}
`,
	})

	document, err := Analyze(context.Background(), root, Options{})
	if err != nil {
		t.Fatalf("analyze HTTP client fixture: %v", err)
	}
	dependencies := dependenciesForKind(document, semantic.DependencyKindHTTPClient)
	if len(dependencies) != 5 {
		t.Fatalf("HTTP client dependency count = %d, want 5: %+v", len(dependencies), dependencies)
	}
	if !hasDependencyURL(dependencies, "https://orders.example.test/orders", "get", true) ||
		!hasDependencyURL(dependencies, "https://users.example.test/users", "get", true) ||
		!hasDependencyURL(dependencies, "https://billing.example.test/payments", "post", true) {
		t.Fatalf("missing static HTTP client dependency: %+v", dependencies)
	}
	if !hasDependencyURL(dependencies, "", "post", false) || !hasDependencyURL(dependencies, "", "do", false) {
		t.Fatalf("missing dynamic HTTP URL/method dependency: %+v", dependencies)
	}
	for _, dependency := range dependencies {
		if dependency.TargetPackage != "net/http" {
			t.Fatalf("HTTP target package = %q, want net/http", dependency.TargetPackage)
		}
	}
	if hasDiagnostic(document, "UNSUPPORTED_HTTP_ROUTER") {
		t.Fatalf("standard HTTP handler was treated as unsupported router: %+v", document.Diagnostics)
	}
	if !hasDiagnostic(document, "DYNAMIC_HTTP_URL") || !hasDiagnostic(document, "DYNAMIC_HTTP_METHOD") {
		t.Fatalf("missing dynamic HTTP diagnostics: %+v", document.Diagnostics)
	}
}

func TestAnalyzeRecognizesGRPCClientDependencies(t *testing.T) {
	root := writeProject(t, map[string]string{
		"go.mod": `module example.com/grpcclientfixture

go 1.26.1

require (
	google.golang.org/grpc v0.0.0
	example.com/api v0.0.0
)

replace google.golang.org/grpc => ./stubs/grpc
replace example.com/api => ./stubs/api
`,
		"stubs/grpc/go.mod": "module google.golang.org/grpc\n\ngo 1.26.1\n",
		"stubs/grpc/grpc.go": `package grpc

import "context"

type ClientConn struct{}
type CallOption struct{}
type DialOption struct{}

func Dial(target string, options ...DialOption) (*ClientConn, error) { return nil, nil }
func DialContext(context.Context, string, ...DialOption) (*ClientConn, error) { return nil, nil }
`,
		"stubs/api/go.mod": "module example.com/api\n\ngo 1.26.1\n\nrequire google.golang.org/grpc v0.0.0\n\nreplace google.golang.org/grpc => ../grpc\n",
		"stubs/api/api.go": `package api

import (
	"context"
	"google.golang.org/grpc"
)

type HelloRequest struct{}
type HelloReply struct{}
type GreeterClient interface {
	SayHello(context.Context, *HelloRequest, ...grpc.CallOption) (*HelloReply, error)
}

type greeterClient struct{}

func (*greeterClient) SayHello(context.Context, *HelloRequest, ...grpc.CallOption) (*HelloReply, error) {
	return nil, nil
}

func NewGreeterClient(*grpc.ClientConn) GreeterClient { return &greeterClient{} }
`,
		"client.go": `package grpcclientfixture

import (
	"context"

	"example.com/api"
	"google.golang.org/grpc"
)

type localClient struct{}

func (localClient) SayHello(context.Context, *api.HelloRequest, ...grpc.CallOption) (*api.HelloReply, error) {
	return nil, nil
}

func Run(target string) {
	dynamicConn, _ := grpc.Dial(target)
	dynamicClient := api.NewGreeterClient(dynamicConn)
	dynamicClient.SayHello(context.Background(), &api.HelloRequest{})

	staticConn, _ := grpc.Dial("dns:///orders")
	staticClient := api.NewGreeterClient(staticConn)
	staticClient.SayHello(context.Background(), &api.HelloRequest{})

	localClient{}.SayHello(context.Background(), &api.HelloRequest{})
}
`,
	})

	document, err := Analyze(context.Background(), root, Options{})
	if err != nil {
		t.Fatalf("analyze gRPC client fixture: %v", err)
	}
	dependencies := dependenciesForKind(document, semantic.DependencyKindRPCClient)
	if len(dependencies) != 2 {
		t.Fatalf("RPC client dependency count = %d, want 2: %+v", len(dependencies), dependencies)
	}
	if !hasDependencyTarget(dependencies, "dns:///orders", true) || !hasDependencyTarget(dependencies, "", false) {
		t.Fatalf("missing static/dynamic gRPC targets: %+v", dependencies)
	}
	for _, dependency := range dependencies {
		if dependency.TargetPackage != "example.com/api" || dependency.Resource != "GreeterClient" {
			t.Fatalf("RPC dependency identity = %+v", dependency)
		}
	}
	if !hasDiagnostic(document, "DYNAMIC_GRPC_TARGET") {
		t.Fatalf("missing dynamic gRPC target diagnostic: %+v", document.Diagnostics)
	}
}

func writeProject(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, contents := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create fixture directory: %v", err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatalf("write fixture file: %v", err)
		}
	}
	return root
}

func hasPackageFile(document semantic.Document, wanted string) bool {
	for _, pkg := range document.Packages {
		for _, file := range pkg.Files {
			if file == wanted {
				return true
			}
		}
	}
	return false
}

func hasDiagnostic(document semantic.Document, wanted string) bool {
	for _, diagnostic := range document.Diagnostics {
		if diagnostic.Code == wanted {
			return true
		}
	}
	return false
}

func hasFunction(document semantic.Document, wanted string) bool {
	for _, function := range document.Functions {
		if function.Name == wanted {
			return true
		}
	}
	return false
}

func endpointsForKind(document semantic.Document, kind semantic.EndpointKind) []semantic.Endpoint {
	result := make([]semantic.Endpoint, 0)
	for _, endpoint := range document.Endpoints {
		if endpoint.Kind == kind {
			result = append(result, endpoint)
		}
	}
	return result
}

func hasEndpoint(endpoints []semantic.Endpoint, functionID, method, path string) bool {
	for _, endpoint := range endpoints {
		if endpoint.FunctionID == functionID && endpoint.HTTPMethod == method && endpoint.HTTPPath == path {
			return true
		}
	}
	return false
}

func hasCronEndpoint(endpoints []semantic.Endpoint, functionID, schedule string) bool {
	for _, endpoint := range endpoints {
		if endpoint.FunctionID == functionID && endpoint.CronSchedule == schedule {
			return true
		}
	}
	return false
}

func dependenciesForKind(document semantic.Document, kind semantic.DependencyKind) []semantic.Dependency {
	result := make([]semantic.Dependency, 0)
	for _, dependency := range document.Dependencies {
		if dependency.Kind == kind {
			result = append(result, dependency)
		}
	}
	return result
}

func hasDependencyValue(dependencies []semantic.Dependency, value string, static bool) bool {
	for _, dependency := range dependencies {
		if dependency.Value == value && dependency.ValueIsStatic == static {
			return true
		}
	}
	return false
}

func hasDependencyURL(dependencies []semantic.Dependency, targetURL, operation string, static bool) bool {
	for _, dependency := range dependencies {
		if dependency.TargetURL == targetURL && dependency.Operation == operation && dependency.ValueIsStatic == static {
			return true
		}
	}
	return false
}

func hasDependencyTarget(dependencies []semantic.Dependency, target string, static bool) bool {
	for _, dependency := range dependencies {
		if dependency.TargetService == target && dependency.ValueIsStatic == static {
			return true
		}
	}
	return false
}

func functionNamed(t *testing.T, document semantic.Document, name string) semantic.Function {
	t.Helper()
	for _, function := range document.Functions {
		if function.Name == name {
			return function
		}
	}
	t.Fatalf("function %q not found: %+v", name, document.Functions)
	return semantic.Function{}
}

func assertHasCall(t *testing.T, document semantic.Document, callerID, calleeID string, resolution semantic.CallResolution) {
	t.Helper()
	for _, edge := range document.CallEdges {
		if edge.CallerFunctionID == callerID && edge.CalleeFunctionID == calleeID && edge.Resolution == resolution {
			return
		}
	}
	t.Fatalf("call edge %s -> %s (%s) not found: %+v", callerID, calleeID, resolution, document.CallEdges)
}
