package naming

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"

	"github.com/zhuyanxi/axiom-insight/generator/policy"
)

// NameItem is one candidate name entering the collision table.
type NameItem struct {
	// Signal is the owning signal: metrics, tracing or logging.
	Signal string
	// TargetID is the stable IR entity ID; the disambiguation suffix is
	// derived from it.
	TargetID string
	// Name is the normalized candidate name.
	Name string
}

// NameResult is one disambiguated output name.
type NameResult struct {
	// Signal and TargetID identify the entity.
	Signal   string
	TargetID string
	// Name is the final name, possibly suffixed.
	Name string
	// Suffixed reports whether a collision suffix was applied.
	Suffixed bool
}

// Disambiguate resolves normalized-name collisions within each signal.
// For a group of equal names the entries are sorted by TargetID: the
// smallest ID keeps the base name and every other entry gains a
// "_<sha256(targetID)[:8]>" suffix. The mapping depends only on the
// member set, never on input order. Each suffixed entry produces a
// GEN_NAME_COLLISION warning diagnostic.
func (NamingPolicy) Disambiguate(items []NameItem) ([]NameResult, *DiagnosticList) {
	results := make([]NameResult, 0, len(items))
	if len(items) == 0 {
		return results, new(DiagnosticList)
	}
	ordered := append([]NameItem(nil), items...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Signal != ordered[j].Signal {
			return ordered[i].Signal < ordered[j].Signal
		}
		if ordered[i].Name != ordered[j].Name {
			return ordered[i].Name < ordered[j].Name
		}
		return ordered[i].TargetID < ordered[j].TargetID
	})

	diagnostics := new(DiagnosticList)
	for index := 0; index < len(ordered); {
		groupEnd := index + 1
		for groupEnd < len(ordered) &&
			ordered[groupEnd].Signal == ordered[index].Signal &&
			ordered[groupEnd].Name == ordered[index].Name {
			groupEnd++
		}
		group := ordered[index:groupEnd]
		results = append(results, NameResult{
			Signal: group[0].Signal, TargetID: group[0].TargetID,
			Name: group[0].Name, Suffixed: false,
		})
		for _, item := range group[1:] {
			suffix := shortHash(item.TargetID)
			results = append(results, NameResult{
				Signal: item.Signal, TargetID: item.TargetID,
				Name: item.Name + "_" + suffix, Suffixed: true,
			})
			diagnostics.Add(policy.CodeNameCollision, item.Signal, item.TargetID,
				"name", "normalized name collides with another target and was disambiguated with a stable suffix")
		}
		index = groupEnd
	}
	return results, diagnostics
}

// shortHash derives a stable hex suffix from a target ID. Used only for
// deterministic disambiguation, never for protecting secrets.
func shortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:CollisionSuffixLength]
}

// DisambiguationSuffix returns the stable suffix the collision table
// derives from a target ID. Exported so planners can match disambiguated
// names back to their items.
func DisambiguationSuffix(targetID string) string {
	return shortHash(targetID)
}

// ContainsName reports whether the disambiguated results include a name.
func ContainsName(results []NameResult, name string) bool {
	for _, result := range results {
		if result.Name == name {
			return true
		}
	}
	return false
}
