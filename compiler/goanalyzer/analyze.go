package goanalyzer

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/zhuyanxi/axiom-insight/compiler/semantic"
	"golang.org/x/mod/modfile"
	"golang.org/x/tools/go/packages"
	"gopkg.in/yaml.v3"
)

const schemaVersion = "v1"

type Options struct {
	IncludeTests     bool
	Include          []string
	Exclude          []string
	ServiceName      string
	ConfigPath       string
	ConfigYAML       string
	Env              []string
	BuildFlags       []string
	EndpointAdapters []EndpointAdapter
	DependencyRules  []DependencyRule
}

func Analyze(ctx context.Context, sourceRoot string, options Options) (semantic.Document, error) {
	root, err := filepath.Abs(sourceRoot)
	if err != nil {
		return semantic.Document{}, fmt.Errorf("resolve source root: %w", err)
	}
	info, err := os.Stat(root)
	if err != nil {
		return semantic.Document{}, fmt.Errorf("stat source root: %w", err)
	}
	if !info.IsDir() {
		return semantic.Document{}, fmt.Errorf("source root %q is not a directory", sourceRoot)
	}

	serviceName, modulePath, config, configDiagnostics := resolveService(root, options)
	document := semantic.Document{
		SchemaVersion: schemaVersion,
		Service: semantic.Service{
			Name:       serviceName,
			SourceRoot: root,
			Language:   "go",
			ModulePath: modulePath,
		},
		Diagnostics: configDiagnostics,
	}
	endpointAdapters := options.EndpointAdapters
	if endpointAdapters == nil && len(config.FrameworkAdapters) > 0 {
		var adapterErr error
		endpointAdapters, adapterErr = endpointAdaptersByName(config.FrameworkAdapters)
		if adapterErr != nil {
			document.Diagnostics = append(document.Diagnostics, semantic.Diagnostic{
				Severity: semantic.DiagnosticSeverityError,
				Code:     semantic.DiagnosticCodeInvalidConfig,
				Message:  adapterErr.Error(),
			})
			endpointAdapters = []EndpointAdapter{}
		}
	}

	loaded, loadErr := loadPackages(ctx, root, options)
	if loadErr != nil {
		return document, loadErr
	}

	for _, pkg := range loaded {
		document.Diagnostics = append(document.Diagnostics, packageDiagnostics(root, pkg)...)
		files := packageFiles(root, pkg, options.IncludeTests)
		if len(files) == 0 {
			continue
		}
		document.Packages = append(document.Packages, semantic.Package{
			Name:       pkg.Name,
			ImportPath: pkg.PkgPath,
			Files:      files,
		})
	}

	sort.Slice(document.Packages, func(left, right int) bool {
		return semantic.PackageID(document.Packages[left]) < semantic.PackageID(document.Packages[right])
	})
	for _, pkg := range document.Packages {
		document.Service.PackageIDs = append(document.Service.PackageIDs, semantic.PackageID(pkg))
	}
	sort.Strings(document.Service.PackageIDs)
	packageIDs := make(map[string]string, len(document.Packages))
	for _, pkg := range document.Packages {
		packageIDs[pkg.ImportPath] = semantic.PackageID(pkg)
	}
	functionAnalysis := analyzeFunctions(root, loaded, packageIDs, options.IncludeTests, endpointAdapters, options.DependencyRules)
	document.Functions = append(document.Functions, functionAnalysis.functions...)
	document.Endpoints = append(document.Endpoints, functionAnalysis.endpoints...)
	document.Dependencies = append(document.Dependencies, functionAnalysis.dependencies...)
	document.CallEdges = append(document.CallEdges, functionAnalysis.callEdges...)
	document.Diagnostics = append(document.Diagnostics, functionAnalysis.diagnostics...)
	sort.SliceStable(document.Diagnostics, func(left, right int) bool {
		if document.Diagnostics[left].Code != document.Diagnostics[right].Code {
			return document.Diagnostics[left].Code < document.Diagnostics[right].Code
		}
		if document.Diagnostics[left].Message != document.Diagnostics[right].Message {
			return document.Diagnostics[left].Message < document.Diagnostics[right].Message
		}
		if document.Diagnostics[left].SourceLocation.RelativePath != document.Diagnostics[right].SourceLocation.RelativePath {
			return document.Diagnostics[left].SourceLocation.RelativePath < document.Diagnostics[right].SourceLocation.RelativePath
		}
		if document.Diagnostics[left].SourceLocation.StartLine != document.Diagnostics[right].SourceLocation.StartLine {
			return document.Diagnostics[left].SourceLocation.StartLine < document.Diagnostics[right].SourceLocation.StartLine
		}
		return document.Diagnostics[left].SourceLocation.StartColumn < document.Diagnostics[right].SourceLocation.StartColumn
	})

	return document, nil
}

// AnalyzeSummary returns the semantic document and its derived scan summary.
func AnalyzeSummary(ctx context.Context, sourceRoot string, options Options) (semantic.Document, semantic.ScanSummary, error) {
	document, err := Analyze(ctx, sourceRoot, options)
	return document, semantic.Summarize(document), err
}

