package generator

import (
	"fmt"
	"slices"

	"github.com/zhuyanxi/axiom-insight/generator/naming"
	"github.com/zhuyanxi/axiom-insight/generator/policy"
	observabilityv1 "github.com/zhuyanxi/axiom-insight/ir/v1"
)

// RenderLoggingPlan converts the logs section of a validated
// GenerationPlan into the closed logging.yaml contract and renders it to
// canonical bytes. The file expresses the immutable built-in redaction
// rules plus user additions, event conditions in the finite status order,
// and runtime-context correlation bindings — it never contains generated
// timestamps, fake request/trace/span IDs or host information. The
// rendered bytes are immediately re-parsed with the strict decoder,
// re-validated semantically and scanned for sensitive constants.
//
// Errors carry the GEN_RENDER_ERROR context and the offending LogPlan ID;
// no partial bytes are ever returned. The function touches no filesystem,
// environment, clock or network.
func RenderLoggingPlan(plan *observabilityv1.GenerationPlan, resolved policy.Policy) ([]byte, error) {
	document, err := loggingDocument(plan, resolved)
	if err != nil {
		return nil, err
	}
	contents, err := RenderLogging(document)
	if err != nil {
		return nil, renderFailure("", err)
	}
	reparsed, err := DecodeLogging(contents)
	if err != nil {
		return nil, renderFailure("", err)
	}
	if err := scanSensitiveConstants(reparsed); err != nil {
		return nil, renderFailure("", err)
	}
	return contents, nil
}

// loggingDocument converts the plan into the typed logging.yaml model.
func loggingDocument(plan *observabilityv1.GenerationPlan, resolved policy.Policy) (*LoggingDocument, error) {
	if plan == nil {
		return nil, renderFailure("$", fmt.Errorf("plan is nil"))
	}
	if plan.GetSchemaVersion() != observabilityv1.GenerationPlanSchemaVersion {
		return nil, renderFailure("$", fmt.Errorf(
			"plan schema version %q is not supported; supported: %s",
			plan.GetSchemaVersion(), observabilityv1.GenerationPlanSchemaVersion))
	}

	// The built-in credential denylist is unclosable: the policy already
	// guarantees a superset, verified here so a misconfigured policy can
	// never reach a file.
	for name := range policy.BuiltinCredentialDenylist {
		if !slices.Contains(resolved.Logging.RedactFields, name) {
			return nil, renderFailure("$", fmt.Errorf(
				"policy redaction is missing the unclosable built-in entry %q", name))
		}
	}

	document := &LoggingDocument{
		SchemaVersion: SchemaVersionLogging,
		DocumentType:  DocumentTypeLogging,
		Source: Source{
			IRSchemaVersion: plan.GetSourceIrSchemaVersion(),
			ServiceName:     plan.GetServiceName(),
		},
		GeneratedBy: GeneratedBy{Name: GeneratorName, Version: GeneratorVersion},
		Redaction: Redaction{
			Immutable:  true,
			FieldNames: append([]string(nil), resolved.Logging.RedactFields...),
		},
	}

	// Result events (completed/failed) of one target must be mutually
	// exclusive: no runtime status may fire two events.
	statusSets := make(map[string]map[string]bool)
	eventByTarget := make(map[string]string)
	for _, logPlan := range plan.GetLogs() {
		converted, err := convertLogEvent(logPlan)
		if err != nil {
			return nil, renderFailure(logPlan.GetId(), err)
		}
		if len(logPlan.GetStatuses()) > 0 {
			target := logPlan.GetTarget().GetId()
			set := make(map[string]bool, len(logPlan.GetStatuses()))
			for _, status := range logPlan.GetStatuses() {
				set[status.String()] = true
			}
			for existing := range statusSets[target] {
				if set[existing] {
					return nil, renderFailure(logPlan.GetId(), fmt.Errorf(
						"event condition overlaps %q for target %s", eventByTarget[target], target))
				}
			}
			statusSets[target] = set
			eventByTarget[target] = logPlan.GetEventName()
		}
		document.Events = append(document.Events, converted)
	}
	return document, nil
}

