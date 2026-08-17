package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zhuyanxi/axiom-insight/dashboard"
	"github.com/zhuyanxi/axiom-insight/dashboard/model"
	"github.com/zhuyanxi/axiom-insight/dashboard/pipeline"
	"github.com/zhuyanxi/axiom-insight/generator"
	"github.com/zhuyanxi/axiom-insight/generator/policy"
	"github.com/zhuyanxi/axiom-insight/plugins"
)

const (
	cliDashboardMessageCode       = "CLI_DASHBOARD_ERROR"
	dashboardReportSchema         = "cli.dashboard_report/v1"
	dashboardFileName             = "dashboard.json"
	dashboardLockName             = ".si-dashboard.lock"
	dashboardTempName             = ".si-dashboard-tmp-dashboard.json"
	maxDashboardReportBytes       = 256 << 10
	maxDashboardReportDiagnostics = 512
	maxDashboardReportText        = 1024
)

type dashboardOptions struct {
	configPath   string
	outputDir    string
	include      []string
	exclude      []string
	includeTests bool
	strict       bool
	dryRun       bool
	force        bool
	format       string
	version      bool
}

type dashboardReport struct {
	SchemaVersion          string                      `json:"schema_version"`
	Status                 string                      `json:"status"`
	CLIVersion             string                      `json:"cli_version"`
	IRSchemaVersion        string                      `json:"ir_schema_version"`
	GeneratorSchemaVersion string                      `json:"generator_schema_version"`
	DashboardSchemaVersion string                      `json:"dashboard_schema_version"`
	GrafanaSchemaVersion   int                         `json:"grafana_schema_version"`
	Service                string                      `json:"service,omitempty"`
	CompletedStage         string                      `json:"completed_stage"`
	Dashboard              *dashboardReportSummary     `json:"dashboard,omitempty"`
	DryRun                 bool                        `json:"dry_run"`
	Written                []string                    `json:"written"`
	Diagnostics            []dashboardReportDiagnostic `json:"diagnostics"`
	Error                  *dashboardReportError       `json:"error,omitempty"`
}

type dashboardReportSummary struct {
	Name          string `json:"name"`
	PolicyDigest  string `json:"policy_digest"`
	SHA256        string `json:"sha256"`
	PanelCount    int    `json:"panel_count"`
	QueryCount    int    `json:"query_count"`
	RowCount      int    `json:"row_count"`
	ExistedBefore bool   `json:"existed_before"`
}

type dashboardReportDiagnostic struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Category string `json:"category,omitempty"`
	TargetID string `json:"target_id,omitempty"`
	Field    string `json:"field,omitempty"`
	Message  string `json:"message"`
}

type dashboardReportError struct {
	Code    string `json:"code"`
	Stage   string `json:"stage"`
	Message string `json:"message"`
}

type dashboardFileError struct {
	Code    string
	Stage   string
	Message string
}

func (failure *dashboardFileError) Error() string {
	return fmt.Sprintf("%s: %s: %s", failure.Code, failure.Stage, failure.Message)
}

func newDashboardCommand() *cobra.Command {
	options := dashboardOptions{}
	command := &cobra.Command{
		Use:   "dashboard [path]",
		Short: "Generate an offline Grafana dashboard",
		RunE: func(command *cobra.Command, args []string) error {
			return executeDashboard(command, args, options)
		},
	}
	command.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return usageFailure(err)
	})
	command.Flags().StringVar(&options.configPath, "config", "", "path to si.yaml")
	command.Flags().StringVar(&options.outputDir, "output-dir", "", "dashboard output directory (default <source-root>/dashboards)")
	command.Flags().StringSliceVar(&options.include, "include", nil, "package patterns to include")
	command.Flags().StringSliceVar(&options.exclude, "exclude", nil, "package patterns to exclude")
	command.Flags().BoolVar(&options.includeTests, "include-tests", false, "include Go test files")
	command.Flags().BoolVar(&options.strict, "strict", false, "fail on dashboard warnings")
	command.Flags().BoolVar(&options.dryRun, "dry-run", false, "plan and validate without writing files")
	command.Flags().BoolVar(&options.force, "force", false, "replace existing dashboard.json")
	command.Flags().StringVar(&options.format, "format", "text", "output format: text or json")
	command.Flags().BoolVar(&options.version, "version", false, "print CLI, IR, Generator, Dashboard and Grafana schema versions")
	return command
}

