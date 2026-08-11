package dashboard

import (
	"errors"
	"strings"
	"testing"
)

// TestDashboardUIDFormat exercises the UID contract: fixed si-<part>-v1
// shell, 8..40 length, Grafana charset, determinism and the SHA-256
// fallback for names with no usable characters.
func TestDashboardUIDFormat(t *testing.T) {
	cases := []struct {
		name     string
		fallback bool
	}{
		{name: "payment"},
		{name: "Payment Service"},
		{name: "pkg/http/handler"},
		{name: "A_B-C.D"},
		{name: "  spaced   out  "},
		{name: "a"}, // hash-padded, no exact prefix
		{name: "", fallback: true},
		{name: "支付服务", fallback: true},
		{name: "only-symbols-!!!", fallback: false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			uid := DashboardUID(test.name)
			if len(uid) < MinUIDLength || len(uid) > MaxUIDLength {
				t.Errorf("UID %q length %d outside [%d, %d]", uid, len(uid), MinUIDLength, MaxUIDLength)
			}
			if !strings.HasPrefix(uid, "si-") || !strings.HasSuffix(uid, "-v1") {
				t.Errorf("UID %q must match si-<part>-v1", uid)
			}
			for _, character := range uid {
				if !(character >= 'a' && character <= 'z' ||
					character >= '0' && character <= '9' ||
					character == '-') {
					t.Errorf("UID %q has illegal character %q", uid, character)
				}
			}
			if uid != DashboardUID(test.name) {
				t.Errorf("UID %q is not deterministic", uid)
			}
			if test.fallback {
				part := uid[len("si-") : len(uid)-len("-v1")]
				if len(part) != 16 || !isHex(part) {
					t.Errorf("fallback UID part %q must be a 16-hex SHA-256 prefix", part)
				}
			}
		})
	}
}

// TestDashboardUIDNormalizationCollisions documents that distinct raw
// names may normalize to the same UID; the set-level disambiguation
// (DisambiguateUIDs) is what makes them unique.
func TestDashboardUIDNormalizationCollisions(t *testing.T) {
	expected := DashboardUID("a-b")
	for _, name := range []string{"a.b", "a/b", "a b", "A_B", "a--b", "a..b"} {
		if got := DashboardUID(name); got != expected {
			t.Errorf("DashboardUID(%q) = %q, want %q", name, got, expected)
		}
	}
}

// TestDashboardUIDLongName truncates the service part so the UID stays
// within MaxUIDLength.
func TestDashboardUIDLongName(t *testing.T) {
	long := strings.Repeat("service", 30) // 210 chars
	uid := DashboardUID(long)
	if len(uid) > MaxUIDLength {
		t.Errorf("long service UID %q exceeds %d", uid, MaxUIDLength)
	}
	if !strings.HasPrefix(uid, "si-") || !strings.HasSuffix(uid, "-v1") {
		t.Errorf("long service UID %q breaks the fixed shell", uid)
	}
}

// TestResolvePanelIDsOrderIndependent verifies AC1 for panel IDs: any
// input permutation yields the same key->ID mapping.
func TestResolvePanelIDsOrderIndependent(t *testing.T) {
	keys := []string{
		PanelIDKey(CategoryHTTP, "item:http:get_user", "rate"),
		PanelIDKey(CategoryHTTP, "item:http:create_order", "rate"),
		PanelIDKey(CategoryDatabase, "item:database:users", "p95"),
		PanelIDKey(CategoryCache, "item:cache:session", "inflight"),
		PanelIDKey(CategoryServiceOverview, "item:service_overview:summary", "errors"),
	}
	canonical := ResolvePanelIDs(keys)
	if len(canonical) != len(keys) {
		t.Fatalf("resolved %d IDs for %d keys", len(canonical), len(keys))
	}
	byKey := make(map[string]int64, len(keys))
	for index, key := range keys {
		byKey[key] = canonical[index]
	}

	// All 5! = 120 permutations.
	permuted := append([]string(nil), keys...)
	permuteStrings(permuted, 0, func(permutation []string) {
		got := ResolvePanelIDs(permutation)
		for index, key := range permutation {
			if got[index] != byKey[key] {
				t.Fatalf("key %q maps to %d in one order and %d in another", key, byKey[key], got[index])
			}
		}
	})
	for _, id := range canonical {
		if id < 0 {
			t.Errorf("panel ID %d is negative", id)
		}
	}
}

func permuteStrings(items []string, index int, visit func([]string)) {
	if index == len(items) {
		visit(items)
		return
	}
	for position := index; position < len(items); position++ {
		items[index], items[position] = items[position], items[index]
		permuteStrings(items, index+1, visit)
		items[index], items[position] = items[position], items[index]
	}
}

