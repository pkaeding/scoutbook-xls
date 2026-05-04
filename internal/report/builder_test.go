package report

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/pkaeding/scoutbook-xls/internal/scouting"
)

// --- test helpers ---------------------------------------------------------

func strPtr(s string) *string { return &s }

// newRankReq builds a leaf RankRequirement matching a rank-sheet row. The
// requirement is assumed to link to a single adventure by id/name; it is NOT
// marked electiveAdventure unless the test overrides afterwards. A sortOrder
// is synthesized from `number` ("1a" → "1.1", "1b" → "1.2", "2a" → "2.1")
// so the builder's rank-req ordering passes through naturally.
func newRankReq(number, name string, linkedAdvID int, linkedAdvName string, pct float64) scouting.RankRequirement {
	return scouting.RankRequirement{
		RequirementNumber: number,
		SortOrder:         sortOrderFromReqNumber(number),
		Name:              name,
		PercentCompleted:  pct,
		LinkedAdventureID: &linkedAdvID,
		LinkedAdventure: scouting.LinkedAdventure{
			ID:               linkedAdvID,
			Name:             linkedAdvName,
			PercentCompleted: pct,
		},
	}
}

// sortOrderFromReqNumber converts a scouting-style requirement number like
// "1a" to a dotted sortOrder "1.1". Non-letter suffixes are ignored beyond
// the digit prefix.
func sortOrderFromReqNumber(n string) string {
	var digits, suffix string
	for i, c := range n {
		if c >= '0' && c <= '9' {
			digits += string(c)
			continue
		}
		suffix = n[i:]
		break
	}
	if digits == "" {
		return ""
	}
	if suffix == "" {
		return digits
	}
	// letter suffix → 1-indexed integer ('a'→1, 'b'→2, ...)
	letterNum := 0
	if len(suffix) > 0 {
		c := suffix[0]
		if c >= 'a' && c <= 'z' {
			letterNum = int(c-'a') + 1
		} else if c >= 'A' && c <= 'Z' {
			letterNum = int(c-'A') + 1
		}
	}
	if letterNum == 0 {
		return digits
	}
	return digits + "." + strconv.Itoa(letterNum)
}

// newAdventure builds a scout-level Adventure entry (the filtered list item).
func newAdventure(id int, name, shortName string, pct float64) scouting.Adventure {
	return scouting.Adventure{
		AdventureID:      id,
		AdventureName:    name,
		ShortName:        shortName,
		RankID:           11, // Webelos by default; tests that care can override
		PercentCompleted: pct,
	}
}

// newReq builds a Requirement on an adventure detail with the given number,
// sort/display order, and optional date-completed pointer. SortOrder is
// derived from the leading integer of `number` so tests don't have to set
// it explicitly ("1" → 1.0, "2" → 2.0).
func newReq(number, name string, dateCompleted *string) scouting.Requirement {
	sortOrder := 0.0
	if n, err := strconv.ParseFloat(number, 64); err == nil {
		sortOrder = n
	}
	return scouting.Requirement{
		RequirementNumber: number,
		RequirementName:   name,
		SortOrder:         sortOrder,
		DateCompleted:     dateCompleted,
		IsCompleted:       dateCompleted != nil,
	}
}

// newAdventureReqs builds an AdventureRequirements detail for a single
// adventure with the provided leaf requirements.
func newAdventureReqs(id int, name string, pct float64, reqs ...scouting.Requirement) scouting.AdventureRequirements {
	return scouting.AdventureRequirements{
		AdventureID:      id,
		AdventureName:    name,
		PercentCompleted: pct,
		Requirements:     reqs,
	}
}

// newScoutInput is a concise constructor for ScoutInput. The userID defaults
// to a non-zero hash of the first name so the default-constructed scout is
// considered "resolved"; tests that want an unresolved scout pass UserID=0
// by constructing the struct literal directly.
func newScoutInput(first, last string, userID int, rankReqs scouting.RankRequirements, advs []scouting.Adventure, advReqs map[int]scouting.AdventureRequirements) ScoutInput {
	return ScoutInput{
		FirstName:     first,
		LastName:      last,
		FullName:      first + " " + last,
		UserID:        userID,
		RankReqs:      rankReqs,
		Adventures:    advs,
		AdventureReqs: advReqs,
	}
}

