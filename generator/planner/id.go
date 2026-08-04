package planner

// StableID derives a plan item ID from the signal, the stable IR target
// ID and a planner-chosen purpose. IDs never change when names change,
// which keeps plans stable across IR renaming. The purpose must not
// contain ':'; planner-chosen purposes are controlled constants.
func StableID(signal, targetID, purpose string) string {
	return signal + ":" + targetID + ":" + purpose
}

// Purpose constants for plan item IDs, shared by the signal sub-planners
// so IDs stay consistent across signals that target the same entity.
const (
	// PurposeCount is the counter plan purpose, e.g. "...:count".
	PurposeCount = "count"
	// PurposeDuration is the histogram plan purpose.
	PurposeDuration = "duration"
	// PurposeDurationSummary is the optional summary plan purpose.
	PurposeDurationSummary = "duration_summary"
	// PurposeInFlight is the gauge plan purpose.
	PurposeInFlight = "in_flight"
	// PurposeRoot marks the endpoint root span.
	PurposeRoot = "root"
	// PurposeChild marks a dependency or internal child span.
	PurposeChild = "child"
	// PurposeStart/PurposeEnd/PurposeFailed mark log events.
	PurposeStart  = "started"
	PurposeEnd    = "completed"
	PurposeFailed = "failed"
)
