package blackpug

import (
	"strings"
	"testing"
)

func TestNameCore(t *testing.T) {
	for in, want := range map[string]string{
		"Kayaking BSA":    "kayaking",
		"Kayaking Award":  "kayaking",
		"Mile Swim Award": "mile swim",
		"Award":           "award", // never strip a name down to nothing
		"Archery":         "archery",
	} {
		if got := nameCore(in); got != want {
			t.Errorf("nameCore(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAwardResolveItem(t *testing.T) {
	cat := []catalogItem{{ID: 101, Name: "Kayaking Award"}, {ID: 102, Name: "Mile Swim Award"}}
	if it, why := awardKind.resolveItem(cat, "Kayaking BSA"); why != "" || it.ID != 101 {
		t.Errorf("Kayaking BSA -> %+v, %q", it, why)
	}
	if it, why := awardKind.resolveItem(cat, "kayaking award"); why != "" || it.ID != 101 {
		t.Errorf("exact (case-insensitive) -> %+v, %q", it, why)
	}
	if _, why := awardKind.resolveItem(cat, "Warp Theory"); why == "" {
		t.Error("unknown award should not resolve")
	}

	amb := append(cat, catalogItem{ID: 103, Name: "Kayaking Patch"})
	if _, why := awardKind.resolveItem(amb, "Kayaking BSA"); !strings.Contains(why, "ambiguous") {
		t.Errorf("ambiguous core must not be guessed: %q", why)
	}
	// ...but an exact name still wins over the ambiguity.
	if it, why := awardKind.resolveItem(amb, "Kayaking Patch"); why != "" || it.ID != 103 {
		t.Errorf("exact in ambiguous catalog -> %+v, %q", it, why)
	}

	// Merit badges never fuzzy-match: "Kayaking BSA" is not "Kayaking".
	if _, why := meritBadgeKind.resolveItem([]catalogItem{{ID: 1, Name: "Kayaking"}}, "Kayaking BSA"); why == "" {
		t.Error("merit badge names must match exactly")
	}
}

func TestAwardResolveVersion(t *testing.T) {
	one := catalogItem{Name: "Kayaking Award", Versions: []catalogVersion{{ID: 414, Version: "2002"}}}
	if v, why := awardKind.resolveVersion(one, "0000"); why != "" || v.ID != 414 {
		t.Errorf("0000 with one version -> %+v, %q", v, why)
	}
	if v, why := awardKind.resolveVersion(one, "2002"); why != "" || v.ID != 414 {
		t.Errorf("exact -> %+v, %q", v, why)
	}
	if _, why := awardKind.resolveVersion(one, "1999"); why == "" {
		t.Error("a specific unknown version must not resolve")
	}

	// Several versions, one still current: that's the one.
	multi := catalogItem{Name: "X", Versions: []catalogVersion{{ID: 1, Version: "2010", Expired: true}, {ID: 2, Version: "2020"}}}
	if v, why := awardKind.resolveVersion(multi, "0000"); why != "" || v.ID != 2 {
		t.Errorf("0000 with one live version -> %+v, %q", v, why)
	}
	// Two current versions: ambiguous.
	two := catalogItem{Name: "X", Versions: []catalogVersion{{ID: 1, Version: "2010"}, {ID: 2, Version: "2020"}}}
	if _, why := awardKind.resolveVersion(two, "0000"); why == "" {
		t.Error("0000 with two live versions must not be guessed")
	}
	// Merit badges don't get this leniency.
	if _, why := meritBadgeKind.resolveVersion(one, "0000"); why == "" {
		t.Error("merit badge version 0000 must not resolve")
	}
}
