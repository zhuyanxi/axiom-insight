package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/zhuyanxi/axiom-insight/compiler/semantic"
	"github.com/zhuyanxi/axiom-insight/generator/policy"
	observabilityv1 "github.com/zhuyanxi/axiom-insight/ir/v1"
	"github.com/zhuyanxi/axiom-insight/plugins"
	"google.golang.org/protobuf/encoding/protojson"
)

const (
	exitScanError          = 1
	exitUsageError         = 2
	defaultCLIVersion      = "v0.1.0"
	cliUsageMessageCode    = "CLI_INVALID_ARGUMENT"
	cliScanMessageCode     = "CLI_SCAN_ERROR"
	cliInternalMessageCode = "CLI_INTERNAL_ERROR"
)

var cliVersion = defaultCLIVersion

type commandError struct {
	exitCode    int
	messageCode string
	err         error
}

func (failure *commandError) Error() string {
	return fmt.Sprintf("%s: %v", failure.messageCode, failure.err)
}

func (failure *commandError) Unwrap() error {
	return failure.err
}

type scanOptions struct {
	configPath   string
	format       string
	output       string
	include      []string
	exclude      []string
	includeTests bool
	version      bool
}

type scanConfig struct {
	Name              string   `mapstructure:"name"`
	ServiceName       string   `mapstructure:"service_name"`
	Language          string   `mapstructure:"language"`
	Languages         []string `mapstructure:"languages"`
	EnabledLanguage   string   `mapstructure:"enabled_language"`
	EnabledLanguages  []string `mapstructure:"enabled_languages"`
	IncludeTests      bool     `mapstructure:"include_tests"`
	Include           []string `mapstructure:"include"`
	Exclude           []string `mapstructure:"exclude"`
	FrameworkAdapters []string `mapstructure:"framework_adapters"`
	Frameworks        []string `mapstructure:"frameworks"`
	Service           struct {
		Name string `mapstructure:"name"`
	} `mapstructure:"service"`
	// Generation holds the strictly decoded si.yaml `generation` node. It
	// has no mapstructure tag on purpose: Viper decodes the scan fields
	// only, and loadScanConfig fills this field from the strict decoder so
	// unknown generation fields are rejected with GEN_INVALID_CONFIG.
	Generation *policy.GenerationConfig
}

type jsonSummaryItem struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type jsonScanResult struct {
	SchemaVersion string            `json:"schema_version"`
	Summary       []jsonSummaryItem `json:"summary"`
	Document      json.RawMessage   `json:"document"`
	Diagnostics   json.RawMessage   `json:"diagnostics"`
}

var supportedFrameworkAdapters = map[string]struct{}{
	"net/http":                  {},
	"github.com/gorilla/mux":    {},
	"grpc":                      {},
	"github.com/robfig/cron":    {},
	"github.com/robfig/cron/v3": {},
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	command := newRootCommand(stdout, stderr)
	command.SetArgs(args)
	if err := command.Execute(); err != nil {
		var failure *commandError
		if !errors.As(err, &failure) {
			failure = internalFailure(err)
		}
		fmt.Fprintln(stderr, failure)
		return failure.exitCode
	}
	return 0
}

func newRootCommand(stdout, stderr io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use:           "si",
		Short:         "Offline Go observability scanner",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return usageFailure(err)
	})

	options := scanOptions{}
	scan := &cobra.Command{
		Use:   "scan [path]",
		Short: "Scan local Go source and print observability summary",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) > 1 {
				return usageFailure(fmt.Errorf("scan accepts at most one path"))
			}
			return nil
		},
		RunE: func(command *cobra.Command, args []string) error {
			return executeScan(command, args, options)
		},
	}
	scan.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return usageFailure(err)
	})
	scan.Flags().StringVar(&options.configPath, "config", "", "path to si.yaml")
	scan.Flags().StringVar(&options.format, "format", "text", "output format: text or json")
	scan.Flags().StringVar(&options.output, "output", "", "write JSON result to file")
	scan.Flags().StringSliceVar(&options.include, "include", nil, "package patterns to include")
	scan.Flags().StringSliceVar(&options.exclude, "exclude", nil, "package patterns to exclude")
	scan.Flags().BoolVar(&options.includeTests, "include-tests", false, "include Go test files")
	scan.Flags().BoolVar(&options.version, "version", false, "print CLI and IR schema versions")
	root.AddCommand(scan)
	return root
}