// findSummaryRow finds the first summary row whose label equals or contains s.
// Returns -1 if no such row exists.
func findSummaryRow(rows []SummaryRow, s string) int {
	for i, r := range rows {
		if r.Label == s || strings.Contains(r.Label, s) {
			return i
		}
	}
	return -1
}

// findAdventureSheet finds the AdventureSheet with the given id, or nil.
func findAdventureSheet(sheets []AdventureSheet, id int) *AdventureSheet {
	for i := range sheets {
		if sheets[i].AdventureID == id {
			return &sheets[i]
		}
	}
	return nil
}

// --- tests ----------------------------------------------------------------

func TestBuildReportOrdersScoutsByFirstName(t *testing.T) {
	// Inputs deliberately not sorted: Charlie, Alice, Bob.
	scouts := []ScoutInput{
		newScoutInput("Charlie", "Cat", 1001, scouting.RankRequirements{}, nil, nil),
		newScoutInput("Alice", "Apple", 1002, scouting.RankRequirements{}, nil, nil),
		newScoutInput("Bob", "Bear", 1003, scouting.RankRequirements{}, nil, nil),
	}

	r := BuildReport("Webelos", "1", "Webelos", scouts)

	if got, want := len(r.Scouts), 3; got != want {
		t.Fatalf("len(Scouts) = %d, want %d", got, want)
	}
	want := []string{"Alice", "Bob", "Charlie"}
	for i, w := range want {
		if got := r.Scouts[i].FirstName; got != w {
			t.Errorf("Scouts[%d].FirstName = %q, want %q", i, got, w)
		}
	}
}

func TestBuildReportSummaryHasRankRequirementsFirst(t *testing.T) {
	// Two scouts, each with two rank reqs (1a, 2a) and one adventure with
	// percentCompleted > 0 so the adventure row is not excluded.
	rankReqs := scouting.RankRequirements{
		ID:   11,
		Name: "Webelos",
		Requirements: []scouting.RankRequirement{
			newRankReq("1a", "Bobcat (Webelos)", 132, "Bobcat (Webelos)", 0.5),
			newRankReq("2a", "My Family", 140, "My Family", 0.5),
		},
	}
	advs := []scouting.Adventure{
		newAdventure(140, "My Family", "MyFam", 0.5),
	}

	a := newScoutInput("Alice", "Apple", 1, rankReqs, advs, nil)
	b := newScoutInput("Bob", "Bear", 2, rankReqs, advs, nil)

	r := BuildReport("Webelos", "1", "Webelos", []ScoutInput{a, b})

	if len(r.SummaryRows) < 5 {
		t.Fatalf("len(SummaryRows) = %d, want >= 5 (header, 2 rank reqs, header, 1 adv)", len(r.SummaryRows))
	}

	// 0: Rank Requirements header
	if r.SummaryRows[0].Kind != SummaryRowSectionHeader {
		t.Errorf("SummaryRows[0].Kind = %v, want SummaryRowSectionHeader", r.SummaryRows[0].Kind)
	}
	if !strings.Contains(strings.ToLower(r.SummaryRows[0].Label), "rank") {
		t.Errorf("SummaryRows[0].Label = %q, want to contain \"rank\"", r.SummaryRows[0].Label)
	}

	// 1: rank req 1a
	if r.SummaryRows[1].Kind != SummaryRowRankReq {
		t.Errorf("SummaryRows[1].Kind = %v, want SummaryRowRankReq", r.SummaryRows[1].Kind)
	}
	if !strings.Contains(r.SummaryRows[1].Label, "1a") {
		t.Errorf("SummaryRows[1].Label = %q, want to contain \"1a\"", r.SummaryRows[1].Label)
	}

	// 2: rank req 2a
	if r.SummaryRows[2].Kind != SummaryRowRankReq {
		t.Errorf("SummaryRows[2].Kind = %v, want SummaryRowRankReq", r.SummaryRows[2].Kind)
	}
	if !strings.Contains(r.SummaryRows[2].Label, "2a") {
		t.Errorf("SummaryRows[2].Label = %q, want to contain \"2a\"", r.SummaryRows[2].Label)
	}

	// 3: Adventures section header
	if r.SummaryRows[3].Kind != SummaryRowSectionHeader {
		t.Errorf("SummaryRows[3].Kind = %v, want SummaryRowSectionHeader", r.SummaryRows[3].Kind)
	}
	if !strings.Contains(strings.ToLower(r.SummaryRows[3].Label), "adventure") {
		t.Errorf("SummaryRows[3].Label = %q, want to contain \"adventure\"", r.SummaryRows[3].Label)
	}

	// 4: adventure row
	if r.SummaryRows[4].Kind != SummaryRowAdventure {
		t.Errorf("SummaryRows[4].Kind = %v, want SummaryRowAdventure", r.SummaryRows[4].Kind)
	}
}

