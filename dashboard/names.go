package dashboard

import (
	"sort"
	"strings"
	"unicode"
)

// Safe naming contract (P2-04, task 3). Titles derive only from controlled
// categories, operations and Phase 1 normalized names; control characters,
// path structure and raw target values never reach the visualization layer.
// Everything here is a pure function of its inputs.
const (
	// MaxTitleLength bounds every generated title (dashboard, category and
	// panel). Grafana allows more; the contract is deliberately stricter.
	MaxTitleLength = 64
	// titleDisambiguationSuffixLength is the hex suffix width used when a
	// title or UID collides.
	titleDisambiguationSuffixLength = 8
)

// CategoryOrder is the fixed top-to-bottom layout order. Layout code must
// iterate this order, never map iteration order or input order.
var CategoryOrder = []Category{
	CategoryServiceOverview,
	CategoryHTTP,
	CategoryRPC,
	CategoryKafka,
	CategoryDatabase,
	CategoryCache,
}

// ServiceTitle derives the dashboard title from the service name. The
// basename is the last path component, so "pkg/http/handler" and
// "handler" name the same dashboard; control characters are removed and
// the result is bounded by MaxTitleLength. A name with no usable
// characters falls back to a fixed SHA-256 prefix.
func ServiceTitle(serviceName string) string {
	title := sanitizeTitle(basename(serviceName))
	if !hasAlphanumeric(title) {
		title = sha256Hex(serviceName)[:16]
	}
	return clipTitle(title, "")
}

// hasAlphanumeric reports whether the value carries any letter or digit.
func hasAlphanumeric(value string) bool {
	for _, character := range value {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			return true
		}
	}
	return false
}

// CategoryTitle maps a category onto its controlled display title. Only
// the five v1 categories have titles; unknown categories are sanitized
// defensively but never pass raw bytes.
func CategoryTitle(category Category) string {
	switch category {
	case CategoryServiceOverview:
		return "Service Overview"
	case CategoryHTTP:
		return "HTTP"
	case CategoryRPC:
		return "RPC"
	case CategoryKafka:
		return "Kafka"
	case CategoryDatabase:
		return "Database"
	case CategoryCache:
		return "Cache"
	default:
		return sanitizeTitle(string(category))
	}
}

// PanelTitle composes a panel title from controlled parts: the category
// title, the controlled operation and the Phase 1 normalized name. Parts
// are sanitized individually, joined by single spaces and bounded by
// MaxTitleLength. The composition is fixed, so later stories must not
// introduce free-form templates.
func PanelTitle(category Category, operation, name string) string {
	parts := []string{CategoryTitle(category), sanitizeTitle(operation), sanitizeTitle(name)}
	var builder strings.Builder
	for _, part := range parts {
		if part == "" {
			continue
		}
		if builder.Len() > 0 {
			builder.WriteByte(' ')
		}
		builder.WriteString(part)
	}
	return clipTitle(builder.String(), "")
}

// UIDResult is one disambiguated dashboard UID.
type UIDResult struct {
	// ServiceName is the input service name.
	ServiceName string
	// UID is the final UID, unique across distinct colliding names.
	UID string
	// Suffixed reports whether a collision suffix was applied.
	Suffixed bool
}

// DisambiguateUIDs resolves dashboard UID collisions across a set of
// service names (AC2). Distinct names whose normalized UIDs are equal are
// sorted by name: the smallest keeps the base UID, every other gains a
// deterministic "-<sha256(name)[:8]>" suffix inside the 8..40 length
// bounds, and each suffixed entry yields a DASHBOARD_NAME_COLLISION
// diagnostic. Identical names name the same service and keep the same
// UID; they never produce a diagnostic. The result depends only on the
// member set, never on input order.
func DisambiguateUIDs(serviceNames []string) ([]UIDResult, []Diagnostic) {
	results := make([]UIDResult, 0, len(serviceNames))
	if len(serviceNames) == 0 {
		return results, nil
	}
	unique := append([]string(nil), serviceNames...)
	sort.Strings(unique)
	unique = dedupeSorted(unique)

	uidByName := make(map[string]string, len(unique))
	var diagnostics []Diagnostic
	for index := 0; index < len(unique); {
		groupEnd := index + 1
		for groupEnd < len(unique) && DashboardUID(unique[groupEnd]) == DashboardUID(unique[index]) {
			groupEnd++
		}
		group := unique[index:groupEnd]
		uidByName[group[0]] = DashboardUID(group[0])
		for _, name := range group[1:] {
			suffix := "-" + sha256Hex(name)[:titleDisambiguationSuffixLength]
			part := uidServicePart(name)
			if len(part) > uidServicePartMax-len(suffix) {
				part = part[:uidServicePartMax-len(suffix)]
			}
			uidByName[name] = "si-" + part + suffix + "-" + UIDSchemaVersion
			diagnostics = append(diagnostics, Diagnostic{
				Code:     CodeNameCollision,
				TargetID: name,
				Field:    "uid",
				Message:  "normalized service name collides with another service and was disambiguated with a stable suffix",
			})
		}
		index = groupEnd
	}

	for _, name := range serviceNames {
		results = append(results, UIDResult{
			ServiceName: name,
			UID:         uidByName[name],
			Suffixed:    uidByName[name] != DashboardUID(name),
		})
	}
	return results, diagnostics
}

