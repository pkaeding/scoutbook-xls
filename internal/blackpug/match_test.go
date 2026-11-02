package blackpug

import (
	"encoding/json"
	"os"
	"strconv"
	"testing"

	"github.com/pkaeding/scoutbook-xls/internal/scouting"
)

// Every spelling of a requirement number seen in a real camp export.
func TestNormalizeRequirement(t *testing.T) {
	tests := []struct{ in, want string }{
		{"3a", "3a"},
		{"3(a)", "3a"},
		{"3a.", "3a"},
		{"10.", "10"},
		{"3j.", "3j"},
		{"1(b)(10)", "1b10"},
		{"8a(2)", "8a2"},
		{"1g[2]", "1g2"},
		{"5. Opt A (6)(b)", "5opta6b"},
		{"2 Option A (10)", "2opta10"},
		{"2 Option B(13)", "2optb13"},
		{"  2B  ", "2b"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := NormalizeRequirement(tt.in); got != tt.want {
			t.Errorf("NormalizeRequirement(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
	// Option-style numbers must agree across the spellings in play.
	if NormalizeRequirement("2 Option A (4)") != NormalizeRequirement("2. Opt A (4)") {
		t.Error("Option/Opt spellings should normalize equal")
	}
}

func loadLeatherwork(t *testing.T) []scouting.MeritBadgeRequirement {
	t.Helper()
	b, err := os.ReadFile(fixturePath(t, "mb_71_requirements_v254.json"))
	if err != nil {
		t.Fatal(err)
	}
	var raw struct {
		Requirements []struct {
			ID                  string `json:"id"`
			ListNumber          string `json:"listNumber"`
			RequirementNumber   string `json:"requirementNumber"`
			SortOrder           string `json:"sortOrder"`
			ParentRequirementID string `json:"parentRequirementId"`
		} `json:"requirements"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	var out []scouting.MeritBadgeRequirement
	for _, r := range raw.Requirements {
		id, _ := strconv.Atoi(r.ID)
		pid, _ := strconv.Atoi(r.ParentRequirementID) // "" for top-level rows
		out = append(out, scouting.MeritBadgeRequirement{
			ID: id, ListNumber: r.ListNumber, RequirementNumber: r.RequirementNumber,
			SortOrder: r.SortOrder, ParentRequirementID: pid,
		})
	}
	return out
}

func TestRequirementIndex_LeatherworkCatalog(t *testing.T) {
	// Expected IDs come from the requirements the SPA itself posted for this
	// badge (Leatherwork #1a..#5a).
	idx := NewRequirementIndex(loadLeatherwork(t))
	want := map[string]int{
		"1a": 7230, "1b": 7231, "2a": 7211, "2d": 7214, "3a": 7216, "3f": 7221, "4": 7232, "5a": 7225,
		"3(f)": 7221, "3f.": 7221,
		// Parent rows have no requirementNumber; reachable via list number.
		"2": 7210, "3": 7215,
	}
	for in, id := range want {
		got, ok := idx.Lookup(in)
		if !ok || got != id {
			t.Errorf("Lookup(%q) = %d, %v; want %d", in, got, ok, id)
		}
	}
	if !idx.IsParent(7210) || !idx.IsParent(7229) || idx.IsParent(7211) || idx.IsParent(7232) {
		t.Error("IsParent: 7210/7229 are parents; 7211 and 4 (7232) are leaves")
	}
	if !idx.HasDescendantIn(7210, map[int]bool{7213: true}) || idx.HasDescendantIn(7210, map[int]bool{7216: true}) {
		t.Error("HasDescendantIn should match only 7210's own sub-requirements")
	}
	for _, in := range []string{"9z", "", "3i", "6"} {
		if id, ok := idx.Lookup(in); ok {
			t.Errorf("Lookup(%q) = %d, want no match", in, id)
		}
	}
}

func TestRequirementIndex_NestedAndOptionNumbering(t *testing.T) {
	reqs := []scouting.MeritBadgeRequirement{
		{ID: 1, ListNumber: "1.", SortOrder: "1"},
		{ID: 2, ListNumber: "(b)", ParentRequirementID: 1, SortOrder: "1.2"},
		{ID: 3, ListNumber: "(1)", ParentRequirementID: 2, SortOrder: "1.2.1"},
		{ID: 4, ListNumber: "(10)", ParentRequirementID: 2, SortOrder: "1.2.10"},
		{ID: 5, ListNumber: "(2)", ParentRequirementID: 2, SortOrder: "1.2.2"},
		{ID: 6, RequirementNumber: "2 Option A (4)", ListNumber: "(4)", SortOrder: "2.1.4"},
	}
	idx := NewRequirementIndex(reqs)
	for in, id := range map[string]int{"1(b)(1)": 3, "1b10": 4, "1.b.2": 5, "2. Opt A (4)": 6} {
		if got, ok := idx.Lookup(in); !ok || got != id {
			t.Errorf("Lookup(%q) = %d, %v; want %d", in, got, ok, id)
		}
	}

	ids := []int{4, 5, 3}
	idx.SortByCatalogOrder(ids)
	if ids[0] != 3 || ids[1] != 5 || ids[2] != 4 {
		t.Errorf("catalog order = %v, want [3 5 4] (1.2.10 sorts after 1.2.2)", ids)
	}
}

func TestRequirementIndex_AmbiguousIsNeverGuessed(t *testing.T) {
	reqs := []scouting.MeritBadgeRequirement{
		{ID: 1, RequirementNumber: "4a"},
		{ID: 2, RequirementNumber: "4(a)"}, // same key as ID 1
	}
	if id, ok := NewRequirementIndex(reqs).Lookup("4a"); ok {
		t.Errorf("ambiguous Lookup returned %d; want no match", id)
	}
}

func TestRequirementIndex_CyclicParentsTerminate(t *testing.T) {
	reqs := []scouting.MeritBadgeRequirement{
		{ID: 1, ListNumber: "(a)", ParentRequirementID: 2},
		{ID: 2, ListNumber: "(b)", ParentRequirementID: 1},
	}
	NewRequirementIndex(reqs) // must return
}