func TestBuildReportAdventureRowPctsMatchScoutData(t *testing.T) {
	// Scout A: "My Family" at 0.5; Scout B: 0.25. Scout order after
	// sort-by-first-name must be A (Alice) then B (Bob).
	advA := []scouting.Adventure{newAdventure(140, "My Family", "MyFam", 0.5)}
	advB := []scouting.Adventure{newAdventure(140, "My Family", "MyFam", 0.25)}

	a := newScoutInput("Alice", "Apple", 1, scouting.RankRequirements{}, advA, nil)
	b := newScoutInput("Bob", "Bear", 2, scouting.RankRequirements{}, advB, nil)

	r := BuildReport("Webelos", "1", "Webelos", []ScoutInput{b, a}) // reversed on input

	idx := findSummaryRow(r.SummaryRows, "My Family")
	if idx < 0 {
		t.Fatalf("no summary row found for \"My Family\"; rows = %+v", r.SummaryRows)
	}
	row := r.SummaryRows[idx]
	if row.Kind != SummaryRowAdventure {
		t.Errorf("My Family row.Kind = %v, want SummaryRowAdventure", row.Kind)
	}
	want := []float64{0.5, 0.25}
	if !slices.Equal(row.Percents, want) {
		t.Errorf("My Family row.Percents = %v, want %v", row.Percents, want)
	}
}

func TestBuildReportExcludesAdventuresNoScoutStarted(t *testing.T) {
	// Two scouts, two adventures each. Adv 140 is 0% for both; Adv 141 is
	// 0.3 for A, 0 for B. Only adv 141 should appear.
	advsA := []scouting.Adventure{
		newAdventure(140, "My Family", "MyFam", 0),
		newAdventure(141, "Duty to God", "DtG", 0.3),
	}
	advsB := []scouting.Adventure{
		newAdventure(140, "My Family", "MyFam", 0),
		newAdventure(141, "Duty to God", "DtG", 0),
	}

	a := newScoutInput("Alice", "Apple", 1, scouting.RankRequirements{}, advsA, nil)
	b := newScoutInput("Bob", "Bear", 2, scouting.RankRequirements{}, advsB, nil)

	r := BuildReport("Webelos", "1", "Webelos", []ScoutInput{a, b})

	// Adv 140 absent from summary.
	if idx := findSummaryRow(r.SummaryRows, "My Family"); idx >= 0 {
		t.Errorf("summary rows unexpectedly contained \"My Family\" (all zeros); rows = %+v", r.SummaryRows)
	}
	// Adv 141 present.
	if idx := findSummaryRow(r.SummaryRows, "Duty to God"); idx < 0 {
		t.Errorf("summary rows missing \"Duty to God\"; rows = %+v", r.SummaryRows)
	}

	// Only adv 141 as AdventureSheet.
	if findAdventureSheet(r.Adventures, 140) != nil {
		t.Errorf("AdventureSheet for 140 unexpectedly present (no scout started it)")
	}
	if findAdventureSheet(r.Adventures, 141) == nil {
		t.Errorf("AdventureSheet for 141 missing")
	}
}