func dedupeSorted(sorted []string) []string {
	if len(sorted) == 0 {
		return sorted
	}
	write := 1
	for read := 1; read < len(sorted); read++ {
		if sorted[read] != sorted[write-1] {
			sorted[write] = sorted[read]
			write++
		}
	}
	return sorted[:write]
}

// TitleItem is one candidate title entering the collision table.
type TitleItem struct {
	// TargetID is the stable entity ID the suffix derives from.
	TargetID string
	// Title is the normalized candidate title.
	Title string
}

// TitleResult is one disambiguated title.
type TitleResult struct {
	// TargetID identifies the entity.
	TargetID string
	// Title is the final title, possibly suffixed.
	Title string
	// Suffixed reports whether a collision suffix was applied.
	Suffixed bool
}

// DisambiguateTitles resolves equal normalized titles deterministically
// (AC2). Within a group of equal titles the entries are sorted by
// TargetID: the smallest keeps the base title, every other gains a
// deterministic " - <sha256(targetID)[:8]>" suffix bounded by
// MaxTitleLength, and each suffixed entry yields a
// DASHBOARD_NAME_COLLISION diagnostic.
func DisambiguateTitles(items []TitleItem) ([]TitleResult, []Diagnostic) {
	results := make([]TitleResult, 0, len(items))
	if len(items) == 0 {
		return results, nil
	}
	ordered := append([]TitleItem(nil), items...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Title != ordered[j].Title {
			return ordered[i].Title < ordered[j].Title
		}
		return ordered[i].TargetID < ordered[j].TargetID
	})

	var diagnostics []Diagnostic
	for index := 0; index < len(ordered); {
		groupEnd := index + 1
		for groupEnd < len(ordered) && ordered[groupEnd].Title == ordered[index].Title {
			groupEnd++
		}
		group := ordered[index:groupEnd]
		results = append(results, TitleResult{
			TargetID: group[0].TargetID, Title: group[0].Title, Suffixed: false,
		})
		for _, item := range group[1:] {
			suffix := " - " + sha256Hex(item.TargetID)[:titleDisambiguationSuffixLength]
			results = append(results, TitleResult{
				TargetID: item.TargetID, Title: clipTitle(item.Title, suffix), Suffixed: true,
			})
			diagnostics = append(diagnostics, Diagnostic{
				Code:     CodeNameCollision,
				TargetID: item.TargetID,
				Field:    "title",
				Message:  "normalized title collides with another entity and was disambiguated with a stable suffix",
			})
		}
		index = groupEnd
	}
	return results, diagnostics
}

// ComposeDashboardTitle derives the dashboard title from the service
// name and the validated title suffix (P2-10). Both parts are sanitized
// and the result is bounded by MaxTitleLength, so no control character,
// path structure or raw IR value reaches the title.
func ComposeDashboardTitle(serviceName, suffix string) string {
	title := ServiceTitle(serviceName)
	suffix = sanitizeTitle(suffix)
	if suffix != "" {
		title = title + " " + suffix
	}
	return clipTitle(title, "")
}

// basename returns the last "/" or "\" separated component, mirroring how
// service names may embed module paths.
func basename(value string) string {
	for separator := len(value) - 1; separator >= 0; separator-- {
		if value[separator] == '/' || value[separator] == '\\' {
			return value[separator+1:]
		}
	}
	return value
}

// sanitizeTitle removes control and format characters (ESC, BEL,
// zero-width joiners) and collapses whitespace runs into single spaces;
// leading and trailing whitespace vanish. Format characters are treated
// as separators so invisible text can never hide inside a title.
func sanitizeTitle(value string) string {
	var builder strings.Builder
	pendingSpace := false
	for _, character := range value {
		if unicode.IsControl(character) || unicode.IsSpace(character) || unicode.Is(unicode.Cf, character) {
			pendingSpace = true
			continue
		}
		if pendingSpace && builder.Len() > 0 {
			builder.WriteByte(' ')
		}
		pendingSpace = false
		builder.WriteRune(character)
	}
	return builder.String()
}

// clipTitle fits title into MaxTitleLength, reserving room for the
// collision suffix when present: the base part shrinks so the complete
// title never exceeds the bound.
func clipTitle(title, suffix string) string {
	limit := MaxTitleLength - len(suffix)
	if limit < 1 {
		return title[:MaxTitleLength]
	}
	if len(title) > limit {
		title = title[:limit]
	}
	return title + suffix
}
