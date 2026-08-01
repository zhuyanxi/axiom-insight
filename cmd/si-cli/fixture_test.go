package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"testing"
)

type fixtureSnapshot struct {
	Summary         map[string]int              `json:"summary"`
	Functions       []fixtureFunctionSnapshot   `json:"functions"`
	Endpoints       []fixtureEndpointSnapshot   `json:"endpoints"`
	Dependencies    []fixtureDependencySnapshot `json:"dependencies"`
	DiagnosticCodes []string                    `json:"diagnostic_codes"`
}

type fixtureFunctionSnapshot struct {
	Name     string `json:"name"`
	Receiver string `json:"receiver"`
}

type fixtureEndpointSnapshot struct {
	Kind         string `json:"kind"`
	Name         string `json:"name"`
	Function     string `json:"function"`
	HTTPMethod   string `json:"http_method"`
	HTTPPath     string `json:"http_path"`
	GRPCService  string `json:"grpc_service"`
	GRPCMethod   string `json:"grpc_method"`
	CronSchedule string `json:"cron_schedule"`
}

type fixtureDependencySnapshot struct {
	Kind          string `json:"kind"`
	Name          string `json:"name"`
	Function      string `json:"function"`
	Operation     string `json:"operation"`
	TargetService string `json:"target_service"`
	TargetURL     string `json:"target_url"`
	TargetPackage string `json:"target_package"`
	Resource      string `json:"resource"`
	Value         string `json:"value"`
	ValueIsStatic bool   `json:"value_is_static"`
}

type fixtureScanResult struct {
	Summary  []jsonSummaryItem `json:"summary"`
	Document fixtureDocument   `json:"document"`
}

type fixtureDocument struct {
	Functions    []fixtureFunctionJSON   `json:"functions"`
	Endpoints    []fixtureEndpointJSON   `json:"endpoints"`
	Dependencies []fixtureDependencyJSON `json:"dependencies"`
	Diagnostics  []fixtureDiagnosticJSON `json:"diagnostics"`
}

type fixtureFunctionJSON struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Receiver string `json:"receiver"`
}

type fixtureEndpointJSON struct {
	Kind         string `json:"kind"`
	Name         string `json:"name"`
	FunctionID   string `json:"function_id"`
	HTTPMethod   string `json:"http_method"`
	HTTPPath     string `json:"http_path"`
	GRPCService  string `json:"grpc_service"`
	GRPCMethod   string `json:"grpc_method"`
	CronSchedule string `json:"cron_schedule"`
}

type fixtureDependencyJSON struct {
	Kind          string `json:"kind"`
	Name          string `json:"name"`
	FunctionID    string `json:"function_id"`
	Operation     string `json:"operation"`
	TargetService string `json:"target_service"`
	TargetURL     string `json:"target_url"`
	TargetPackage string `json:"target_package"`
	Resource      string `json:"resource"`
	Value         string `json:"value"`
	ValueIsStatic bool   `json:"value_is_static"`
}

type fixtureDiagnosticJSON struct {
	Code string `json:"code"`
}

func TestScanFixtures(t *testing.T) {
	fixtureRoot := phase0FixturesRoot(t)
	fixtureNames := []string{
		"http",
		"grpc",
		"cron",
		"kafka",
		"sql",
		"redis",
		"http-client",
		"rpc-client",
		"malformed",
		"negative",
	}
	for _, name := range fixtureNames {
		t.Run(name, func(t *testing.T) {
			runFixtureScan(t, filepath.Join(fixtureRoot, name))
		})
	}
}

func runFixtureScan(t *testing.T, fixturePath string) {
	t.Helper()
	expectedPath := filepath.Join(fixturePath, "expected.json")
	expectedContents, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("read fixture snapshot: %v", err)
	}
	var want fixtureSnapshot
	if err := json.Unmarshal(expectedContents, &want); err != nil {
		t.Fatalf("parse fixture snapshot: %v", err)
	}
	sortFixtureSnapshot(&want)

	beforeFiles := fixtureFiles(t, fixturePath)
	var stdout, stderr bytes.Buffer
	code := run([]string{"scan", fixturePath, "--format", "json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("scan exit code = %d, want 0; stderr = %s", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("scan stderr = %q, want empty", stderr.String())
	}

	var result fixtureScanResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("parse scan JSON: %v\n%s", err, stdout.String())
	}
	got := snapshotFromScanResult(result)
	sortFixtureSnapshot(&got)
	if !reflect.DeepEqual(got, want) {
		gotJSON, _ := json.MarshalIndent(got, "", "  ")
		wantJSON, _ := json.MarshalIndent(want, "", "  ")
		t.Fatalf("fixture snapshot mismatch\ngot:\n%s\nwant:\n%s", gotJSON, wantJSON)
	}
	if afterFiles := fixtureFiles(t, fixturePath); !reflect.DeepEqual(beforeFiles, afterFiles) {
		t.Fatalf("scan changed fixture files\nbefore: %v\nafter: %v", beforeFiles, afterFiles)
	}
}