func TestBuildReportRankReqPercentsFromRawField(t *testing.T) {
	// requirementNumber "2" models an elective-count rank requirement where
	// the API has pre-baked percentCompleted to 0/0.5/1.0. Use that value
	// directly; do not recompute from linkedElectiveAdventures.
	mkRankReqs := func(pct float64) scouting.RankRequirements {
		return scouting.RankRequirements{
			Requirements: []scouting.RankRequirement{
				{
					RequirementNumber: "2",
					Name:              "Complete 2 elective adventures",
					ElectiveAdventure: true,
					PercentCompleted:  pct,
				},
			},
		}
	}

	a := newScoutInput("Alice", "Apple", 1, mkRankReqs(0.5), nil, nil)
	b := newScoutInput("Bob", "Bear", 2, mkRankReqs(1.0), nil, nil)

	r := BuildReport("Webelos", "1", "Webelos", []ScoutInput{a, b})

	idx := findSummaryRow(r.SummaryRows, "2")
	if idx < 0 {
		t.Fatalf("no summary row for rank req \"2\"; rows = %+v", r.SummaryRows)
	}
	row := r.SummaryRows[idx]
	if row.Kind != SummaryRowRankReq {
		t.Errorf("rank req \"2\" row.Kind = %v, want SummaryRowRankReq", row.Kind)
	}
	want := []float64{0.5, 1.0}
	if !slices.Equal(row.Percents, want) {
		t.Errorf("rank req \"2\" row.Percents = %v, want %v", row.Percents, want)
	}
}

func TestBuildReportPerAdventureSheetRequirements(t *testing.T) {
	// 1 scout, 1 adventure (id=140) with 3 requirements.
	advReqs := newAdventureReqs(
		140, "My Family", 0.33,
		newReq("1", "Talk about faith", nil),
		newReq("2", "Family tree", strPtr("2025-12-11")),
		newReq("3", "Plan meal", nil),
	)
	advs := []scouting.Adventure{newAdventure(140, "My Family", "MyFam", 0.33)}

	a := newScoutInput("Alice", "Apple", 1, scouting.RankRequirements{}, advs, map[int]scouting.AdventureRequirements{140: advReqs})

	r := BuildReport("Webelos", "1", "Webelos", []ScoutInput{a})

	sheet := findAdventureSheet(r.Adventures, 140)
	if sheet == nil {
		t.Fatalf("no AdventureSheet for id=140")
	}
	if got, want := len(sheet.Rows), 3; got != want {
		t.Fatalf("len(sheet.Rows) = %d, want %d", got, want)
	}

	// Find row "2" and assert DatesCompleted[0] == "2025-12-11".
	var row2 *AdventureRow
	for i := range sheet.Rows {
		if strings.HasPrefix(sheet.Rows[i].Label, "2") {
			row2 = &sheet.Rows[i]
			break
		}
	}
	if row2 == nil {
		t.Fatalf("no row starting with \"2\" in sheet; rows = %+v", sheet.Rows)
	}
	if len(row2.DatesCompleted) != 1 {
		t.Fatalf("row2.DatesCompleted len = %d, want 1", len(row2.DatesCompleted))
	}
	if row2.DatesCompleted[0] == nil {
		t.Fatalf("row2.DatesCompleted[0] = nil, want pointer to \"2025-12-11\"")
	}
	if got, want := *row2.DatesCompleted[0], "2025-12-11"; got != want {
		t.Errorf("row2.DatesCompleted[0] = %q, want %q", got, want)
	}

	// Rows 1 and 3 should be nil.
	for _, row := range sheet.Rows {
		if strings.HasPrefix(row.Label, "1") || strings.HasPrefix(row.Label, "3") {
			if len(row.DatesCompleted) != 1 {
				t.Fatalf("row %q DatesCompleted len = %d, want 1", row.Label, len(row.DatesCompleted))
			}
			if row.DatesCompleted[0] != nil {
				t.Errorf("row %q DatesCompleted[0] = %q, want nil", row.Label, *row.DatesCompleted[0])
			}
		}
	}

	// OverallPcts should reflect the scout's percentCompleted on this adventure.
	if len(sheet.OverallPcts) != 1 {
		t.Fatalf("len(sheet.OverallPcts) = %d, want 1", len(sheet.OverallPcts))
	}
	if got, want := sheet.OverallPcts[0], 0.33; got != want {
		t.Errorf("sheet.OverallPcts[0] = %v, want %v", got, want)
	}
}

