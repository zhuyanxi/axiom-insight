package dashboard

import (
	"testing"
)

// TestDeterministicAcrossPermutations is AC1: 25 fixed permutations of one
// catalog yield byte-identical UIDs, Row/Panel IDs, refIds and grid
// positions. The permutations come from a private LCG, never global RNG.
func TestDeterministicAcrossPermutations(t *testing.T) {
	fixture := permutationFixture()
	canonical := fixture.compute()

	for round := 1; round <= 25; round++ {
		permuted := fixture.permuted(round)
		got := permuted.compute()
		if !got.equal(canonical) {
			t.Fatalf("permutation %d produces different IDs/layout", round)
		}
	}
}

// permutationFixture is the fixed semantic catalog: one service, six
// categories, items with colliding normalized names, fixed panel purposes.
type permutationCatalog struct {
	serviceName string
	keys        []string
	// refGroups are the per-panel query key sets: refIds are allocated
	// within each panel, never across the catalog.
	refGroups  [][]string
	items      []TitleItem
	categories []CategoryLayout
}

// permutationResult is every deterministic output for one input order.
type permutationResult struct {
	uid       string
	panelIDs  map[string]int64
	refIDs    map[string]string
	rowIDs    map[Category]int64
	rowY      map[Category]int
	positions map[Category]map[int64]GridPlacement
	titles    map[string]string
}

func permutationFixture() permutationCatalog {
	var fixture permutationCatalog
	fixture.serviceName = "payment-service"
	itemsPerCategory := map[Category][]string{
		CategoryServiceOverview: {"item:service_overview:summary"},
		CategoryHTTP:            {"item:http:get_user", "item:http:getuser", "item:http:create_order"},
		CategoryRPC:             {"item:rpc:get_user", "item:rpc:list_orders"},
		CategoryKafka:           {"item:kafka:orders_ingest"},
		CategoryDatabase:        {"item:database:users", "item:database:orders"},
		CategoryCache:           {"item:cache:session"},
	}
	purposes := []string{"rate", "error_ratio", "p95", "inflight"}
	widths := map[string]int{"rate": PanelWidthStat, "error_ratio": PanelWidthTimeSeries, "p95": PanelWidthTimeSeries, "inflight": PanelWidthTable}
	itemIDsByCategory := map[Category][]string(itemsPerCategory)

	for _, category := range CategoryOrder {
		itemIDs := itemIDsByCategory[category]
		var panels []GridPanel
		for _, itemID := range itemIDs {
			// "get_user" and "getuser" normalize to the same display name
			// to exercise title collision determinism.
			title := PanelTitle(category, "rate", itemID)
			fixture.items = append(fixture.items, TitleItem{TargetID: itemID, Title: title})
			var refGroup []string
			for _, purpose := range purposes {
				key := PanelIDKey(category, itemID, purpose)
				fixture.keys = append(fixture.keys, key)
				refGroup = append(refGroup, key)
				panels = append(panels, GridPanel{ID: 0, Width: widths[purpose], Height: 8})
			}
			fixture.refGroups = append(fixture.refGroups, refGroup)
		}
		rowID := RowIDKey(category, "summary")
		fixture.keys = append(fixture.keys, rowID)
		fixture.categories = append(fixture.categories, CategoryLayout{
			Category: category, RowID: 0, Panels: panels,
		})
	}
	// Panel and row IDs are the resolved hash IDs, so the grid has real
	// canonical sort keys; the layout then becomes order-independent.
	resolved := ResolvePanelIDs(fixture.keys)
	idByKey := make(map[string]int64, len(fixture.keys))
	for index, key := range fixture.keys {
		idByKey[key] = resolved[index]
	}
	for categoryIndex := range fixture.categories {
		category := fixture.categories[categoryIndex].Category
		fixture.categories[categoryIndex].RowID = idByKey[RowIDKey(category, "summary")]
		// Panels were appended per item, four purposes per item: panel k
		// belongs to item k/4 with purpose k%4.
		itemIDs := itemIDsByCategory[category]
		for panelIndex := range fixture.categories[categoryIndex].Panels {
			key := PanelIDKey(category, itemIDs[panelIndex/len(purposes)], purposes[panelIndex%len(purposes)])
			fixture.categories[categoryIndex].Panels[panelIndex].ID = idByKey[key]
		}
	}
	return fixture
}

