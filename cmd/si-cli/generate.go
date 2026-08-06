package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zhuyanxi/axiom-insight/generator"
	"github.com/zhuyanxi/axiom-insight/generator/commit"
	"github.com/zhuyanxi/axiom-insight/generator/planner"
	"github.com/zhuyanxi/axiom-insight/generator/planner/logging"
	"github.com/zhuyanxi/axiom-insight/generator/planner/metrics"
	"github.com/zhuyanxi/axiom-insight/generator/planner/tracing"
	"github.com/zhuyanxi/axiom-insight/generator/policy"
	observabilityv1 "github.com/zhuyanxi/axiom-insight/ir/v1"
	"github.com/zhuyanxi/axiom-insight/plugins"
)

const (
	// cliGenerateMessageCode is the stable message code for generate
	// pipeline failures (plan, render, validate or commit).
	cliGenerateMessageCode = "CLI_GENERATE_ERROR"
	// generateReportSchema is the versioned GenerateReport contract.
	generateReportSchema = "cli.generate_report/v1"
)

// generateOptions carries the generate command flags. Optional values use
// pointers so "unset" is distinguishable from an explicit false/empty.
type generateOptions struct {
	configPath   string
	outputDir    string
	signals      []string
	include      []string
	exclude      []string
	includeTests bool
	strict       bool
	dryRun       bool
	force        bool
	format       string
	version      bool
}

// generateReport is the versioned, machine-readable command report. It
// never contains generated YAML content or sensitive values.
type generateReport struct {
	SchemaVersion          string            `json:"schema_version"`
	Status                 string            `json:"status"`
	CLIVersion             string            `json:"cli_version"`
	IRSchemaVersion        string            `json:"ir_schema_version"`
	GeneratorSchemaVersion string            `json:"generator_schema_version"`
	Service                string            `json:"service,omitempty"`
	Signals                []string          `json:"signals,omitempty"`
	CompletedStage         string            `json:"completed_stage"`
	PlannedFiles           []plannedFile     `json:"planned_files,omitempty"`
	DryRun                 bool              `json:"dry_run"`
	Written                []string          `json:"written,omitempty"`
	Diagnostics            []json.RawMessage `json:"diagnostics,omitempty"`
	Error                  *reportError      `json:"error,omitempty"`
}

type plannedFile struct {
	Name          string `json:"name"`
	Definitions   int    `json:"definitions"`
	SHA256        string `json:"sha256"`
	ExistedBefore bool   `json:"existed_before"`
}

type reportError struct {
	Code    string `json:"code,omitempty"`
	Stage   string `json:"stage,omitempty"`
	Message string `json:"message"`
}

// newGenerateCommand builds the `si generate` command.
func newGenerateCommand() *cobra.Command {
	options := generateOptions{}
	command := &cobra.Command{
		Use:   "generate [path]",
		Short: "Generate observability files from analyzed source",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) > 1 {
				return usageFailure(fmt.Errorf("generate accepts at most one path"))
			}
			return nil
		},
		RunE: func(command *cobra.Command, args []string) error {
			return executeGenerate(command, args, options)
		},
	}
	command.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return usageFailure(err)
	})
	command.Flags().StringVar(&options.configPath, "config", "", "path to si.yaml")
	command.Flags().StringVar(&options.outputDir, "output-dir", "", "output directory (default <source-root>/generate)")
	command.Flags().StringSliceVar(&options.signals, "signals", nil, "signals to generate: metrics,tracing,logging")
	command.Flags().StringSliceVar(&options.include, "include", nil, "package patterns to include")
	command.Flags().StringSliceVar(&options.exclude, "exclude", nil, "package patterns to exclude")
	command.Flags().BoolVar(&options.includeTests, "include-tests", false, "include Go test files")
	command.Flags().BoolVar(&options.strict, "strict", false, "fail on generator warnings")
	command.Flags().BoolVar(&options.dryRun, "dry-run", false, "plan and validate without writing files")
	command.Flags().BoolVar(&options.force, "force", false, "replace existing managed targets")
	command.Flags().StringVar(&options.format, "format", "text", "output format: text or json")
	command.Flags().BoolVar(&options.version, "version", false, "print CLI, IR and Generator schema versions")
	return command
}

