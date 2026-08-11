package dashboard

import (
	"strings"
	"testing"
)

// TestServiceTitle derives safe dashboard titles: basename extraction,
// control-character removal and the length bound.
func TestServiceTitle(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{name: "payment", want: "payment"},
		{name: "pkg/http/handler", want: "handler"},
		{name: `pkg\http\handler`, want: "handler"},
		{name: "Payment   Service", want: "Payment Service"},
		{name: "line1\nline2\ttab", want: "line1 line2 tab"},
		{name: "\x1b\x07red", want: "red"},
		{name: "\x00nul", want: "nul"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := ServiceTitle(test.name); got != test.want {
				t.Errorf("ServiceTitle(%q) = %q, want %q", test.name, got, test.want)
			}
		})
	}
}

// TestServiceTitleBounds caps long names and falls back for unusable ones.
func TestServiceTitleBounds(t *testing.T) {
	if got := ServiceTitle(strings.Repeat("s", 100)); len(got) != MaxTitleLength {
		t.Errorf("long title length = %d, want %d", len(got), MaxTitleLength)
	}
	fallback := ServiceTitle("!!!")
	if len(fallback) != 16 || !isHex(fallback) {
		t.Errorf("fallback title %q must be a 16-hex SHA-256 prefix", fallback)
	}
	if ServiceTitle("payment") != ServiceTitle("payment") {
		t.Error("ServiceTitle is not deterministic")
	}
}

// TestCategoryTitle pins the controlled category titles and defensively
// sanitizes unknown categories.
func TestCategoryTitle(t *testing.T) {
	expected := map[Category]string{
		CategoryServiceOverview: "Service Overview",
		CategoryHTTP:            "HTTP",
		CategoryRPC:             "RPC",
		CategoryKafka:           "Kafka",
		CategoryDatabase:        "Database",
		CategoryCache:           "Cache",
	}
	for category, want := range expected {
		if got := CategoryTitle(category); got != want {
			t.Errorf("CategoryTitle(%s) = %q, want %q", category, got, want)
		}
	}
	if got := CategoryTitle(Category("weird\x07")); got != "weird" {
		t.Errorf("unknown category title must be sanitized, got %q", got)
	}
}

// TestPanelTitle composes titles from controlled parts only, removing
// control characters and collapsing whitespace.
func TestPanelTitle(t *testing.T) {
	cases := []struct {
		category  Category
		operation string
		name      string
		want      string
	}{
		{CategoryHTTP, "rate", "get_user", "HTTP rate get_user"},
		{CategoryDatabase, "p95", "users", "Database p95 users"},
		{CategoryServiceOverview, "errors", "summary", "Service Overview errors summary"},
		{CategoryHTTP, "\x1b\x07rate", "get_user", "HTTP rate get_user"},
		{CategoryHTTP, "  ", "get_user", "HTTP get_user"},
		{CategoryHTTP, "", "", "HTTP"},
	}
	for _, test := range cases {
		if got := PanelTitle(test.category, test.operation, test.name); got != test.want {
			t.Errorf("PanelTitle(%s, %q, %q) = %q, want %q",
				test.category, test.operation, test.name, got, test.want)
		}
	}
	if got := PanelTitle(CategoryHTTP, "rate", strings.Repeat("n", 100)); len(got) != MaxTitleLength {
		t.Errorf("panel title length = %d, want %d", len(got), MaxTitleLength)
	}
}

// TestUnicodeTitles documents that non-space Unicode passes through titles
// (titles are not ASCII-restricted; UIDs are) while control characters
// never do.
func TestUnicodeTitles(t *testing.T) {
	if got := ServiceTitle("支付服务"); got != "支付服务" {
		t.Errorf("Unicode title mangled: %q", got)
	}
	if got := PanelTitle(CategoryHTTP, "rate", "get_user​"); strings.Contains(got, "​") {
		t.Errorf("zero-width space survives sanitization: %q", got)
	}
}