func loadPackages(ctx context.Context, root string, options Options) ([]*packages.Package, error) {
	config := &packages.Config{
		Context:    ctx,
		Dir:        root,
		Env:        packageEnv(root, options.Env),
		BuildFlags: append([]string(nil), options.BuildFlags...),
		Tests:      options.IncludeTests,
		Mode: packages.NeedName |
			packages.NeedFiles |
			packages.NeedCompiledGoFiles |
			packages.NeedSyntax |
			packages.NeedTypes |
			packages.NeedTypesInfo |
			packages.NeedImports |
			packages.NeedDeps,
	}
	loaded, err := packages.Load(config, packagePatterns(options.Include)...)
	if err != nil {
		return nil, fmt.Errorf("load Go packages: %w", err)
	}
	return filterPackages(root, deduplicatePackages(loaded), options.Exclude), nil
}

func deduplicatePackages(loaded []*packages.Package) []*packages.Package {
	result := make([]*packages.Package, 0, len(loaded))
	indexes := make(map[string]int, len(loaded))
	for _, pkg := range loaded {
		if pkg == nil {
			continue
		}
		key := pkg.PkgPath
		if key == "" {
			key = pkg.ID
		}
		index, exists := indexes[key]
		if !exists {
			indexes[key] = len(result)
			result = append(result, pkg)
			continue
		}
		if packageVariantScore(pkg) > packageVariantScore(result[index]) {
			result[index] = pkg
		}
	}
	return result
}

func packageVariantScore(pkg *packages.Package) int {
	if pkg == nil {
		return 0
	}
	return len(pkg.Syntax)*4 + len(pkg.CompiledGoFiles)*2 + len(pkg.GoFiles)
}

func packagePatterns(patterns []string) []string {
	if len(patterns) == 0 {
		return []string{"./..."}
	}
	result := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern != "" {
			result = append(result, pattern)
		}
	}
	if len(result) == 0 {
		return []string{"./..."}
	}
	return result
}

func filterPackages(root string, loaded []*packages.Package, excludes []string) []*packages.Package {
	if len(excludes) == 0 {
		return loaded
	}
	filtered := make([]*packages.Package, 0, len(loaded))
	for _, pkg := range loaded {
		if !packageMatchesAny(pkg, root, excludes) {
			filtered = append(filtered, pkg)
		}
	}
	return filtered
}

func packageMatchesAny(pkg *packages.Package, root string, patterns []string) bool {
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(filepath.ToSlash(pattern))
		pattern = strings.TrimPrefix(pattern, "./")
		if pattern == "" {
			continue
		}
		if packagePathMatches(pkg.PkgPath, pattern) {
			return true
		}
		for _, file := range append(append([]string{}, pkg.GoFiles...), pkg.CompiledGoFiles...) {
			relative, err := filepath.Rel(root, file)
			if err == nil && packagePathMatches(filepath.ToSlash(relative), pattern) {
				return true
			}
		}
	}
	return false
}

func packagePathMatches(value, pattern string) bool {
	value = strings.TrimPrefix(filepath.ToSlash(value), "./")
	if strings.HasSuffix(pattern, "/...") {
		prefix := strings.TrimSuffix(pattern, "/...")
		return value == prefix || strings.HasPrefix(value, prefix+"/")
	}
	if matched, err := path.Match(pattern, value); err == nil && matched {
		return true
	}
	return value == pattern || strings.HasPrefix(value, pattern+"/")
}

func packageEnv(root string, extra []string) []string {
	env := append([]string(nil), os.Environ()...)
	if _, err := os.Stat(filepath.Join(root, "go.mod")); os.IsNotExist(err) && !hasEnv(env, "GO111MODULE") {
		env = append(env, "GO111MODULE=off")
	}
	for _, value := range extra {
		key := value
		if separator := strings.IndexByte(value, '='); separator >= 0 {
			key = value[:separator]
		}
		env = replaceEnv(env, key, value)
	}
	return env
}

func hasEnv(env []string, key string) bool {
	for _, value := range env {
		if strings.HasPrefix(value, key+"=") {
			return true
		}
	}
	return false
}

func replaceEnv(env []string, key, value string) []string {
	for index, existing := range env {
		if strings.HasPrefix(existing, key+"=") {
			env[index] = value
			return env
		}
	}
	return append(env, value)
}

type scanConfig struct {
	Name              string   `yaml:"name"`
	ServiceName       string   `yaml:"service_name"`
	FrameworkAdapters []string `yaml:"framework_adapters"`
	Frameworks        []string `yaml:"frameworks"`
	Service           struct {
		Name string `yaml:"name"`
	} `yaml:"service"`
}

func resolveService(root string, options Options) (string, string, scanConfig, []semantic.Diagnostic) {
	modulePath := readModulePath(filepath.Join(root, "go.mod"))
	configPath := options.ConfigPath
	if configPath == "" {
		configPath = filepath.Join(root, "si.yaml")
	}
	config, diagnostics := readScanConfig(root, configPath, options.ConfigYAML)
	serviceName := config.Service.Name
	if serviceName == "" {
		serviceName = config.ServiceName
	}
	if serviceName == "" {
		serviceName = config.Name
	}
	if options.ServiceName != "" {
		serviceName = options.ServiceName
	}
	if serviceName == "" && modulePath != "" {
		serviceName = filepath.Base(modulePath)
	}
	if serviceName == "" {
		serviceName = filepath.Base(root)
	}
	config.FrameworkAdapters = append(config.FrameworkAdapters, config.Frameworks...)
	return serviceName, modulePath, config, diagnostics
}