// executeGenerate runs the Analyze -> Plan -> Render -> Validate -> Commit
// pipeline.
func executeGenerate(command *cobra.Command, args []string, options generateOptions) error {
	report := &generateReport{
		SchemaVersion:          generateReportSchema,
		Status:                 "success",
		CLIVersion:             cliVersion,
		IRSchemaVersion:        plugins.CurrentSchemaVersion,
		GeneratorSchemaVersion: generator.GeneratorVersion,
		CompletedStage:         "flags",
	}

	if options.version {
		fmt.Fprintf(command.OutOrStdout(), "si version: %s\nir_schema_version: %s\ngenerator_schema_version: %s\n",
			cliVersion, plugins.CurrentSchemaVersion, generator.GeneratorVersion)
		return nil
	}
	format := strings.ToLower(strings.TrimSpace(options.format))
	if format != "text" && format != "json" {
		return usageFailure(fmt.Errorf("unsupported format %q; use text or json", options.format))
	}

	reportJSON := format == "json"
	finish := func(err error) error {
		if !reportJSON {
			return err
		}
		// JSON mode: write exactly one complete report to stdout, mark
		// the error as reported and keep the exit code.
		if err != nil {
			report.Status = "failure"
			if commandFailure, ok := errors.AsType[*commandError](err); ok {
				report.Error = &reportError{Code: commandFailure.messageCode, Stage: report.CompletedStage, Message: commandFailure.err.Error()}
			} else {
				report.Error = &reportError{Code: "CLI_GENERATE_ERROR", Stage: report.CompletedStage, Message: err.Error()}
			}
		}
		contents, marshalErr := json.MarshalIndent(report, "", "  ")
		if marshalErr != nil {
			return internalFailure(fmt.Errorf("marshal generate report: %w", marshalErr))
		}
		contents = append(contents, '\n')
		if _, writeErr := command.OutOrStdout().Write(contents); writeErr != nil {
			return internalFailure(writeErr)
		}
		if err == nil {
			return nil
		}
		if failure, ok := errors.AsType[*commandError](err); ok {
			failure.reported = true
			return failure
		}
		return &commandError{exitCode: exitScanError, messageCode: cliGenerateMessageCode, err: err, reported: true}
	}

	sourceRoot := "."
	if len(args) == 1 {
		sourceRoot = args[0]
	}
	root, err := filepath.Abs(sourceRoot)
	if err != nil {
		return finish(usageFailure(fmt.Errorf("resolve source path %q: %w", sourceRoot, err)))
	}
	info, err := os.Stat(root)
	if err != nil {
		return finish(usageFailure(fmt.Errorf("invalid source path %q: %w", sourceRoot, err)))
	}
	if !info.IsDir() {
		return finish(usageFailure(fmt.Errorf("source path %q is not a directory", sourceRoot)))
	}

	config, configYAML, err := loadScanConfig(root, options.configPath)
	if err != nil {
		return finish(usageFailure(err))
	}
	if err := validateScanConfig(config); err != nil {
		return finish(usageFailure(err))
	}

	overrides := buildPolicyOverrides(command, options)
	resolvedPolicy, err := policy.Resolve(config.Generation, overrides)
	if err != nil {
		return finish(usageFailure(err))
	}
	report.Signals = append([]string(nil), resolvedPolicy.Signals...)
	report.Service = ""
	report.CompletedStage = "scan"

	scanOptions := scanOptions{
		configPath:   options.configPath,
		include:      options.include,
		exclude:      options.exclude,
		includeTests: options.includeTests,
	}
	request := buildAnalyzeRequest(root, configYAML, config, command, scanOptions)
	document, err := analyzeDocument(command.Context(), request)
	if err != nil {
		return finish(&commandError{exitCode: exitScanError, messageCode: cliGenerateMessageCode, err: fmt.Errorf("scan: %w", err)})
	}
	report.Service = document.GetService().GetName()

	plan, planReport, err := planDocument(command.Context(), document, *resolvedPolicy)
	if err != nil {
		return finish(&commandError{exitCode: exitScanError, messageCode: cliGenerateMessageCode, err: err})
	}
	_ = planReport
	report.CompletedStage = "render"

	outputDir := resolvedPolicy.OutputDir
	if options.outputDir != "" {
		outputDir = options.outputDir
	}
	if !filepath.IsAbs(outputDir) {
		outputDir = filepath.Join(root, outputDir)
	}
	outputDir = filepath.Clean(outputDir)

	// Render every selected signal in memory first; any failure aborts
	// before a single file is touched.
	var targets []commit.Target
	for _, signal := range resolvedPolicy.Signals {
		rendered, err := renderSignal(signal, plan, *resolvedPolicy)
		if err != nil {
			return finish(&commandError{exitCode: exitScanError, messageCode: cliGenerateMessageCode, err: err})
		}
		entry := plannedFile{Name: signalFileName(signal), Definitions: definitionsFor(signal, plan), SHA256: sha256Hex(rendered)}
		report.PlannedFiles = append(report.PlannedFiles, entry)
		targets = append(targets, commit.Target{Name: entry.Name, Contents: rendered})
	}
	report.CompletedStage = "validate"

	if options.dryRun {
		report.DryRun = true
		report.CompletedStage = "commit"
		if reportJSON {
			return finish(nil)
		}
		return finish(writeTextReport(command.OutOrStdout(), report))
	}

	writer := commit.OSFileWriter{}
	if err := writer.MkdirAll(outputDir, 0o755); err != nil {
		return finish(&commandError{exitCode: exitScanError, messageCode: cliGenerateMessageCode, err: fmt.Errorf("create output directory: %w", err)})
	}
	report.CompletedStage = "commit"

	generation, err := commit.New(writer, outputDir, targets)
	if err != nil {
		return finish(&commandError{exitCode: exitScanError, messageCode: cliGenerateMessageCode, err: err})
	}
	if err := generation.Prepare(options.force); err != nil {
		_ = generation.Cleanup()
		return finish(&commandError{exitCode: exitScanError, messageCode: cliGenerateMessageCode, err: err})
	}
	if err := generation.Commit(); err != nil {
		cleanupErr := generation.Cleanup()
		if cleanupErr != nil {
			err = fmt.Errorf("%w; additionally %v", err, cleanupErr)
		}
		return finish(&commandError{exitCode: exitScanError, messageCode: cliGenerateMessageCode, err: err})
	}
	if err := generation.Cleanup(); err != nil {
		return finish(&commandError{exitCode: exitScanError, messageCode: cliGenerateMessageCode, err: err})
	}

	for _, entry := range report.PlannedFiles {
		report.Written = append(report.Written, entry.Name)
	}
	if reportJSON {
		return finish(nil)
	}
	return finish(writeTextReport(command.OutOrStdout(), report))
}