func executeDashboard(command *cobra.Command, args []string, options dashboardOptions) error {
	report := &dashboardReport{
		SchemaVersion:          dashboardReportSchema,
		Status:                 "success",
		CLIVersion:             cliVersion,
		IRSchemaVersion:        plugins.CurrentSchemaVersion,
		GeneratorSchemaVersion: generator.GeneratorVersion,
		DashboardSchemaVersion: model.ContractVersion,
		GrafanaSchemaVersion:   model.SchemaVersion,
		CompletedStage:         "flags",
		DryRun:                 options.dryRun,
		Written:                []string{},
		Diagnostics:            []dashboardReportDiagnostic{},
	}

	if options.version {
		fmt.Fprintf(command.OutOrStdout(), "si version: %s\nir_schema_version: %s\ngenerator_schema_version: %s\ndashboard_schema_version: %s\ngrafana_schema_version: %d\n",
			cliVersion, plugins.CurrentSchemaVersion, generator.GeneratorVersion, model.ContractVersion, model.SchemaVersion)
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
		if err != nil {
			report.Status = "failure"
			report.Error = makeDashboardReportError(err, report.CompletedStage)
		}
		contents, marshalErr := marshalDashboardReport(report)
		if marshalErr != nil {
			failure := internalFailure(fmt.Errorf("marshal dashboard report: %w", marshalErr))
			resetDashboardReportForEncodingFailure(report)
			var fallbackErr error
			contents, fallbackErr = marshalDashboardReport(report)
			if fallbackErr != nil {
				return failure
			}
			err = failure
		}
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
		return &commandError{exitCode: exitScanError, messageCode: cliDashboardMessageCode, err: err, reported: true}
	}
	if len(args) > 1 {
		return finish(usageFailure(fmt.Errorf("dashboard accepts at most one path")))
	}

	sourceRoot := "."
	if len(args) == 1 {
		sourceRoot = args[0]
	}
	root, err := filepath.Abs(sourceRoot)
	if err != nil {
		return finish(usageFailure(fmt.Errorf("resolve source path: %w", err)))
	}
	info, err := os.Stat(root)
	if err != nil {
		return finish(usageFailure(fmt.Errorf("invalid source path: %w", err)))
	}
	if !info.IsDir() {
		return finish(usageFailure(fmt.Errorf("source path is not a directory")))
	}

	config, configYAML, err := loadScanConfig(root, options.configPath)
	if err != nil {
		return finish(usageFailure(err))
	}
	if err := validateScanConfig(config); err != nil {
		return finish(usageFailure(err))
	}
	generationPolicy, err := policy.Resolve(config.Generation, nil)
	if err != nil {
		return finish(usageFailure(err))
	}
	dashboardOverrides := &dashboard.Overrides{}
	if command.Flags().Changed("output-dir") {
		dashboardOverrides.OutputDir = &options.outputDir
	}
	if command.Flags().Changed("strict") {
		dashboardOverrides.Strict = &options.strict
	}
	dashboardPolicy, err := dashboard.Resolve(config.Dashboard, dashboardOverrides)
	if err != nil {
		return finish(usageFailure(err))
	}
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
		return finish(dashboardFailure("scan", err))
	}
	report.Service = document.GetService().GetName()

	report.CompletedStage = "plan"
	generationPlan, _, err := planDocument(command.Context(), document, *generationPolicy)
	if err != nil {
		return finish(dashboardFailure("plan", err))
	}

	report.CompletedStage = "catalog"
	catalog, err := dashboard.BuildCatalog(document, generationPlan, *dashboardPolicy)
	if err != nil {
		return finish(dashboardFailure("catalog", err))
	}

	report.CompletedStage = "plan"
	dashboardPlan, err := pipeline.Build(catalog, *dashboardPolicy)
	if err != nil {
		return finish(dashboardFailure("plan", err))
	}
	for _, diagnostic := range dashboardPlan.Diagnostics() {
		report.Diagnostics = append(report.Diagnostics, dashboardReportDiagnostic{
			Code: diagnostic.Code, Severity: dashboardDiagnosticSeverity(diagnostic.Code),
			Category: dashboardDiagnosticCategory(diagnostic),
			TargetID: diagnostic.TargetID, Field: diagnostic.Field, Message: diagnostic.Message,
		})
	}
	report.CompletedStage = "validate"
	if firstDashboardWarningCode(dashboardPlan.Diagnostics()) != "" {
		report.Status = "warning"
	}
	if dashboardPolicy.Strict {
		if warningCode := firstDashboardWarningCode(dashboardPlan.Diagnostics()); warningCode != "" {
			return finish(dashboardFailure("validate", fmt.Errorf("%s: strict mode rejected dashboard warning", warningCode)))
		}
	}

	report.CompletedStage = "render"
	rendered, err := pipeline.Render(dashboardPlan)
	if err != nil {
		return finish(dashboardFailure("render", err))
	}
	report.CompletedStage = "validate"
	outputDir := dashboardPolicy.OutputDir
	if !filepath.IsAbs(outputDir) {
		outputDir = filepath.Join(root, outputDir)
	}
	outputDir = filepath.Clean(outputDir)
	existedBefore, err := inspectDashboardTarget(outputDir)
	if err != nil {
		return finish(dashboardFailure("validate", err))
	}
	report.Dashboard = &dashboardReportSummary{
		Name: dashboardPlan.Title(), PolicyDigest: dashboardPlan.PolicyDigest(),
		SHA256: rendered.SHA256, PanelCount: rendered.PanelCount,
		QueryCount: rendered.QueryCount, RowCount: rendered.RowCount,
		ExistedBefore: existedBefore,
	}

	if options.dryRun {
		report.DryRun = true
		report.CompletedStage = "commit"
		if reportJSON {
			return finish(nil)
		}
		return finish(writeDashboardTextReport(command.OutOrStdout(), report))
	}

	report.CompletedStage = "commit"
	if existed, err := commitDashboardFile(outputDir, rendered.Bytes, options.force); err != nil {
		return finish(dashboardFailure("commit", err))
	} else if existed != report.Dashboard.ExistedBefore {
		return finish(dashboardFailure("commit", fmt.Errorf("dashboard target changed during commit")))
	}
	report.Written = []string{dashboardFileName}
	if reportJSON {
		return finish(nil)
	}
	return finish(writeDashboardTextReport(command.OutOrStdout(), report))
}