// TestDisambiguateUIDs is AC2 for services: equal normalized names yield
// unique, length-compliant UIDs plus DASHBOARD_NAME_COLLISION diagnostics,
// independent of input order.
func TestDisambiguateUIDs(t *testing.T) {
	names := []string{"payment", "payment", "a.b", "a_b", "checkout"}
	results, diagnostics := DisambiguateUIDs(names)
	if len(results) != len(names) {
		t.Fatalf("got %d results for %d names", len(results), len(names))
	}
	uidByName := make(map[string]string, len(results))
	for _, result := range results {
		if len(result.UID) < MinUIDLength || len(result.UID) > MaxUIDLength {
			t.Errorf("UID %q length %d outside [%d, %d]", result.UID, len(result.UID), MinUIDLength, MaxUIDLength)
		}
		if previous, present := uidByName[result.ServiceName]; present {
			if previous != result.UID {
				t.Errorf("identical name %q got different UIDs", result.ServiceName)
			}
			continue
		}
		uidByName[result.ServiceName] = result.UID
	}
	// Distinct names never share a UID.
	seen := make(map[string]string)
	for name, uid := range uidByName {
		if other, present := seen[uid]; present {
			t.Errorf("distinct names %q and %q share UID %q", other, name, uid)
		}
		seen[uid] = name
	}
	// Only the "a_b" sibling of the colliding pair carries the suffix;
	// identical duplicates never collide.
	if len(diagnostics) != 1 {
		t.Fatalf("expected 1 collision diagnostic (a.b vs a_b), got %d", len(diagnostics))
	}
	for _, diagnostic := range diagnostics {
		if diagnostic.Code != CodeNameCollision {
			t.Errorf("code = %q, want %s", diagnostic.Code, CodeNameCollision)
		}
		if diagnostic.Field != "uid" {
			t.Errorf("field = %q, want uid", diagnostic.Field)
		}
	}
	if diagnostics[0].TargetID != "a_b" {
		t.Errorf("diagnostic TargetID = %q, want the suffixed name a_b", diagnostics[0].TargetID)
	}
	for name, wantSuffixed := range map[string]bool{"a_b": true, "a.b": false, "payment": false, "checkout": false} {
		for _, result := range results {
			if result.ServiceName != name {
				continue
			}
			if result.Suffixed != wantSuffixed {
				t.Errorf("name %q Suffixed = %v, want %v", name, result.Suffixed, wantSuffixed)
			}
		}
	}

	// Permutation independence: reversed input yields the same mapping.
	reversed, _ := DisambiguateUIDs([]string{"checkout", "a_b", "a.b", "payment", "payment"})
	for index, name := range names {
		wantUID := results[index].UID
		for _, result := range reversed {
			if result.ServiceName == name {
				if result.UID != wantUID {
					t.Errorf("UID for %q depends on input order: %q vs %q", name, result.UID, wantUID)
				}
				break
			}
		}
	}
}

// TestDisambiguateUIDsLongName keeps the suffixed UID within bounds and
// identical names (one service) collapse onto one UID.
func TestDisambiguateUIDsLongName(t *testing.T) {
	long := strings.Repeat("service", 20)
	results, _ := DisambiguateUIDs([]string{long, long})
	if len(results) != 2 || results[0].UID != results[1].UID {
		t.Fatalf("identical names must share one UID: %+v", results)
	}
	// A distinct name that normalizes to the same part must be
	// disambiguated inside the length bounds.
	colliding := long + "!"
	results, diagnostics := DisambiguateUIDs([]string{long, colliding})
	if len(results) != 2 || results[0].UID == results[1].UID {
		t.Fatal("distinct colliding names must produce two unique UIDs")
	}
	if len(diagnostics) != 1 {
		t.Fatalf("expected 1 collision diagnostic, got %d", len(diagnostics))
	}
	for _, result := range results {
		if len(result.UID) > MaxUIDLength {
			t.Errorf("UID %q exceeds %d", result.UID, MaxUIDLength)
		}
		if len(result.UID) < MinUIDLength {
			t.Errorf("UID %q below %d", result.UID, MinUIDLength)
		}
	}
}