func TestBuildReportPerAdventureSheetOrderingMatchesRequirementNumber(t *testing.T) {
	// Requirements delivered out-of-order: "3", "1", "2". Rows should appear
	// in requirementNumber order: "1", "2", "3".
	advReqs := newAdventureReqs(
		140, "My Family", 0.0,
		newReq("3", "third", nil),
		newReq("1", "first", nil),
		newReq("2", "second", nil),
	)
	advs := []scouting.Adventure{newAdventure(140, "My Family", "MyFam", 0.1)}

	a := newScoutInput("Alice", "Apple", 1, scouting.RankRequirements{}, advs, map[int]scouting.AdventureRequirements{140: advReqs})

	r := BuildReport("Webelos", "1", "Webelos", []ScoutInput{a})

	sheet := findAdventureSheet(r.Adventures, 140)
	if sheet == nil {
		t.Fatalf("no AdventureSheet for id=140")
	}
	if got, want := len(sheet.Rows), 3; got != want {
		t.Fatalf("len(sheet.Rows) = %d, want %d", got, want)
	}
	wantPrefixes := []string{"1", "2", "3"}
	for i, p := range wantPrefixes {
		if !strings.HasPrefix(sheet.Rows[i].Label, p) {
			t.Errorf("sheet.Rows[%d].Label = %q, want prefix %q", i, sheet.Rows[i].Label, p)
		}
	}
}

func TestBuildReportDenLabel(t *testing.T) {
	r := BuildReport("Webelos", "1", "Webelos", nil)
	if got, want := r.DenLabel, "Webelos 1"; got != want {
		t.Errorf("DenLabel = %q, want %q", got, want)
	}
	if got, want := r.RankName, "Webelos"; got != want {
		t.Errorf("RankName = %q, want %q", got, want)
	}
}

func TestBuildReportSkipsScoutWithMissingUserId(t *testing.T) {
	// One scout with UserID=0 (unresolved); should be dropped and a
	// warning should mention their name.
	good := newScoutInput("Alice", "Apple", 1, scouting.RankRequirements{}, nil, nil)
	bad := ScoutInput{
		FirstName: "Zane",
		LastName:  "Zephyr",
		FullName:  "Zane Zephyr",
		UserID:    0,
	}

	r := BuildReport("Webelos", "1", "Webelos", []ScoutInput{good, bad})

	for _, s := range r.Scouts {
		if s.UserID == 0 {
			t.Errorf("Scouts unexpectedly contains scout with UserID=0: %+v", s)
		}
		if s.FirstName == "Zane" {
			t.Errorf("Scouts unexpectedly contains unresolved \"Zane\"")
		}
	}

	mentioned := false
	for _, w := range r.Warnings {
		if strings.Contains(w, "Zane") {
			mentioned = true
			break
		}
	}
	if !mentioned {
		t.Errorf("expected a warning mentioning \"Zane\", got %v", r.Warnings)
	}
}

func TestBuildReportHandlesScoutWithNoAdventuresStarted(t *testing.T) {
	// Scout has adventures listed but all at 0%. Scout column still
	// present; Adventures section has 0 adventure rows; per-adventure
	// sheets empty.
	advs := []scouting.Adventure{
		newAdventure(140, "My Family", "MyFam", 0),
		newAdventure(141, "Duty to God", "DtG", 0),
	}
	a := newScoutInput("Alice", "Apple", 1, scouting.RankRequirements{}, advs, nil)

	r := BuildReport("Webelos", "1", "Webelos", []ScoutInput{a})

	if got, want := len(r.Scouts), 1; got != want {
		t.Fatalf("len(Scouts) = %d, want %d", got, want)
	}
	if r.Scouts[0].FirstName != "Alice" {
		t.Errorf("Scouts[0].FirstName = %q, want \"Alice\"", r.Scouts[0].FirstName)
	}

	advRowCount := 0
	for _, row := range r.SummaryRows {
		if row.Kind == SummaryRowAdventure {
			advRowCount++
		}
	}
	if advRowCount != 0 {
		t.Errorf("got %d adventure summary rows, want 0", advRowCount)
	}

	if got := len(r.Adventures); got != 0 {
		t.Errorf("len(Adventures) = %d, want 0", got)
	}
}