func makeDashboardReportError(err error, stage string) *dashboardReportError {
	code := dashboardErrorCode(err)
	if code == "" {
		if failure, ok := errors.AsType[*commandError](err); ok {
			code = failure.messageCode
		}
	}
	return &dashboardReportError{Code: code, Stage: stage, Message: dashboardReportMessage(code, stage)}
}

func dashboardReportMessage(code, stage string) string {
	if code == "" {
		return stage + " stage failed"
	}
	return fmt.Sprintf("%s stage failed (%s)", stage, code)
}

func marshalDashboardReport(report *dashboardReport) ([]byte, error) {
	normalizeDashboardReport(report)
	contents, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return nil, err
	}
	contents = append(contents, '\n')
	if len(contents) > maxDashboardReportBytes {
		return nil, fmt.Errorf("report exceeds %d-byte limit", maxDashboardReportBytes)
	}
	return contents, nil
}

func normalizeDashboardReport(report *dashboardReport) {
	if report.Written == nil {
		report.Written = []string{}
	}
	if report.Diagnostics == nil {
		report.Diagnostics = []dashboardReportDiagnostic{}
	}
	report.CLIVersion = trimDashboardReportField(report.CLIVersion, 64)
	report.IRSchemaVersion = trimDashboardReportField(report.IRSchemaVersion, 64)
	report.GeneratorSchemaVersion = trimDashboardReportField(report.GeneratorSchemaVersion, 64)
	report.DashboardSchemaVersion = trimDashboardReportField(report.DashboardSchemaVersion, 64)
	report.Service = trimDashboardReportField(report.Service, 255)
	for index := range report.Diagnostics {
		report.Diagnostics[index].Code = trimDashboardReportField(report.Diagnostics[index].Code, 96)
		report.Diagnostics[index].Severity = trimDashboardReportText(report.Diagnostics[index].Severity)
		report.Diagnostics[index].Category = trimDashboardReportText(report.Diagnostics[index].Category)
		report.Diagnostics[index].TargetID = trimDashboardReportText(report.Diagnostics[index].TargetID)
		report.Diagnostics[index].Field = trimDashboardReportText(report.Diagnostics[index].Field)
		report.Diagnostics[index].Message = trimDashboardReportText(report.Diagnostics[index].Message)
	}
	sort.SliceStable(report.Diagnostics, func(left, right int) bool {
		leftDiagnostic, rightDiagnostic := report.Diagnostics[left], report.Diagnostics[right]
		if dashboardSeverityRank(leftDiagnostic.Severity) != dashboardSeverityRank(rightDiagnostic.Severity) {
			return dashboardSeverityRank(leftDiagnostic.Severity) < dashboardSeverityRank(rightDiagnostic.Severity)
		}
		if leftDiagnostic.Category != rightDiagnostic.Category {
			return leftDiagnostic.Category < rightDiagnostic.Category
		}
		if leftDiagnostic.TargetID != rightDiagnostic.TargetID {
			return leftDiagnostic.TargetID < rightDiagnostic.TargetID
		}
		if leftDiagnostic.Code != rightDiagnostic.Code {
			return leftDiagnostic.Code < rightDiagnostic.Code
		}
		if leftDiagnostic.Field != rightDiagnostic.Field {
			return leftDiagnostic.Field < rightDiagnostic.Field
		}
		return leftDiagnostic.Message < rightDiagnostic.Message
	})
	if len(report.Diagnostics) > maxDashboardReportDiagnostics {
		report.Diagnostics = report.Diagnostics[:maxDashboardReportDiagnostics]
	}
	if report.Dashboard != nil {
		report.Dashboard.Name = trimDashboardReportField(report.Dashboard.Name, 255)
		report.Dashboard.PolicyDigest = trimDashboardReportField(report.Dashboard.PolicyDigest, 64)
		report.Dashboard.SHA256 = trimDashboardReportField(report.Dashboard.SHA256, 64)
	}
	if report.Error != nil {
		report.Error.Code = trimDashboardReportField(report.Error.Code, 96)
		report.Error.Stage = trimDashboardReportText(report.Error.Stage)
		report.Error.Message = trimDashboardReportText(report.Error.Message)
	}
}

