package blackpug

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/pkaeding/scoutbook-xls/internal/scouting"
)

// Row types for whole-award completion rows. Black Pug's export has only
// ever been seen with requirement rows for awards; this is the assumed name
// of the completion row, used only to double-check the result.
const TypeAward = "Award"

// catalogItem is a merit badge or award in the catalog.
type catalogItem struct {
	ID       int
	Name     string
	Versions []catalogVersion
}

type catalogVersion struct {
	ID      int
	Version string
	Expired bool
}

// plannedReq is one requirement to record: its ID and the date (YYYY-MM-DD)
// the scout completed it.
type plannedReq struct {
	ID   int
	Date string
}

// plannedScout is one scout's batch for one item.
type plannedScout struct {
	UserID    int
	VersionID int
	Reqs      []plannedReq
}

// kind captures everything that differs between recording merit badges and
// recording awards; the matching and safety logic is shared.
type kind struct {
	// noun is used in messages ("merit badge", "award").
	noun string
	// reqType and doneType are the export's row types for one requirement and
	// for the whole item. doneType may be unused by an export.
	reqType, doneType string
	// fuzzyNames: Black Pug and Scouting.org name awards differently
	// ("Kayaking BSA" vs "Kayaking Award"), and Black Pug exports awards
	// without a real version ("0000").
	fuzzyNames bool

	catalog      func(ctx context.Context, c *scouting.Client, org string) ([]catalogItem, error)
	requirements func(ctx context.Context, c *scouting.Client, org string, id, versionID int) (scouting.MeritBadgeRequirements, error)
	progress     func(ctx context.Context, c *scouting.Client, org string, id int, members []string) ([]scouting.AdvancementProgress, error)
	post         func(ctx context.Context, c *scouting.Client, org string, id int, scouts []plannedScout, leaderUID int, today string) ([]scouting.AdvancementScoutResult, error)
}

var meritBadgeKind = kind{
	noun:     "merit badge",
	reqType:  TypeMeritBadgeRequirement,
	doneType: TypeMeritBadge,
	catalog: func(ctx context.Context, c *scouting.Client, org string) ([]catalogItem, error) {
		badges, err := scouting.FetchMeritBadges(ctx, c, org)
		if err != nil {
			return nil, err
		}
		out := make([]catalogItem, 0, len(badges))
		for _, b := range badges {
			item := catalogItem{ID: b.ID, Name: b.Name}
			for _, v := range b.Versions {
				id, err := strconv.Atoi(v.VersionID)
				if err != nil || id == 0 {
					continue // unusable: can't fetch requirements or post against it
				}
				item.Versions = append(item.Versions, catalogVersion{ID: id, Version: v.Version})
			}
			out = append(out, item)
		}
		return out, nil
	},
	requirements: scouting.FetchMeritBadgeRequirements,
	progress:     scouting.FetchMeritBadgeProgress,
	post: func(ctx context.Context, c *scouting.Client, org string, id int, scouts []plannedScout, leaderUID int, today string) ([]scouting.AdvancementScoutResult, error) {
		body := make([]scouting.MeritBadgeScoutEntry, 0, len(scouts))
		for _, s := range scouts {
			e := scouting.MeritBadgeScoutEntry{OrganizationGUID: org, UserID: s.UserID, VersionID: s.VersionID}
			for _, r := range s.Reqs {
				e.Requirements = append(e.Requirements, scouting.MeritBadgeRequirementEntry{
					ID: r.ID, Started: true, DateCompleted: r.Date, MarkedCompletedDate: today, Completed: true,
					Approved: true, LeaderApprovedUserID: leaderUID, LeaderApprovedDate: today,
				})
			}
			body = append(body, e)
		}
		return scouting.PostMeritBadgeRequirements(ctx, c, org, id, body)
	},
}