// TestBuildReportExcludesParentRankRequirements verifies that "group" rank
// requirements (ones with an empty requirementNumber like "Complete the six
// required adventures:") don't show up as rows in the summary.
func TestBuildReportExcludesParentRankRequirements(t *testing.T) {
	parent := scouting.RankRequirement{
		RequirementNumber: "", // parent / group row
		SortOrder:         "1",
		Name:              "Complete the six required adventures:",
		ChildrenRequired:  "6",
	}
	leaf1 := newRankReq("1a", "Bobcat", 132, "Bobcat", 1.0)
	leaf2 := newRankReq("1b", "Walkabout", 62, "Walkabout", 0.5)

	rankReqs := scouting.RankRequirements{
		ID:           11,
		Name:         "Webelos",
		Requirements: []scouting.RankRequirement{parent, leaf1, leaf2},
	}
	a := newScoutInput("Alice", "Apple", 1, rankReqs, nil, nil)

	r := BuildReport("Webelos", "1", "Webelos", []ScoutInput{a})

	for _, row := range r.SummaryRows {
		if row.Kind == SummaryRowRankReq &&
			strings.Contains(row.Label, "Complete the six") {
			t.Errorf("parent requirement unexpectedly rendered as row: %q", row.Label)
		}
	}

	rankRowCount := 0
	for _, row := range r.SummaryRows {
		if row.Kind == SummaryRowRankReq {
			rankRowCount++
		}
	}
	if rankRowCount != 2 {
		t.Errorf("rank-req rows = %d, want 2 (leaves only)", rankRowCount)
	}
}

// TestBuildReportOrdersRankReqsBySortOrder verifies that the builder sorts
// rank requirements by the dotted sortOrder ("1.1" < "1.2" < "2.1"), which
// yields 1a/1b/1c/2a/2b rather than the API's interleaved order.
func TestBuildReportOrdersRankReqsBySortOrder(t *testing.T) {
	// Delivered in an interleaved order like the real API.
	delivered := []scouting.RankRequirement{
		newRankReq("1a", "Bobcat", 132, "Bobcat", 0.5),
		newRankReq("2a", "Elective", 0, "", 0.5),
		newRankReq("1b", "Walkabout", 62, "Walkabout", 0.5),
		newRankReq("2b", "Elective", 0, "", 0.5),
		newRankReq("1c", "SFH", 61, "SFH", 0.5),
	}
	// newRankReq for "2a"/"2b" sets linkedAdvID=0, which is not a real id.
	// Make them electives with no linked adventure for cleanliness.
	delivered[1].LinkedAdventureID = nil
	delivered[1].ElectiveAdventure = true
	delivered[3].LinkedAdventureID = nil
	delivered[3].ElectiveAdventure = true

	rankReqs := scouting.RankRequirements{
		ID: 11, Name: "Webelos", Requirements: delivered,
	}
	a := newScoutInput("Alice", "Apple", 1, rankReqs, nil, nil)
	r := BuildReport("Webelos", "1", "Webelos", []ScoutInput{a})

	want := []string{"1a", "1b", "1c", "2a", "2b"}
	var got []string
	for _, row := range r.SummaryRows {
		if row.Kind != SummaryRowRankReq {
			continue
		}
		// Label format is "{num} — {name}".
		parts := strings.SplitN(row.Label, " ", 2)
		got = append(got, parts[0])
	}
	if !slices.Equal(got, want) {
		t.Errorf("rank-req order = %v, want %v", got, want)
	}
}

// TestBuildReportOrdersAdventuresByRankReqOrder verifies that adventures
// linked directly to rank requirements appear first (in rank-req order),
// regardless of scout-list iteration order, and are included even when no
// scout has started them yet.
func TestBuildReportOrdersAdventuresByRankReqOrder(t *testing.T) {
	// Rank has 3 required linked adventures in order: Bobcat(132),
	// Walkabout(62), SFH(61). Plus one elective slot that links to nothing.
	rankReqs := scouting.RankRequirements{
		ID: 11, Name: "Webelos",
		Requirements: []scouting.RankRequirement{
			newRankReq("1a", "Bobcat", 132, "Bobcat", 0),
			newRankReq("1b", "Walkabout", 62, "Walkabout", 0),
			newRankReq("1c", "SFH", 61, "SFH", 0),
		},
	}

	// Scout's adventures include the 3 required + 1 elective (adv 69, Art
	// Explosion) at 0.5 progress. The API typically lists them in some
	// arbitrary order — let's scramble them.
	advs := []scouting.Adventure{
		newAdventure(61, "Stronger, Faster, Higher", "SFH", 0),
		newAdventure(69, "Art Explosion", "Art Explosion", 0.5),
		newAdventure(132, "Bobcat (Webelos)", "Bobcat (Webelos)", 0),
		newAdventure(62, "Webelos Walkabout", "Webelos Walkabout", 0),
	}

	a := newScoutInput("Alice", "Apple", 1, rankReqs, advs, nil)
	r := BuildReport("Webelos", "1", "Webelos", []ScoutInput{a})

	// Expected summary adventure rows in order: Bobcat(132), Walkabout(62),
	// SFH(61), then Art Explosion(69) since the scout has started it.
	wantOrder := []string{"Bobcat (Webelos)", "Webelos Walkabout", "Stronger, Faster, Higher", "Art Explosion"}
	var got []string
	for _, row := range r.SummaryRows {
		if row.Kind == SummaryRowAdventure {
			got = append(got, row.Label)
		}
	}
	if !slices.Equal(got, wantOrder) {
		t.Errorf("adventure summary order = %v, want %v", got, wantOrder)
	}

	// Per-adventure sheets should appear in the same order.
	var sheetOrder []int
	for _, s := range r.Adventures {
		sheetOrder = append(sheetOrder, s.AdventureID)
	}
	wantSheetOrder := []int{132, 62, 61, 69}
	if !slices.Equal(sheetOrder, wantSheetOrder) {
		t.Errorf("adventure sheet order = %v, want %v", sheetOrder, wantSheetOrder)
	}
}