func trimDashboardReportField(value string, limit int) string {
	value = trimDashboardReportText(value)
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

func resetDashboardReportForEncodingFailure(report *dashboardReport) {
	report.Status = "failure"
	report.Service = ""
	report.Dashboard = nil
	report.Written = []string{}
	report.Diagnostics = []dashboardReportDiagnostic{}
	report.Error = &dashboardReportError{
		Code: cliInternalMessageCode, Stage: report.CompletedStage,
		Message: "dashboard report could not be encoded within its size limit",
	}
}

func trimDashboardReportText(value string) string {
	value = strings.Map(func(character rune) rune {
		if character < 0x20 || character == 0x7f {
			return ' '
		}
		return character
	}, value)
	if len(value) <= maxDashboardReportText {
		return value
	}
	return value[:maxDashboardReportText-3] + "..."
}

func dashboardSeverityRank(severity string) int {
	switch severity {
	case "error":
		return 0
	case "warning":
		return 1
	default:
		return 2
	}
}

func dashboardDiagnosticCategory(diagnostic dashboard.Diagnostic) string {
	for _, category := range []string{"overview", "http", "rpc", "kafka", "database", "cache"} {
		if strings.Contains(diagnostic.Field, category) {
			return category
		}
	}
	return "catalog"
}

func dashboardFailure(stage string, err error) error {
	code := dashboardErrorCode(err)
	if code == "" {
		code = cliDashboardMessageCode
	}
	return &commandError{exitCode: exitScanError, messageCode: code, err: fmt.Errorf("%s: %w", stage, err)}
}

func dashboardErrorCode(err error) string {
	var fileFailure *dashboardFileError
	if errors.As(err, &fileFailure) {
		return fileFailure.Code
	}
	var catalogFailure *dashboard.CatalogError
	if errors.As(err, &catalogFailure) {
		return catalogFailure.Code
	}
	var catalogFailures *dashboard.CatalogErrors
	if errors.As(err, &catalogFailures) && len(catalogFailures.Violations()) > 0 {
		return catalogFailures.Violations()[0].Code
	}
	var configFailures *dashboard.ConfigErrors
	if errors.As(err, &configFailures) {
		return dashboard.CodeInvalidConfig
	}
	for _, code := range []string{
		dashboard.CodeInvalidConfig, dashboard.CodeInvalidIR, dashboard.CodeDanglingReference,
		dashboard.CodeUnsupportedSchema, dashboard.CodeMissingRequiredMetric, dashboard.CodeUnsupportedTarget,
		dashboard.CodeEmptyCategory, dashboard.CodePanelLimitExceeded, dashboard.CodeNameCollision,
		dashboard.CodeSensitiveValueDropped, dashboard.CodeRenderError, dashboard.CodeOutputExists,
		dashboard.CodeUnsafeTarget,
	} {
		if strings.Contains(err.Error(), code) {
			return code
		}
	}
	return ""
}

func dashboardDiagnosticSeverity(code string) string {
	if isDashboardWarning(code) {
		return "warning"
	}
	return "info"
}

func isDashboardWarning(code string) bool {
	switch code {
	case dashboard.CodeMissingRequiredMetric, dashboard.CodeUnsupportedTarget,
		dashboard.CodeNameCollision, dashboard.CodeSensitiveValueDropped:
		return true
	default:
		return false
	}
}

func firstDashboardWarningCode(diagnostics []dashboard.Diagnostic) string {
	for _, diagnostic := range diagnostics {
		if isDashboardWarning(diagnostic.Code) {
			return diagnostic.Code
		}
	}
	return ""
}

func writeDashboardTextReport(output io.Writer, report *dashboardReport) error {
	if report.Status == "failure" {
		if report.Error != nil {
			_, err := fmt.Fprintf(output, "dashboard failed: %s\n", report.Error.Message)
			return err
		}
		_, err := io.WriteString(output, "dashboard failed\n")
		return err
	}
	if report.DryRun {
		_, err := fmt.Fprintf(output, "planned %s panels:%d queries:%d rows:%d sha256:%s\n",
			dashboardFileName, report.Dashboard.PanelCount, report.Dashboard.QueryCount,
			report.Dashboard.RowCount, report.Dashboard.SHA256)
		return err
	}
	_, err := fmt.Fprintf(output, "written %s panels:%d queries:%d rows:%d sha256:%s\n",
		dashboardFileName, report.Dashboard.PanelCount, report.Dashboard.QueryCount,
		report.Dashboard.RowCount, report.Dashboard.SHA256)
	return err
}

func inspectDashboardTarget(outputDir string) (bool, error) {
	info, err := os.Lstat(outputDir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, &dashboardFileError{Code: dashboard.CodeInvalidConfig, Stage: "commit", Message: "cannot inspect output directory"}
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return false, &dashboardFileError{Code: dashboard.CodeUnsafeTarget, Stage: "commit", Message: "output directory must be a real directory"}
	}
	targetPath := filepath.Join(outputDir, dashboardFileName)
	targetInfo, err := os.Lstat(targetPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, &dashboardFileError{Code: dashboard.CodeInvalidConfig, Stage: "commit", Message: "cannot inspect dashboard target"}
	}
	if targetInfo.Mode()&os.ModeSymlink != 0 || !targetInfo.Mode().IsRegular() || hardLinkCount(targetInfo) > 1 {
		return false, &dashboardFileError{Code: dashboard.CodeUnsafeTarget, Stage: "commit", Message: "dashboard target must be a single-link regular file"}
	}
	return true, nil
}

func hardLinkCount(info os.FileInfo) uint64 {
	value := reflect.ValueOf(info.Sys())
	if !value.IsValid() {
		return 1
	}
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return 1
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return 1
	}
	field := value.FieldByName("Nlink")
	if !field.IsValid() {
		return 1
	}
	switch field.Kind() {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return field.Uint()
	default:
		return 1
	}
}

