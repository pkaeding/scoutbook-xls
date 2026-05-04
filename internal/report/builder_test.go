package report

import (
	"slices"
	"strings"
	"testing"

	"github.com/pkaeding/scoutbook-xls/internal/scouting"
)

// --- test helpers ---------------------------------------------------------

func strPtr(s string) *string { return &s }

// newRankReq builds a leaf RankRequirement matching a rank-sheet row. The
// requirement is assumed to link to a single adventure by id/name; it is NOT
// marked electiveAdventure unless the test overrides afterwards.
func newRankReq(number, name string, linkedAdvId int, linkedAdvName string, pct float64) scouting.RankRequirement {
	return scouting.RankRequirement{
		RequirementNumber: number,
		Name:              name,
		PercentCompleted:  pct,
		LinkedAdventureId: &linkedAdvId,
		LinkedAdventure: scouting.LinkedAdventure{
			Id:               linkedAdvId,
			Name:             linkedAdvName,
			PercentCompleted: pct,
		},
	}
}

// newAdventure builds a scout-level Adventure entry (the filtered list item).
func newAdventure(id int, name, shortName string, pct float64) scouting.Adventure {
	return scouting.Adventure{
		AdventureId:      id,
		AdventureName:    name,
		ShortName:        shortName,
		RankId:           11, // Webelos by default; tests that care can override
		PercentCompleted: pct,
	}
}

// newReq builds a Requirement on an adventure detail with the given number,
// sort/display order, and optional date-completed pointer.
func newReq(number, name string, dateCompleted *string) scouting.Requirement {
	return scouting.Requirement{
		RequirementNumber: number,
		RequirementName:   name,
		DateCompleted:     dateCompleted,
		IsCompleted:       dateCompleted != nil,
	}
}

// newAdventureReqs builds an AdventureRequirements detail for a single
// adventure with the provided leaf requirements.
func newAdventureReqs(id int, name string, pct float64, reqs ...scouting.Requirement) scouting.AdventureRequirements {
	return scouting.AdventureRequirements{
		AdventureId:      id,
		AdventureName:    name,
		PercentCompleted: pct,
		Requirements:     reqs,
	}
}

// newScoutInput is a concise constructor for ScoutInput. The userId defaults
// to a non-zero hash of the first name so the default-constructed scout is
// considered "resolved"; tests that want an unresolved scout pass UserId=0
// by constructing the struct literal directly.
func newScoutInput(first, last string, userId int, rankReqs scouting.RankRequirements, advs []scouting.Adventure, advReqs map[int]scouting.AdventureRequirements) ScoutInput {
	return ScoutInput{
		FirstName:     first,
		LastName:      last,
		FullName:      first + " " + last,
		UserId:        userId,
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
		if sheets[i].AdventureId == id {
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
		Id:   11,
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
	// One scout with UserId=0 (unresolved); should be dropped and a
	// warning should mention their name.
	good := newScoutInput("Alice", "Apple", 1, scouting.RankRequirements{}, nil, nil)
	bad := ScoutInput{
		FirstName: "Zane",
		LastName:  "Zephyr",
		FullName:  "Zane Zephyr",
		UserId:    0,
	}

	r := BuildReport("Webelos", "1", "Webelos", []ScoutInput{good, bad})

	for _, s := range r.Scouts {
		if s.UserId == 0 {
			t.Errorf("Scouts unexpectedly contains scout with UserId=0: %+v", s)
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
