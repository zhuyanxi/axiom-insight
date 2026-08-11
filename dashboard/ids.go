package dashboard

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// Deterministic ID contract (P2-04). Every ID and refId is derived from
// controlled, canonical inputs with a fixed hash version; nothing depends
// on input order, the clock, the filesystem, random numbers or the
// environment. The hash is not a security boundary: every input is already
// provenance- and allowlist-validated before it reaches these functions.
const (
	// UIDSchemaVersion is the dashboard schema marker embedded in every
	// dashboard UID.
	UIDSchemaVersion = "v1"
	// HashVersion identifies the fixed ID hashing algorithm. Bumping the
	// hash inputs, the collision strategy or the UID format requires
	// bumping this constant; it is exported so tests can pin the contract.
	HashVersion = "si-hash-v1"
	// MinUIDLength and MaxUIDLength bound the dashboard UID.
	MinUIDLength = 8
	MaxUIDLength = 40
	// uidServicePartMin is the shortest normalized service part that still
	// yields a UID of MinUIDLength with the fixed "si-" and "-v1" shells.
	uidServicePartMin = MinUIDLength - len("si-") - len("-v1")
	// uidServicePartMax is the longest service part that keeps the UID at
	// or under MaxUIDLength.
	uidServicePartMax = MaxUIDLength - len("si-") - len("-v1")
	// MaxRefIDCount is the per-panel query ceiling: refIds run A..Z and
	// never extend beyond Z.
	MaxRefIDCount = 26
)

// DashboardUID derives the stable dashboard UID from the service name and
// the dashboard schema version: si-<normalized-service>-v1. The
// normalized part is ASCII-only lowercase letters, digits and single
// dashes; when the service name has no usable characters a fixed SHA-256
// prefix is used instead. The result always satisfies
// MinUIDLength <= len(uid) <= MaxUIDLength and the Grafana UID charset.
func DashboardUID(serviceName string) string {
	part := uidServicePart(serviceName)
	return "si-" + part + "-" + UIDSchemaVersion
}

// uidServicePart normalizes the service name into the UID middle segment.
// Separator runs collapse into a single dash; the result is trimmed of
// leading and trailing dashes, padded with a deterministic hash prefix
// when too short, and truncated to uidServicePartMax.
func uidServicePart(serviceName string) string {
	var builder strings.Builder
	separatorPending := false
	for _, character := range serviceName {
		if character >= 'a' && character <= 'z' ||
			character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' {
			if separatorPending && builder.Len() > 0 {
				builder.WriteByte('-')
			}
			separatorPending = false
			if character >= 'A' && character <= 'Z' {
				character = unicode.ToLower(character)
			}
			builder.WriteRune(character)
			continue
		}
		separatorPending = true
	}
	part := strings.Trim(builder.String(), "-")
	hashPrefix := sha256Hex(serviceName)[:16]
	if part == "" {
		// No usable characters: a fixed SHA-256 prefix disambiguates
		// stably without leaking the raw name.
		part = hashPrefix
	}
	for len(part) < uidServicePartMin {
		part += hashPrefix
	}
	if len(part) > uidServicePartMax {
		part = part[:uidServicePartMax]
	}
	return part
}

// PanelIDKey composes the canonical hash key for one panel from its
// category, dashboard item stable ID and fixed panel purpose. The key is
// the single input of the collision-free ID resolution.
func PanelIDKey(category Category, itemID, purpose string) string {
	return "panel:" + string(category) + ":" + itemID + ":" + purpose
}

// RowIDKey composes the canonical hash key for one row.
func RowIDKey(category Category, purpose string) string {
	return "row:" + string(category) + ":" + purpose
}

// ResolvePanelIDs maps canonical source keys onto unique positive panel
// IDs. Collisions are resolved deterministically: the keys are fully
// sorted, and a colliding hash rehashes with an explicit attempt counter,
// so the result never depends on input order or array index. The returned
// IDs align with the input order. Complexity is O(N log N), N being the
// number of keys.
func ResolvePanelIDs(keys []string) []int64 {
	return resolveIDs(keys, hashID)
}

// resolveIDs is the collision-free ID allocator with an injectable hash
// so collision behavior is directly testable.
func resolveIDs(keys []string, hash func(string, int) int64) []int64 {
	if len(keys) == 0 {
		return nil
	}
	sorted := append([]string(nil), keys...)
	sort.Strings(sorted)

	used := make(map[int64]bool, len(keys))
	byKey := make(map[string]int64, len(keys))
	for _, key := range sorted {
		id := hash(key, 0)
		for attempt := 1; used[id]; attempt++ {
			id = hash(key, attempt)
		}
		used[id] = true
		byKey[key] = id
	}

	result := make([]int64, len(keys))
	for index, key := range keys {
		result[index] = byKey[key]
	}
	return result
}

// hashID is the fixed FNV-1a 64-bit digest over the key parts mapped into
// the legal positive integer range. The attempt parameter distinguishes
// rehash rounds after a collision.
func hashID(key string, attempt int) int64 {
	value := fnv1a64(key)
	if attempt > 0 {
		value = fnv1a64(key + "\x00" + strconv.Itoa(attempt))
	}
	return int64(value & 0x7fffffff)
}

func fnv1a64(parts ...string) uint64 {
	const (
		offset = uint64(14695981039346656037)
		prime  = uint64(1099511628211)
	)
	hash := offset
	for _, part := range parts {
		for index := 0; index < len(part); index++ {
			hash ^= uint64(part[index])
			hash *= prime
		}
		// Separate parts with a marker byte so ("ab","c") and ("a","bc")
		// do not collide.
		hash ^= 0xff
		hash *= prime
	}
	return hash
}

// AllocateRefIDs maps canonical query keys to refIds. RefIds are assigned
// A..Z in fully sorted key order; the returned slice aligns with the input
// order. More than MaxRefIDCount queries fail with a stable
// DASHBOARD_PANEL_LIMIT_EXCEEDED error instead of extending beyond Z.
func AllocateRefIDs(keys []string) ([]string, error) {
	if len(keys) > MaxRefIDCount {
		return nil, &CatalogError{
			Code:    CodePanelLimitExceeded,
			Field:   "panel.ref_ids",
			Message: fmt.Sprintf("a panel may have at most %d queries; refIds are limited to A-Z", MaxRefIDCount),
		}
	}
	sorted := append([]string(nil), keys...)
	sort.Strings(sorted)
	refIDByKey := make(map[string]string, len(keys))
	for index, key := range sorted {
		refIDByKey[key] = string(rune('A' + index))
	}
	refIDs := make([]string, len(keys))
	for index, key := range keys {
		refIDs[index] = refIDByKey[key]
	}
	return refIDs, nil
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