// convertLogEvent maps one LogPlan onto the YAML contract. Conditions are
// rendered in the finite status order; an unconditional event (empty
// statuses, e.g. a start event) fires for every status.
func convertLogEvent(logPlan *observabilityv1.LogPlan) (LogEvent, error) {
	if logPlan.GetId() == "" {
		return LogEvent{}, fmt.Errorf("log event id is empty")
	}
	if logPlan.GetEventName() == "" {
		return LogEvent{}, fmt.Errorf("log event name is empty")
	}
	target, err := convertTarget(logPlan.GetTarget())
	if err != nil {
		return LogEvent{}, err
	}
	trigger, err := triggerPhaseName(logPlan.GetTrigger().GetPhase())
	if err != nil {
		return LogEvent{}, err
	}
	severity, err := logSeverityName(logPlan.GetSeverity())
	if err != nil {
		return LogEvent{}, err
	}

	event := LogEvent{
		ID:        logPlan.GetId(),
		EventName: logPlan.GetEventName(),
		Target:    target,
		Trigger:   trigger,
		Condition: Condition{StatusIn: statusNames(logPlan.GetStatuses())},
		Severity:  Severity{Constant: severity},
	}
	normalizedKeys := make(map[string]bool, len(logPlan.GetFields()))
	for _, field := range logPlan.GetFields() {
		if field.GetKey() == "" {
			return LogEvent{}, fmt.Errorf("field key is empty")
		}
		normalized := naming.NormalizeFieldKey(field.GetKey())
		if normalizedKeys[normalized] {
			return LogEvent{}, fmt.Errorf("duplicate field key %q after normalization", field.GetKey())
		}
		normalizedKeys[normalized] = true
		binding, err := convertValueBinding(field.GetValue())
		if err != nil {
			return LogEvent{}, fmt.Errorf("field %q: %w", field.GetKey(), err)
		}
		converted := Field{
			Key:     field.GetKey(),
			Type:    bindingTypeName(field.GetValue().GetType()),
			Binding: binding,
		}
		if field.GetValue().GetRequired() {
			converted.Required = new(true)
		}
		event.Fields = append(event.Fields, converted)
	}
	return event, nil
}

// statusNames renders statuses in the finite contract order; empty input
// (unconditional events) expands to all five statuses.
func statusNames(statuses []observabilityv1.RuntimeStatus) []string {
	if len(statuses) == 0 {
		return []string{RuntimeStatusOK, RuntimeStatusError, RuntimeStatusCancelled, RuntimeStatusTimeout, RuntimeStatusUnknown}
	}
	order := map[observabilityv1.RuntimeStatus]int{
		observabilityv1.RuntimeStatus_RUNTIME_STATUS_OK:        0,
		observabilityv1.RuntimeStatus_RUNTIME_STATUS_ERROR:     1,
		observabilityv1.RuntimeStatus_RUNTIME_STATUS_CANCELLED: 2,
		observabilityv1.RuntimeStatus_RUNTIME_STATUS_TIMEOUT:   3,
		observabilityv1.RuntimeStatus_RUNTIME_STATUS_UNKNOWN:   4,
	}
	type entry struct {
		order int
		name  string
	}
	entries := make([]entry, 0, len(statuses))
	for _, status := range statuses {
		name, _ := runtimeStatusName(status)
		entries = append(entries, entry{order: order[status], name: name})
	}
	slices.SortStableFunc(entries, func(left, right entry) int {
		return left.order - right.order
	})
	names := make([]string, 0, len(entries))
	for _, item := range entries {
		names = append(names, item.name)
	}
	return names
}

func logSeverityName(severity observabilityv1.LogSeverity) (string, error) {
	switch severity {
	case observabilityv1.LogSeverity_LOG_SEVERITY_INFO:
		return LogSeverityInfo, nil
	case observabilityv1.LogSeverity_LOG_SEVERITY_WARN:
		return LogSeverityWarn, nil
	case observabilityv1.LogSeverity_LOG_SEVERITY_ERROR:
		return LogSeverityError, nil
	default:
		return "", fmt.Errorf("unspecified log severity")
	}
}

// scanSensitiveConstants verifies that no rendered constant carries a
// sensitive value, as a final canary check on the produced document.
func scanSensitiveConstants(document *LoggingDocument) error {
	for _, event := range document.Events {
		for _, field := range event.Fields {
			if field.Binding.Source != ValueSourceConstant {
				continue
			}
			value := field.Binding.String
			if field.Binding.Number != 0 {
				continue
			}
			if naming.IsSensitiveValue(value) || naming.IsHighCardinalityValue(value) {
				return fmt.Errorf("event %s field %q carries a blocked constant", event.ID, field.Key)
			}
		}
	}
	return nil
}