// TestBuildReportAdventureRowLabelUsesRequirementText verifies that a
// per-adventure sheet row's label reads "<number> — <requirement text>",
// NOT "<number> — <adventure name>".
func TestBuildReportAdventureRowLabelUsesRequirementText(t *testing.T) {
	advReqs := newAdventureReqs(
		140, "My Family", 0.25,
		newReq("1", "With your parent, plan, cook, and eat a balanced meal.", nil),
		newReq("2", "Carry out an act of kindness.", strPtr("2025-12-11")),
	)
	advs := []scouting.Adventure{newAdventure(140, "My Family", "My Family", 0.25)}

	a := newScoutInput("Alice", "Apple", 1, scouting.RankRequirements{}, advs, map[int]scouting.AdventureRequirements{140: advReqs})
	r := BuildReport("Webelos", "1", "Webelos", []ScoutInput{a})

	sheet := findAdventureSheet(r.Adventures, 140)
	if sheet == nil {
		t.Fatalf("no AdventureSheet for id=140")
	}
	if got, want := len(sheet.Rows), 2; got != want {
		t.Fatalf("len(Rows) = %d, want %d", got, want)
	}

	row1 := sheet.Rows[0].Label
	if !strings.Contains(row1, "plan, cook, and eat") {
		t.Errorf("row 0 label = %q, want to contain requirement text", row1)
	}
	if strings.Contains(row1, "My Family") {
		t.Errorf("row 0 label = %q, should NOT contain the adventure name", row1)
	}

	row2 := sheet.Rows[1].Label
	if !strings.Contains(row2, "act of kindness") {
		t.Errorf("row 1 label = %q, want to contain requirement text", row2)
	}
}

// TestBuildReportRankReqAllCompleted verifies AllCompleted flips true on a
// rank-req row only when every resolved scout has a DateCompleted.
func TestBuildReportRankReqAllCompleted(t *testing.T) {
	completeReq := func(num, name, date string) scouting.RankRequirement {
		r := newRankReq(num, name, 132, "Linked", 1.0)
		r.Completed = true
		r.DateCompleted = strPtr(date)
		return r
	}
	partialReq := func(num, name string) scouting.RankRequirement {
		r := newRankReq(num, name, 62, "Linked", 0.5)
		return r
	}
	aliceRank := scouting.RankRequirements{
		ID: 11, Name: "Webelos",
		Requirements: []scouting.RankRequirement{
			completeReq("1a", "Bobcat", "2025-09-11"),
			completeReq("1b", "Walkabout", "2025-10-01"),
		},
	}
	bobRank := scouting.RankRequirements{
		ID: 11, Name: "Webelos",
		Requirements: []scouting.RankRequirement{
			completeReq("1a", "Bobcat", "2025-09-15"),
			partialReq("1b", "Walkabout"),
		},
	}

	alice := newScoutInput("Alice", "Apple", 1, aliceRank, nil, nil)
	bob := newScoutInput("Bob", "Berry", 2, bobRank, nil, nil)

	r := BuildReport("Webelos", "1", "Webelos", []ScoutInput{alice, bob})

	findRow := func(num string) *SummaryRow {
		for i := range r.SummaryRows {
			if strings.HasPrefix(r.SummaryRows[i].Label, num+" ") ||
				strings.HasPrefix(r.SummaryRows[i].Label, num+" ") {
				return &r.SummaryRows[i]
			}
		}
		return nil
	}
	row1a := findRow("1a")
	if row1a == nil {
		t.Fatalf("no row for 1a")
	}
	if !row1a.AllCompleted {
		t.Errorf("1a AllCompleted = false, want true (both scouts have dates)")
	}

	row1b := findRow("1b")
	if row1b == nil {
		t.Fatalf("no row for 1b")
	}
	if row1b.AllCompleted {
		t.Errorf("1b AllCompleted = true, want false (Bob has no date)")
	}
}