func executeScan(command *cobra.Command, args []string, options scanOptions) error {
	if options.version {
		fmt.Fprintf(command.OutOrStdout(), "si version: %s\nir_schema_version: %s\n", cliVersion, plugins.CurrentSchemaVersion)
		return nil
	}
	format := strings.ToLower(strings.TrimSpace(options.format))
	if format != "text" && format != "json" {
		return usageFailure(fmt.Errorf("unsupported format %q; use text or json", options.format))
	}
	if options.output != "" && format != "json" {
		return usageFailure(fmt.Errorf("--output requires --format json"))
	}

	sourceRoot := "."
	if len(args) == 1 {
		sourceRoot = args[0]
	}
	root, err := filepath.Abs(sourceRoot)
	if err != nil {
		return usageFailure(fmt.Errorf("resolve source path %q: %w", sourceRoot, err))
	}
	info, err := os.Stat(root)
	if err != nil {
		return usageFailure(fmt.Errorf("invalid source path %q: %w", sourceRoot, err))
	}
	if !info.IsDir() {
		return usageFailure(fmt.Errorf("source path %q is not a directory", sourceRoot))
	}

	config, configYAML, err := loadScanConfig(root, options.configPath)
	if err != nil {
		return usageFailure(err)
	}
	if err := validateScanConfig(config); err != nil {
		return usageFailure(err)
	}
	// The generation node is part of the si.yaml contract: an invalid
	// value must fail the command with a config error instead of being
	// silently ignored. scan itself does not consume the policy.
	if _, err := policy.Resolve(config.Generation, nil); err != nil {
		return usageFailure(err)
	}

	request := &observabilityv1.AnalyzeRequest{
		SourceRoot:    root,
		SchemaVersion: plugins.CurrentSchemaVersion,
		Config:        configYAML,
		IncludeTests:  config.IncludeTests,
		Include:       append([]string(nil), config.Include...),
		Exclude:       append([]string(nil), config.Exclude...),
	}
	if command.Flags().Changed("include-tests") {
		request.IncludeTests = options.includeTests
	}
	if command.Flags().Changed("include") {
		request.Include = append([]string(nil), options.include...)
	}
	if command.Flags().Changed("exclude") {
		request.Exclude = append([]string(nil), options.exclude...)
	}

	transport := plugins.NewInProcessTransport(nil)
	analyzer, err := transport.Connect(command.Context())
	if err != nil {
		return scanFailure(fmt.Errorf("connect analyzer: %w", err))
	}
	defer transport.Close()
	response, err := analyzer.Analyze(command.Context(), request)
	if err != nil {
		return scanFailure(fmt.Errorf("scan %q: %w", sourceRoot, err))
	}
	if response == nil || response.Document == nil {
		return scanFailure(fmt.Errorf("scan %q returned no IR document", sourceRoot))
	}

	summary := summarizeIR(response.Document)
	if format == "json" {
		contents, err := marshalScanResult(response.Document, summary)
		if err != nil {
			return scanFailure(fmt.Errorf("marshal scan result: %w", err))
		}
		contents = append(contents, '\n')
		if options.output == "" {
			_, err = command.OutOrStdout().Write(contents)
		} else {
			err = os.WriteFile(options.output, contents, 0o644)
		}
		if err != nil {
			return scanFailure(fmt.Errorf("write scan result: %w", err))
		}
		return nil
	}

	writeTextSummary(command.OutOrStdout(), response.Document, summary)
	return nil
}

func loadScanConfig(root, configPath string) (scanConfig, string, error) {
	settings := viper.New()
	if configPath == "" {
		settings.SetConfigName("si")
		settings.SetConfigType("yaml")
		settings.AddConfigPath(root)
	} else {
		path := configPath
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}
		settings.SetConfigFile(path)
	}

	if err := settings.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if configPath == "" && errors.As(err, &notFound) {
			return scanConfig{}, "", nil
		}
		return scanConfig{}, "", fmt.Errorf("read scan config: %w", err)
	}

	var config scanConfig
	if err := settings.Unmarshal(&config); err != nil {
		return scanConfig{}, "", fmt.Errorf("decode scan config: %w", err)
	}
	contents, err := os.ReadFile(settings.ConfigFileUsed())
	if err != nil {
		return scanConfig{}, "", fmt.Errorf("read scan config contents: %w", err)
	}
	generation, err := policy.DecodeConfigFile(contents)
	if err != nil {
		return scanConfig{}, "", fmt.Errorf("%s: decode generation config: %w", policy.CodeInvalidConfig, err)
	}
	config.Generation = generation
	return config, string(contents), nil
}