// TestResolvePanelIDsCollisionRehash forces hash collisions through the
// injectable hash and verifies the attempt counter produces unique
// positive IDs that do not depend on input order.
func TestResolvePanelIDsCollisionRehash(t *testing.T) {
	collidingHash := func(key string, attempt int) int64 {
		// Every key has the same length, so every attempt-0 hash is equal;
		// the attempt counter alone separates the group.
		return int64(len(key) + attempt)
	}
	keys := []string{"aa", "ab", "ac"}
	first := resolveIDs(keys, collidingHash)
	second := resolveIDs([]string{"ac", "aa", "ab"}, collidingHash)

	byKey := make(map[string]int64, len(keys))
	for index, key := range keys {
		byKey[key] = first[index]
	}
	for index, key := range []string{"ac", "aa", "ab"} {
		if second[index] != byKey[key] {
			t.Errorf("collision resolution depends on input order for key %q", key)
		}
	}
	seen := make(map[int64]bool)
	for key, id := range byKey {
		if id < 0 {
			t.Errorf("key %q got negative ID %d", key, id)
		}
		if seen[id] {
			t.Errorf("collision resolution produced duplicate ID %d", id)
		}
		seen[id] = true
	}
}

// TestResolvePanelIDsEmpty returns nil for no keys.
func TestResolvePanelIDsEmpty(t *testing.T) {
	if ids := ResolvePanelIDs(nil); ids != nil {
		t.Errorf("expected nil for empty key set, got %v", ids)
	}
}

// TestAllocateRefIDs assigns A..Z in fully sorted key order, aligned with
// the input order, independent of permutation (AC1/AC4).
func TestAllocateRefIDs(t *testing.T) {
	keys := []string{"cache:hit", "http:rate", "database:p95"}
	refIDs, err := AllocateRefIDs(keys)
	if err != nil {
		t.Fatalf("AllocateRefIDs failed: %v", err)
	}
	// Sorted: cache:hit, database:p95, http:rate.
	expected := []string{"A", "C", "B"}
	for index, want := range expected {
		if refIDs[index] != want {
			t.Errorf("refID[%d] = %q, want %q", index, refIDs[index], want)
		}
	}
	permuted, err := AllocateRefIDs([]string{"database:p95", "http:rate", "cache:hit"})
	if err != nil {
		t.Fatalf("permuted AllocateRefIDs failed: %v", err)
	}
	for index := range permuted {
		if permuted[index] != refIDs[2-index] {
			t.Errorf("refId assignment depends on input order")
		}
	}
}

// TestAllocateRefIDsFullAlphabet covers all 26 refIds.
func TestAllocateRefIDsFullAlphabet(t *testing.T) {
	keys := make([]string, 26)
	for index := range keys {
		keys[index] = string(rune('a' + index))
	}
	refIDs, err := AllocateRefIDs(keys)
	if err != nil {
		t.Fatalf("26 keys must fit A..Z: %v", err)
	}
	for index, refID := range refIDs {
		if refID != string(rune('A'+index)) {
			t.Errorf("refID[%d] = %q, want %q", index, refID, string(rune('A'+index)))
		}
	}
}

// TestAllocateRefIDsLimitExceeded is AC4: 27 queries fail with a stable
// error instead of producing "AA" or an undefined refId.
func TestAllocateRefIDsLimitExceeded(t *testing.T) {
	keys := make([]string, 27)
	for index := range keys {
		keys[index] = "query-" + string(rune('a'+index))
	}
	_, err := AllocateRefIDs(keys)
	if err == nil {
		t.Fatal("27 keys must fail")
	}
	var failure *CatalogError
	if !errors.As(err, &failure) {
		t.Fatalf("error is %T, want *CatalogError", err)
	}
	if failure.Code != CodePanelLimitExceeded {
		t.Errorf("code = %q, want %s", failure.Code, CodePanelLimitExceeded)
	}
	if !strings.Contains(err.Error(), "A-Z") {
		t.Errorf("error must state the A-Z bound: %v", err)
	}
	if strings.Contains(err.Error(), "AA") {
		t.Errorf("error must not suggest extended refIds: %v", err)
	}
}

// TestHashIDRange keeps every hash inside the positive int32 range.
func TestHashIDRange(t *testing.T) {
	for _, key := range []string{"", "a", "panel:http:item:rate", strings.Repeat("k", 1000)} {
		for attempt := 0; attempt < 8; attempt++ {
			if id := hashID(key, attempt); id < 0 || id > 0x7fffffff {
				t.Errorf("hashID(%q, %d) = %d outside positive int32 range", key, attempt, id)
			}
		}
	}
}

func isHex(value string) bool {
	for _, character := range value {
		if !(character >= '0' && character <= '9' || character >= 'a' && character <= 'f') {
			return false
		}
	}
	return true
}