func snapshotFromScanResult(result fixtureScanResult) fixtureSnapshot {
	functionKeys := make(map[string]string, len(result.Document.Functions))
	functions := make([]fixtureFunctionSnapshot, 0, len(result.Document.Functions))
	for _, function := range result.Document.Functions {
		key := fixtureFunctionKey(function.Name, function.Receiver)
		functionKeys[function.ID] = key
		functions = append(functions, fixtureFunctionSnapshot{
			Name:     function.Name,
			Receiver: function.Receiver,
		})
	}

	summary := make(map[string]int, len(result.Summary))
	for _, item := range result.Summary {
		summary[item.Name] = item.Count
	}

	endpoints := make([]fixtureEndpointSnapshot, 0, len(result.Document.Endpoints))
	for _, endpoint := range result.Document.Endpoints {
		endpoints = append(endpoints, fixtureEndpointSnapshot{
			Kind:         endpoint.Kind,
			Name:         endpoint.Name,
			Function:     functionKeys[endpoint.FunctionID],
			HTTPMethod:   endpoint.HTTPMethod,
			HTTPPath:     endpoint.HTTPPath,
			GRPCService:  endpoint.GRPCService,
			GRPCMethod:   endpoint.GRPCMethod,
			CronSchedule: endpoint.CronSchedule,
		})
	}

	dependencies := make([]fixtureDependencySnapshot, 0, len(result.Document.Dependencies))
	for _, dependency := range result.Document.Dependencies {
		dependencies = append(dependencies, fixtureDependencySnapshot{
			Kind:          dependency.Kind,
			Name:          dependency.Name,
			Function:      functionKeys[dependency.FunctionID],
			Operation:     dependency.Operation,
			TargetService: dependency.TargetService,
			TargetURL:     dependency.TargetURL,
			TargetPackage: dependency.TargetPackage,
			Resource:      dependency.Resource,
			Value:         dependency.Value,
			ValueIsStatic: dependency.ValueIsStatic,
		})
	}

	diagnosticCodes := make([]string, 0, len(result.Document.Diagnostics))
	for _, diagnostic := range result.Document.Diagnostics {
		diagnosticCodes = append(diagnosticCodes, diagnostic.Code)
	}
	return fixtureSnapshot{
		Summary:         summary,
		Functions:       functions,
		Endpoints:       endpoints,
		Dependencies:    dependencies,
		DiagnosticCodes: diagnosticCodes,
	}
}

func sortFixtureSnapshot(snapshot *fixtureSnapshot) {
	sort.Slice(snapshot.Functions, func(left, right int) bool {
		return fixtureFunctionKey(snapshot.Functions[left].Name, snapshot.Functions[left].Receiver) < fixtureFunctionKey(snapshot.Functions[right].Name, snapshot.Functions[right].Receiver)
	})
	sort.Slice(snapshot.Endpoints, func(left, right int) bool {
		return fixtureEndpointKey(snapshot.Endpoints[left]) < fixtureEndpointKey(snapshot.Endpoints[right])
	})
	sort.Slice(snapshot.Dependencies, func(left, right int) bool {
		return fixtureDependencyKey(snapshot.Dependencies[left]) < fixtureDependencyKey(snapshot.Dependencies[right])
	})
	sort.Strings(snapshot.DiagnosticCodes)
}

func fixtureFunctionKey(name, receiver string) string {
	if receiver == "" {
		return name
	}
	return receiver + "." + name
}

func fixtureEndpointKey(endpoint fixtureEndpointSnapshot) string {
	return endpoint.Kind + ":" + endpoint.Function + ":" + endpoint.Name + ":" + endpoint.HTTPMethod + ":" + endpoint.HTTPPath + ":" + endpoint.GRPCService + ":" + endpoint.GRPCMethod + ":" + endpoint.CronSchedule
}

func fixtureDependencyKey(dependency fixtureDependencySnapshot) string {
	return dependency.Kind + ":" + dependency.Function + ":" + dependency.Name + ":" + dependency.Operation + ":" + dependency.TargetService + ":" + dependency.TargetURL + ":" + dependency.TargetPackage + ":" + dependency.Resource + ":" + dependency.Value
}

func phase0FixturesRoot(t testing.TB) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate fixture test file")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "testdata", "fixtures"))
}

func fixtureFiles(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(relative))
		return nil
	})
	if err != nil {
		t.Fatalf("list fixture files: %v", err)
	}
	sort.Strings(files)
	return files
}