func validateScanConfig(config scanConfig) error {
	languages := append([]string(nil), config.Languages...)
	languages = append(languages, config.EnabledLanguages...)
	if config.Language != "" {
		languages = append(languages, config.Language)
	}
	if config.EnabledLanguage != "" {
		languages = append(languages, config.EnabledLanguage)
	}
	for _, language := range languages {
		language = strings.ToLower(strings.TrimSpace(language))
		if language != "go" && language != "golang" {
			return fmt.Errorf("unsupported language %q; only go is available", language)
		}
	}

	frameworks := append([]string(nil), config.FrameworkAdapters...)
	frameworks = append(frameworks, config.Frameworks...)
	for _, framework := range frameworks {
		framework = strings.TrimSpace(framework)
		if _, ok := supportedFrameworkAdapters[framework]; !ok {
			return fmt.Errorf("unsupported framework adapter %q", framework)
		}
	}
	return nil
}

func summarizeIR(document *observabilityv1.ObservabilityDocument) semantic.ScanSummary {
	var summary semantic.ScanSummary
	for _, endpoint := range document.GetEndpoints() {
		switch endpoint.GetKind() {
		case observabilityv1.EndpointKind_HTTP_HANDLER:
			summary.HTTPHandlers++
		case observabilityv1.EndpointKind_GRPC_HANDLER:
			summary.GRPCHandlers++
		case observabilityv1.EndpointKind_CRON_JOB:
			summary.CronJobs++
		}
	}
	for _, dependency := range document.GetDependencies() {
		switch dependency.GetKind() {
		case observabilityv1.DependencyKind_KAFKA_CONSUMER:
			summary.KafkaConsumers++
		case observabilityv1.DependencyKind_KAFKA_PRODUCER:
			summary.KafkaProducers++
		case observabilityv1.DependencyKind_SQL:
			summary.SQL++
		case observabilityv1.DependencyKind_REDIS:
			summary.Redis++
		case observabilityv1.DependencyKind_HTTP_CLIENT:
			summary.HTTPClients++
		case observabilityv1.DependencyKind_RPC_CLIENT:
			summary.RPCClients++
		}
	}
	summary.Diagnostics = len(document.GetDiagnostics())
	return summary
}

func writeTextSummary(output io.Writer, document *observabilityv1.ObservabilityDocument, summary semantic.ScanSummary) {
	fmt.Fprintf(output, "service: %s\n", document.GetService().GetName())
	fmt.Fprintf(output, "schema_version: %s\n", document.GetSchemaVersion())
	for _, item := range summary.Items() {
		fmt.Fprintf(output, "%s: %d\n", item.Name, item.Count)
	}
}

func marshalScanResult(document *observabilityv1.ObservabilityDocument, summary semantic.ScanSummary) ([]byte, error) {
	protoOptions := protojson.MarshalOptions{UseProtoNames: true, Indent: "  "}
	documentJSON, err := protoOptions.Marshal(document)
	if err != nil {
		return nil, err
	}
	diagnosticsJSON, err := marshalDiagnostics(document.GetDiagnostics())
	if err != nil {
		return nil, err
	}
	result := jsonScanResult{
		SchemaVersion: document.GetSchemaVersion(),
		Summary:       jsonSummaryItems(summary),
		Document:      documentJSON,
		Diagnostics:   diagnosticsJSON,
	}
	return json.MarshalIndent(result, "", "  ")
}

func marshalDiagnostics(diagnostics []*observabilityv1.Diagnostic) (json.RawMessage, error) {
	wrapper := &observabilityv1.ObservabilityDocument{Diagnostics: diagnostics}
	contents, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(wrapper)
	if err != nil {
		return nil, err
	}
	var value struct {
		Diagnostics json.RawMessage `json:"diagnostics"`
	}
	if err := json.Unmarshal(contents, &value); err != nil {
		return nil, err
	}
	if len(value.Diagnostics) == 0 {
		return json.RawMessage("[]"), nil
	}
	return value.Diagnostics, nil
}

func jsonSummaryItems(summary semantic.ScanSummary) []jsonSummaryItem {
	items := summary.Items()
	result := make([]jsonSummaryItem, 0, len(items))
	for _, item := range items {
		result = append(result, jsonSummaryItem{Name: item.Name, Count: item.Count})
	}
	return result
}

func usageFailure(err error) error {
	return &commandError{exitCode: exitUsageError, messageCode: cliUsageMessageCode, err: err}
}

func scanFailure(err error) error {
	return &commandError{exitCode: exitScanError, messageCode: cliScanMessageCode, err: err}
}

func internalFailure(err error) *commandError {
	return &commandError{exitCode: exitScanError, messageCode: cliInternalMessageCode, err: err}
}