// TestDisambiguateTitles is AC2 for titles: equal normalized titles gain a
// stable bounded suffix plus DASHBOARD_NAME_COLLISION diagnostics.
func TestDisambiguateTitles(t *testing.T) {
	items := []TitleItem{
		{TargetID: "item:http:get_user", Title: "HTTP rate get_user"},
		{TargetID: "item:http:getuser", Title: "HTTP rate get_user"},
		{TargetID: "item:http:create_order", Title: "HTTP rate create_order"},
	}
	results, diagnostics := DisambiguateTitles(items)
	if len(results) != len(items) {
		t.Fatalf("got %d results for %d items", len(results), len(items))
	}
	seen := make(map[string]bool)
	for _, result := range results {
		if len(result.Title) > MaxTitleLength {
			t.Errorf("title %q exceeds %d", result.Title, MaxTitleLength)
		}
		if seen[result.Title] {
			t.Errorf("duplicate title %q after disambiguation", result.Title)
		}
		seen[result.Title] = true
	}
	if len(diagnostics) != 1 {
		t.Fatalf("expected 1 collision diagnostic, got %d", len(diagnostics))
	}
	if diagnostics[0].Code != CodeNameCollision || diagnostics[0].Field != "title" {
		t.Errorf("diagnostic = %+v, want CodeNameCollision on title", diagnostics[0])
	}
	if diagnostics[0].TargetID != "item:http:getuser" {
		t.Errorf("diagnostic TargetID = %q, want the suffixed item", diagnostics[0].TargetID)
	}
	titleByTarget := make(map[string]TitleResult)
	for _, result := range results {
		titleByTarget[result.TargetID] = result
	}
	if !titleByTarget["item:http:getuser"].Suffixed {
		t.Errorf("getuser must be suffixed: %+v", titleByTarget["item:http:getuser"])
	}
	if titleByTarget["item:http:get_user"].Suffixed {
		t.Errorf("get_user (first in sorted order) must keep the base title: %+v", titleByTarget["item:http:get_user"])
	}
	if !strings.Contains(titleByTarget["item:http:getuser"].Title, " - ") {
		t.Errorf("suffixed title %q must carry a ' - ' separator", titleByTarget["item:http:getuser"].Title)
	}

	// Permutation independence.
	reversed, _ := DisambiguateTitles([]TitleItem{items[2], items[1], items[0]})
	for _, result := range reversed {
		if result.Title != titleByTarget[result.TargetID].Title {
			t.Errorf("title for %q depends on input order", result.TargetID)
		}
	}
}

// TestDisambiguateTitlesLongBase reserves room for the suffix so the
// suffixed title never exceeds MaxTitleLength.
func TestDisambiguateTitlesLongBase(t *testing.T) {
	base := strings.Repeat("t", MaxTitleLength)
	items := []TitleItem{
		{TargetID: "item:a", Title: base},
		{TargetID: "item:b", Title: base},
	}
	results, _ := DisambiguateTitles(items)
	for _, result := range results {
		if len(result.Title) > MaxTitleLength {
			t.Errorf("title length %d exceeds %d: %q", len(result.Title), MaxTitleLength, result.Title)
		}
	}
	if results[0].Title == results[1].Title {
		t.Error("long colliding titles were not disambiguated")
	}
}

// TestNameFunctionsSanitize verifies raw path structure, control bytes
// and symbol-only names never survive into generated names: paths are
// basenamed, control bytes are removed and unusable names fall back to
// hash material instead of raw bytes.
func TestNameFunctionsSanitize(t *testing.T) {
	if got := ServiceTitle("https://host/svc/../handler"); got != "handler" {
		t.Errorf("path structure leaked: %q", got)
	}
	if got := PanelTitle(CategoryHTTP, "rate", "a\x00b\x1b[31m"); strings.ContainsAny(got, "\x00\x1b") {
		t.Errorf("control bytes leaked into panel title: %q", got)
	}
	results, _ := DisambiguateUIDs([]string{"!!!", "!!!"})
	for _, result := range results {
		if strings.ContainsAny(result.UID, "!") {
			t.Errorf("raw symbols leaked into UID %q", result.UID)
		}
	}
}