func commitDashboardFile(outputDir string, contents []byte, force bool) (existed bool, resultErr error) {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return false, &dashboardFileError{Code: dashboard.CodeInvalidConfig, Stage: "commit", Message: "cannot create output directory"}
	}
	if _, err := inspectDashboardTarget(outputDir); err != nil {
		return false, err
	}
	lockPath := filepath.Join(outputDir, dashboardLockName)
	lock, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		code := dashboard.CodeOutputExists
		message := "another dashboard generation is already running"
		if !os.IsExist(err) {
			code = dashboard.CodeInvalidConfig
			message = "cannot acquire dashboard generation lock"
		}
		return false, &dashboardFileError{Code: code, Stage: "lock", Message: message}
	}
	defer func() {
		if cleanupErr := os.Remove(lockPath); cleanupErr != nil && !os.IsNotExist(cleanupErr) && resultErr == nil {
			resultErr = &dashboardFileError{Code: dashboard.CodeRenderError, Stage: "commit", Message: "cannot clean dashboard lock"}
		}
	}()
	if err := lock.Close(); err != nil {
		return false, &dashboardFileError{Code: dashboard.CodeRenderError, Stage: "commit", Message: "cannot close dashboard lock"}
	}
	existed, err = inspectDashboardTarget(outputDir)
	if err != nil {
		return false, err
	}
	if existed && !force {
		return existed, &dashboardFileError{Code: dashboard.CodeOutputExists, Stage: "commit", Message: "dashboard.json already exists; use --force to replace it"}
	}
	tempPath := filepath.Join(outputDir, dashboardTempName)
	tempCreated := false
	defer func() {
		if tempCreated {
			if cleanupErr := os.Remove(tempPath); cleanupErr != nil && !os.IsNotExist(cleanupErr) && resultErr == nil {
				resultErr = &dashboardFileError{Code: dashboard.CodeRenderError, Stage: "commit", Message: "cannot clean temporary dashboard file"}
			}
		}
	}()
	file, err := os.OpenFile(tempPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		code := dashboard.CodeOutputExists
		message := "temporary dashboard path already exists"
		if !os.IsExist(err) {
			code = dashboard.CodeInvalidConfig
			message = "cannot create temporary dashboard file"
		}
		return existed, &dashboardFileError{Code: code, Stage: "commit", Message: message}
	}
	tempCreated = true
	if err := writeDashboardBytes(file, contents); err != nil {
		if closeErr := file.Close(); closeErr != nil {
			return existed, &dashboardFileError{Code: dashboard.CodeRenderError, Stage: "commit", Message: "cannot close temporary dashboard file after write failure"}
		}
		return existed, &dashboardFileError{Code: dashboard.CodeRenderError, Stage: "commit", Message: "cannot write temporary dashboard file"}
	}
	if err := file.Sync(); err != nil {
		if closeErr := file.Close(); closeErr != nil {
			return existed, &dashboardFileError{Code: dashboard.CodeRenderError, Stage: "commit", Message: "cannot close temporary dashboard file after sync failure"}
		}
		return existed, &dashboardFileError{Code: dashboard.CodeRenderError, Stage: "commit", Message: "cannot sync temporary dashboard file"}
	}
	if err := file.Close(); err != nil {
		return existed, &dashboardFileError{Code: dashboard.CodeRenderError, Stage: "commit", Message: "cannot close temporary dashboard file"}
	}
	if err := os.Rename(tempPath, filepath.Join(outputDir, dashboardFileName)); err != nil {
		return existed, &dashboardFileError{Code: dashboard.CodeRenderError, Stage: "commit", Message: "cannot atomically replace dashboard file"}
	}
	tempCreated = false
	directory, err := os.Open(outputDir)
	if err != nil {
		return existed, &dashboardFileError{Code: dashboard.CodeRenderError, Stage: "commit", Message: "cannot open output directory for sync"}
	}
	if err := directory.Sync(); err != nil {
		if closeErr := directory.Close(); closeErr != nil {
			return existed, &dashboardFileError{Code: dashboard.CodeRenderError, Stage: "commit", Message: "cannot close output directory after sync failure"}
		}
		return existed, &dashboardFileError{Code: dashboard.CodeRenderError, Stage: "commit", Message: "cannot sync output directory"}
	}
	if err := directory.Close(); err != nil {
		return existed, &dashboardFileError{Code: dashboard.CodeRenderError, Stage: "commit", Message: "cannot close output directory"}
	}
	return existed, nil
}

func writeDashboardBytes(file *os.File, contents []byte) error {
	for len(contents) > 0 {
		written, err := file.Write(contents)
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		contents = contents[written:]
	}
	return nil
}
