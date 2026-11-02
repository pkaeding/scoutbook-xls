package blackpug

import (
	"sort"
	"strconv"
	"strings"

	"github.com/pkaeding/scoutbook-xls/internal/scouting"
)

// NormalizeRequirement reduces a requirement number to a comparison key.
// Black Pug and Scouting.org spell the same requirement many ways — "3a",
// "3(a)", "3a.", "1g[2]", "5. Opt A (1)", "2 Option B(13)" — so the key is
// lower-case alphanumerics only, with "option" folded to "opt".
func NormalizeRequirement(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "option", "opt")
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// RequirementIndex finds catalog requirements by Black Pug requirement
// number.
type RequirementIndex struct {
	byKey     map[string]int
	ambiguous map[string]bool
	sortKey   map[int]string
	children  map[int][]int // requirement ID -> direct children
}

// NewRequirementIndex indexes a badge version's requirements. Each is
// reachable by its own RequirementNumber (when set) and by the
// concatenation of list numbers up its parent chain ("2." + "(a)" → "2a"),
// which is the only handle parent rows and unnumbered children have.
func NewRequirementIndex(reqs []scouting.MeritBadgeRequirement) *RequirementIndex {
	idx := &RequirementIndex{
		byKey:     map[string]int{},
		ambiguous: map[string]bool{},
		sortKey:   map[int]string{},
		children:  map[int][]int{},
	}
	byID := make(map[int]scouting.MeritBadgeRequirement, len(reqs))
	for _, r := range reqs {
		byID[r.ID] = r
		idx.sortKey[r.ID] = r.SortOrder
	}
	for _, r := range reqs {
		if _, ok := byID[r.ParentRequirementID]; ok && r.ParentRequirementID != 0 {
			idx.children[r.ParentRequirementID] = append(idx.children[r.ParentRequirementID], r.ID)
		}
	}
	chain := func(r scouting.MeritBadgeRequirement) string {
		var parts []string
		for hops := 0; hops < 10; hops++ { // bounded: the API is not trusted to be acyclic
			parts = append([]string{r.ListNumber}, parts...)
			parent, ok := byID[r.ParentRequirementID]
			if r.ParentRequirementID == 0 || !ok {
				break
			}
			r = parent
		}
		return strings.Join(parts, "")
	}
	for _, r := range reqs {
		keys := map[string]bool{}
		if k := NormalizeRequirement(r.RequirementNumber); k != "" {
			keys[k] = true
		}
		if k := NormalizeRequirement(chain(r)); k != "" {
			keys[k] = true
		}
		for k := range keys {
			if prev, dup := idx.byKey[k]; dup && prev != r.ID {
				idx.ambiguous[k] = true
				continue
			}
			idx.byKey[k] = r.ID
		}
	}
	return idx
}

// Lookup returns the requirement ID for a Black Pug requirement number. ok
// is false when nothing matches or the number is ambiguous; an ambiguous
// match is never guessed.
func (x *RequirementIndex) Lookup(number string) (id int, ok bool) {
	k := NormalizeRequirement(number)
	if k == "" || x.ambiguous[k] {
		return 0, false
	}
	id, ok = x.byKey[k]
	return id, ok
}

// IsParent reports whether the requirement has sub-requirements. The SPA
// records completion on sub-requirements only — it never sent a parent row
// when it entered a badge by hand, and existing records show parents without
// a completion date even on awarded badges — so parents are not written.
func (x *RequirementIndex) IsParent(id int) bool { return len(x.children[id]) > 0 }

// HasDescendantIn reports whether any sub-requirement, at any depth, of the
// parent is in the set.
func (x *RequirementIndex) HasDescendantIn(parent int, set map[int]bool) bool {
	seen := map[int]bool{}
	var walk func(id int) bool
	walk = func(id int) bool {
		if seen[id] {
			return false
		}
		seen[id] = true
		for _, c := range x.children[id] {
			if set[c] || walk(c) {
				return true
			}
		}
		return false
	}
	return walk(parent)
}

// SortByCatalogOrder orders requirement IDs the way the badge lists them
// (sortOrder is dotted, e.g. "2.10" follows "2.9").
func (x *RequirementIndex) SortByCatalogOrder(ids []int) {
	sort.SliceStable(ids, func(i, j int) bool {
		return lessDotted(x.sortKey[ids[i]], x.sortKey[ids[j]])
	})
}

func lessDotted(a, b string) bool {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		ai, aerr := strconv.Atoi(as[i])
		bi, berr := strconv.Atoi(bs[i])
		if aerr != nil || berr != nil {
			if as[i] != bs[i] {
				return as[i] < bs[i]
			}
			continue
		}
		if ai != bi {
			return ai < bi
		}
	}
	return len(as) < len(bs)
}
