package goanalyzer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/pingli/axiom-insight/compiler/semantic"
	"golang.org/x/mod/modfile"
	"golang.org/x/tools/go/packages"
	"gopkg.in/yaml.v3"
)

const schemaVersion = "v1"

type Options struct {
	IncludeTests bool
	ServiceName  string
	ConfigPath   string
	Env          []string
	BuildFlags   []string
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

	serviceName, modulePath, configDiagnostics := resolveService(root, options)
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
	sort.SliceStable(document.Diagnostics, func(left, right int) bool {
		if document.Diagnostics[left].Code != document.Diagnostics[right].Code {
			return document.Diagnostics[left].Code < document.Diagnostics[right].Code
		}
		return document.Diagnostics[left].Message < document.Diagnostics[right].Message
	})

	return document, nil
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
	loaded, err := packages.Load(config, "./...")
	if err != nil {
		return nil, fmt.Errorf("load Go packages: %w", err)
	}
	return loaded, nil
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
	Name        string `yaml:"name"`
	ServiceName string `yaml:"service_name"`
	Service     struct {
		Name string `yaml:"name"`
	} `yaml:"service"`
}

func resolveService(root string, options Options) (string, string, []semantic.Diagnostic) {
	modulePath := readModulePath(filepath.Join(root, "go.mod"))
	configPath := options.ConfigPath
	if configPath == "" {
		configPath = filepath.Join(root, "si.yaml")
	}
	config, diagnostics := readScanConfig(root, configPath)
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
	return serviceName, modulePath, diagnostics
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

func readScanConfig(root, path string) (scanConfig, []semantic.Diagnostic) {
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	contents, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return scanConfig{}, nil
	}
	if err != nil {
		return scanConfig{}, []semantic.Diagnostic{{
			Severity: semantic.DiagnosticSeverityWarning,
			Code:     "CONFIG_READ_ERROR",
			Message:  fmt.Sprintf("read scan config: %v", err),
			SourceLocation: semantic.SourceLocation{
				RelativePath: filepath.Base(path),
			},
		}}
	}
	var config scanConfig
	if err := yaml.Unmarshal(contents, &config); err != nil {
		return scanConfig{}, []semantic.Diagnostic{{
			Severity: semantic.DiagnosticSeverityError,
			Code:     "INVALID_CONFIG",
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
		code := "PACKAGE_LOAD_ERROR"
		if packageError.Kind == packages.ParseError {
			code = "GO_PARSE_ERROR"
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