var awardKind = kind{
	noun:       "award",
	reqType:    TypeAwardRequirement,
	doneType:   TypeAward,
	fuzzyNames: true,
	catalog: func(ctx context.Context, c *scouting.Client, org string) ([]catalogItem, error) {
		awards, err := scouting.FetchAwards(ctx, c, org)
		if err != nil {
			return nil, err
		}
		out := make([]catalogItem, 0, len(awards))
		for _, a := range awards {
			item := catalogItem{ID: a.ID, Name: a.Name}
			for _, v := range a.Versions {
				item.Versions = append(item.Versions, catalogVersion{ID: v.VersionID, Version: v.Version, Expired: v.Expired})
			}
			out = append(out, item)
		}
		return out, nil
	},
	requirements: scouting.FetchAwardRequirements,
	progress:     scouting.FetchAwardProgress,
	post: func(ctx context.Context, c *scouting.Client, org string, id int, scouts []plannedScout, _ int, today string) ([]scouting.AdvancementScoutResult, error) {
		body := make([]scouting.AwardScoutEntry, 0, len(scouts))
		for _, s := range scouts {
			e := scouting.AwardScoutEntry{OrganizationGUID: org, UserID: s.UserID, VersionID: s.VersionID}
			for _, r := range s.Reqs {
				e.Requirements = append(e.Requirements, scouting.AwardRequirementEntry{
					ID: r.ID, DateEarned: r.Date, Started: true, Completed: true,
					MarkedCompletedDate: today, Approved: true, LeaderApprovedDate: today,
				})
			}
			body = append(body, e)
		}
		return scouting.PostAwardRequirements(ctx, c, org, id, body)
	},
}

var kinds = []kind{meritBadgeKind, awardKind}

// handledType reports whether some kind imports rows of this type.
func handledType(t string) bool {
	for _, k := range kinds {
		if t == k.reqType || t == k.doneType {
			return true
		}
	}
	return false
}

// nameCore strips the generic suffixes Black Pug and Scouting.org disagree
// about ("Kayaking BSA", "Kayaking Award") so the rest can be compared.
func nameCore(name string) string {
	words := strings.Fields(strings.ToLower(name))
	for len(words) > 1 {
		switch words[len(words)-1] {
		case "bsa", "award", "patch", "emblem":
			words = words[:len(words)-1]
			continue
		}
		break
	}
	return strings.Join(words, " ")
}

// resolveItem finds the catalog item for an export name. An exact
// (case-insensitive) name always wins; fuzzy kinds then fall back to the
// name core, which must be unambiguous.
func (k kind) resolveItem(catalog []catalogItem, name string) (catalogItem, string) {
	for _, it := range catalog {
		if strings.EqualFold(strings.TrimSpace(it.Name), name) {
			return it, ""
		}
	}
	if k.fuzzyNames {
		var hits []catalogItem
		for _, it := range catalog {
			if nameCore(it.Name) == nameCore(name) {
				hits = append(hits, it)
			}
		}
		switch len(hits) {
		case 1:
			return hits[0], ""
		case 0:
		default:
			names := make([]string, 0, len(hits))
			for _, h := range hits {
				names = append(names, h.Name)
			}
			return catalogItem{}, fmt.Sprintf("name is ambiguous in the catalog (%s)", strings.Join(names, ", "))
		}
	}
	return catalogItem{}, fmt.Sprintf("no %s with this name in the catalog", k.noun)
}

// resolveVersion picks the catalog version for the export's version column.
// Fuzzy kinds treat an empty or all-zero version ("0000") as "the current
// version", which must be unique.
func (k kind) resolveVersion(it catalogItem, version string) (catalogVersion, string) {
	for _, v := range it.Versions {
		if v.Version == version {
			return v, ""
		}
	}
	if k.fuzzyNames && strings.Trim(version, "0") == "" {
		var live []catalogVersion
		for _, v := range it.Versions {
			if !v.Expired {
				live = append(live, v)
			}
		}
		if len(live) == 1 {
			return live[0], ""
		}
		return catalogVersion{}, fmt.Sprintf("export has no version for %s and the catalog has %d current versions", it.Name, len(live))
	}
	return catalogVersion{}, fmt.Sprintf("catalog has no %s version %q", it.Name, version)
}