// buildPolicyOverrides converts explicit CLI flags into policy overrides,
// preserving "unset" semantics.
func buildPolicyOverrides(command *cobra.Command, options generateOptions) *policy.Overrides {
	overrides := &policy.Overrides{}
	if command.Flags().Changed("signals") {
		overrides.Signals = append([]string(nil), options.signals...)
	}
	if command.Flags().Changed("strict") {
		overrides.Strict = &options.strict
	}
	if command.Flags().Changed("include-tests") {
		// include-tests only feeds the scan request, not the policy.
	}
	return overrides
}

// planDocument runs the deterministic planner with the Phase 1
// sub-planners.
func planDocument(ctx context.Context, document *observabilityv1.ObservabilityDocument, resolved policy.Policy) (*observabilityv1.GenerationPlan, planner.Report, error) {
	pipeline := planner.New(planner.Options{
		Metrics: metrics.CompositeMetricsPlanner{Endpoint: metrics.EndpointMetricsPlanner{}, Dependency: metrics.DependencyMetricsPlanner{}},
		Tracing: tracing.CompositeTracingPlanner{Root: tracing.EndpointRootSpanPlanner{}, Dependency: tracing.DependencyChildSpanPlanner{}, Internal: tracing.InternalCallSpanPlanner{}},
		Logging: logging.LoggingPlanner{},
	})
	return pipeline.Plan(ctx, document, resolved)
}

// renderSignal renders one signal's plan section into validated bytes.
func renderSignal(signal string, plan *observabilityv1.GenerationPlan, resolved policy.Policy) ([]byte, error) {
	switch signal {
	case planner.SignalMetrics:
		return generator.RenderMetricsPlan(plan, resolved)
	case planner.SignalTracing:
		return generator.RenderTracingPlan(plan, resolved)
	case planner.SignalLogging:
		return generator.RenderLoggingPlan(plan, resolved)
	default:
		return nil, fmt.Errorf("unsupported signal %q", signal)
	}
}

func signalFileName(signal string) string {
	switch signal {
	case planner.SignalMetrics:
		return "metrics.yaml"
	case planner.SignalTracing:
		return "otel.yaml"
	default:
		return "logging.yaml"
	}
}

func definitionsFor(signal string, plan *observabilityv1.GenerationPlan) int {
	switch signal {
	case planner.SignalMetrics:
		return len(plan.GetMetrics())
	case planner.SignalTracing:
		return len(plan.GetSpans())
	default:
		return len(plan.GetLogs())
	}
}

func sha256Hex(contents []byte) string {
	sum := sha256.Sum256(contents)
	return hex.EncodeToString(sum[:])
}

func writeTextReport(output io.Writer, report *generateReport) error {
	if report.Status == "failure" {
		message := "generate failed"
		if report.Error != nil && report.Error.Message != "" {
			message = report.Error.Message
		}
		_, err := fmt.Fprintf(output, "generate failed: %s\n", message)
		return err
	}
	for _, file := range report.PlannedFiles {
		state := "new"
		if file.ExistedBefore {
			state = "replace"
		}
		fmt.Fprintf(output, "%s %s %d definitions sha256:%s\n", state, file.Name, file.Definitions, file.SHA256)
	}
	return nil
}