// compute runs every deterministic primitive over the fixture's current
// ordering.
func (fixture permutationCatalog) compute() permutationResult {
	result := permutationResult{
		uid:       DashboardUID(fixture.serviceName),
		panelIDs:  make(map[string]int64, len(fixture.keys)),
		refIDs:    make(map[string]string, len(fixture.keys)),
		rowIDs:    make(map[Category]int64),
		rowY:      make(map[Category]int),
		positions: make(map[Category]map[int64]GridPlacement),
		titles:    make(map[string]string),
	}

	ids := ResolvePanelIDs(fixture.keys)
	for index, key := range fixture.keys {
		result.panelIDs[key] = ids[index]
	}
	for _, group := range fixture.refGroups {
		refIDs, err := AllocateRefIDs(group)
		if err != nil {
			panic(err)
		}
		for index, key := range group {
			result.refIDs[key] = refIDs[index]
		}
	}

	for _, row := range PlanGrid(fixture.categories) {
		result.rowIDs[row.Category] = row.RowID
		result.rowY[row.Category] = row.Y
		result.positions[row.Category] = make(map[int64]GridPlacement)
		for _, placement := range row.Panels {
			result.positions[row.Category][placement.ID] = placement
		}
	}

	titles, _ := DisambiguateTitles(fixture.items)
	for _, title := range titles {
		result.titles[title.TargetID] = title.Title
	}
	return result
}

// equal compares two results as pure maps, ignoring iteration order.
func (result permutationResult) equal(other permutationResult) bool {
	if result.uid != other.uid {
		return false
	}
	if !stringInt64MapsEqual(result.panelIDs, other.panelIDs) {
		return false
	}
	if !stringStringMapsEqual(result.refIDs, other.refIDs) {
		return false
	}
	if !categoryInt64MapsEqual(result.rowIDs, other.rowIDs) || !categoryIntMapsEqual(result.rowY, other.rowY) {
		return false
	}
	for category, placements := range result.positions {
		for id, placement := range placements {
			if other.positions[category][id] != placement {
				return false
			}
		}
	}
	return stringStringMapsEqual(result.titles, other.titles)
}

// permuted returns the same semantic catalog with all orderings
// deterministically shuffled by the round-numbered LCG seed.
func (fixture permutationCatalog) permuted(round int) permutationCatalog {
	random := newGridLCG(uint64(round*0x9e3779b1 + 1))
	shuffleStrings := func(items []string) []string {
		result := append([]string(nil), items...)
		for index := len(result) - 1; index > 0; index-- {
			swap := int(random.next() % uint64(index+1))
			result[index], result[swap] = result[swap], result[index]
		}
		return result
	}

	permuted := permutationCatalog{
		serviceName: fixture.serviceName,
		keys:        shuffleStrings(fixture.keys),
		refGroups:   append([][]string(nil), fixture.refGroups...),
		items:       append([]TitleItem(nil), fixture.items...),
		categories:  append([]CategoryLayout(nil), fixture.categories...),
	}
	for index := len(permuted.items) - 1; index > 0; index-- {
		swap := int(random.next() % uint64(index+1))
		permuted.items[index], permuted.items[swap] = permuted.items[swap], permuted.items[index]
	}
	for index := len(permuted.categories) - 1; index > 0; index-- {
		swap := int(random.next() % uint64(index+1))
		permuted.categories[index], permuted.categories[swap] = permuted.categories[swap], permuted.categories[index]
	}
	for groupIndex := range permuted.refGroups {
		group := shuffleStrings(permuted.refGroups[groupIndex])
		permuted.refGroups[groupIndex] = group
	}
	for index := len(permuted.refGroups) - 1; index > 0; index-- {
		swap := int(random.next() % uint64(index+1))
		permuted.refGroups[index], permuted.refGroups[swap] = permuted.refGroups[swap], permuted.refGroups[index]
	}
	for categoryIndex := range permuted.categories {
		panels := append([]GridPanel(nil), permuted.categories[categoryIndex].Panels...)
		for index := len(panels) - 1; index > 0; index-- {
			swap := int(random.next() % uint64(index+1))
			panels[index], panels[swap] = panels[swap], panels[index]
		}
		permuted.categories[categoryIndex].Panels = panels
	}
	return permuted
}

func stringInt64MapsEqual(first, second map[string]int64) bool {
	if len(first) != len(second) {
		return false
	}
	for key, value := range first {
		if second[key] != value {
			return false
		}
	}
	return true
}

func stringStringMapsEqual(first, second map[string]string) bool {
	if len(first) != len(second) {
		return false
	}
	for key, value := range first {
		if second[key] != value {
			return false
		}
	}
	return true
}

func categoryInt64MapsEqual(first, second map[Category]int64) bool {
	if len(first) != len(second) {
		return false
	}
	for key, value := range first {
		if second[key] != value {
			return false
		}
	}
	return true
}

func categoryIntMapsEqual(first, second map[Category]int) bool {
	if len(first) != len(second) {
		return false
	}
	for key, value := range first {
		if second[key] != value {
			return false
		}
	}
	return true
}
