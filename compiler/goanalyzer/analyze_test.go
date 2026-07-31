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