// TestBuildReportAdventureRowAllCompleted verifies AllCompleted flips true
// on a per-adventure-sheet row only when every scout has completed that
// requirement.
func TestBuildReportAdventureRowAllCompleted(t *testing.T) {
	aliceAdvReqs := newAdventureReqs(140, "My Family", 1.0,
		newReq("1", "first", strPtr("2025-09-01")),
		newReq("2", "second", strPtr("2025-09-02")),
	)
	bobAdvReqs := newAdventureReqs(140, "My Family", 0.5,
		newReq("1", "first", strPtr("2025-09-05")),
		newReq("2", "second", nil),
	)
	advs := []scouting.Adventure{newAdventure(140, "My Family", "My Family", 0.75)}

	alice := newScoutInput("Alice", "Apple", 1, scouting.RankRequirements{}, advs, map[int]scouting.AdventureRequirements{140: aliceAdvReqs})
	bob := newScoutInput("Bob", "Berry", 2, scouting.RankRequirements{}, advs, map[int]scouting.AdventureRequirements{140: bobAdvReqs})

	r := BuildReport("Webelos", "1", "Webelos", []ScoutInput{alice, bob})

	sheet := findAdventureSheet(r.Adventures, 140)
	if sheet == nil {
		t.Fatalf("no AdventureSheet for id=140")
	}
	if len(sheet.Rows) != 2 {
		t.Fatalf("len(Rows) = %d, want 2", len(sheet.Rows))
	}
	// Row "1": both scouts have dates → AllCompleted true.
	if !sheet.Rows[0].AllCompleted {
		t.Errorf("sheet.Rows[0] (req 1) AllCompleted = false, want true")
	}
	// Row "2": Bob missing → AllCompleted false.
	if sheet.Rows[1].AllCompleted {
		t.Errorf("sheet.Rows[1] (req 2) AllCompleted = true, want false")
	}
}

// TestBuildReportAdventureSheetSkipsNoteRows verifies that API-provided
// "Note:" rows (requirementNumber == "") are excluded from per-adventure
// sheet rendering.
func TestBuildReportAdventureSheetSkipsNoteRows(t *testing.T) {
	note := scouting.Requirement{
		RequirementNumber: "",
		RequirementName:   "Note: A Cub Scout may earn this Adventure by...",
		SortOrder:         0.1,
	}
	req1 := newReq("1", "Do the thing", nil)
	advReqs := scouting.AdventureRequirements{
		AdventureID:      140,
		AdventureName:    "My Family",
		PercentCompleted: 0.5,
		Requirements:     []scouting.Requirement{note, req1},
	}
	advs := []scouting.Adventure{newAdventure(140, "My Family", "My Family", 0.5)}

	a := newScoutInput("Alice", "Apple", 1, scouting.RankRequirements{}, advs, map[int]scouting.AdventureRequirements{140: advReqs})
	r := BuildReport("Webelos", "1", "Webelos", []ScoutInput{a})

	sheet := findAdventureSheet(r.Adventures, 140)
	if sheet == nil {
		t.Fatalf("no AdventureSheet for id=140")
	}
	if got, want := len(sheet.Rows), 1; got != want {
		t.Fatalf("len(Rows) = %d, want %d (note rows should be skipped)", got, want)
	}
	if strings.Contains(sheet.Rows[0].Label, "Note:") {
		t.Errorf("row 0 label = %q, should not be the Note row", sheet.Rows[0].Label)
	}
}