func endpointAdaptersByName(names []string) ([]EndpointAdapter, error) {
	available := make(map[string]EndpointAdapter)
	for _, adapter := range DefaultEndpointAdapters() {
		available[adapter.Name()] = adapter
	}
	selected := make([]EndpointAdapter, 0, len(names)+1)
	seen := make(map[string]struct{}, len(names)+1)
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		adapter, ok := available[name]
		if !ok || name == "unknown-http-router" {
			if name == "unknown-http-router" {
				continue
			}
			return nil, fmt.Errorf("unsupported framework adapter %q", name)
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		selected = append(selected, adapter)
	}
	if _, exists := seen["unknown-http-router"]; !exists {
		selected = append(selected, available["unknown-http-router"])
	}
	return selected, nil
}

func readModulePath(path string) string {
	contents, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	file, err := modfile.Parse(path, contents, nil)
	if err != nil || file.Module == nil {
		return ""
	}
	return file.Module.Mod.Path
}

func readScanConfig(root, configPath, inlineConfig string) (scanConfig, []semantic.Diagnostic) {
	path := configPath
	var contents []byte
	if inlineConfig != "" {
		contents = []byte(inlineConfig)
		path = "<request config>"
	} else {
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}
		var err error
		contents, err = os.ReadFile(path)
		if os.IsNotExist(err) {
			return scanConfig{}, nil
		}
		if err != nil {
			return scanConfig{}, []semantic.Diagnostic{{
				Severity: semantic.DiagnosticSeverityWarning,
				Code:     semantic.DiagnosticCodeConfigReadError,
				Message:  fmt.Sprintf("read scan config: %v", err),
				SourceLocation: semantic.SourceLocation{
					RelativePath: filepath.Base(path),
				},
			}}
		}
	}
	var config scanConfig
	if err := yaml.Unmarshal(contents, &config); err != nil {
		return scanConfig{}, []semantic.Diagnostic{{
			Severity: semantic.DiagnosticSeverityError,
			Code:     semantic.DiagnosticCodeInvalidConfig,
			Message:  fmt.Sprintf("parse scan config: %v", err),
			SourceLocation: semantic.SourceLocation{
				RelativePath: filepath.Base(path),
			},
		}}
	}
	return config, nil
}

func packageFiles(root string, pkg *packages.Package, includeTests bool) []string {
	paths := append([]string(nil), pkg.CompiledGoFiles...)
	paths = append(paths, pkg.GoFiles...)
	seen := make(map[string]struct{}, len(paths))
	files := make([]string, 0, len(paths))
	for _, path := range paths {
		relative, ok := relativeSourcePath(root, path)
		if !ok || isExcludedPath(relative) || (!includeTests && strings.HasSuffix(relative, "_test.go")) {
			continue
		}
		if _, exists := seen[relative]; exists {
			continue
		}
		seen[relative] = struct{}{}
		files = append(files, relative)
	}
	sort.Strings(files)
	return files
}

func relativeSourcePath(root, path string) (string, bool) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", false
	}
	relative, err := filepath.Rel(root, absPath)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(relative), true
}

func isExcludedPath(path string) bool {
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if part == "vendor" || (strings.HasPrefix(part, ".") && part != ".") {
			return true
		}
	}
	return false
}

func packageDiagnostics(root string, pkg *packages.Package) []semantic.Diagnostic {
	diagnostics := make([]semantic.Diagnostic, 0, len(pkg.Errors))
	for _, packageError := range pkg.Errors {
		code := semantic.DiagnosticCodePackageLoadError
		if packageError.Kind == packages.ParseError {
			code = semantic.DiagnosticCodeGoParseError
		}
		diagnostics = append(diagnostics, semantic.Diagnostic{
			Severity:       semantic.DiagnosticSeverityError,
			Code:           code,
			Message:        packageError.Msg,
			SourceLocation: packageErrorLocation(root, packageError.Pos),
		})
	}
	return diagnostics
}

func packageErrorLocation(root, position string) semantic.SourceLocation {
	parts := strings.Split(position, ":")
	if len(parts) < 3 {
		return semantic.SourceLocation{}
	}
	line, lineErr := strconv.Atoi(parts[len(parts)-2])
	column, columnErr := strconv.Atoi(parts[len(parts)-1])
	if lineErr != nil || columnErr != nil {
		return semantic.SourceLocation{}
	}
	path := strings.Join(parts[:len(parts)-2], ":")
	relative, ok := relativeSourcePath(root, path)
	if !ok {
		relative = filepath.ToSlash(path)
	}
	return semantic.SourceLocation{
		RelativePath: relative,
		StartLine:    int32(line),
		StartColumn:  int32(column),
	}
}
