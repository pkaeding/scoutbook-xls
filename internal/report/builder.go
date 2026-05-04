package report

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/pkaeding/scoutbook-xls/internal/scouting"
)

// ScoutInput is the per-scout input to BuildReport.
type ScoutInput struct {
	FirstName     string
	LastName      string
	FullName      string
	UserID        int
	RankReqs      scouting.RankRequirements
	Adventures    []scouting.Adventure
	AdventureReqs map[int]scouting.AdventureRequirements
}

// ScoutCol is a per-scout column in the report summary sheet.
type ScoutCol struct {
	FirstName string
	FullName  string
	UserID    int
}

// SummaryRowKind distinguishes the kind of a SummaryRow.
type SummaryRowKind int

const (
	SummaryRowSectionHeader SummaryRowKind = iota
	SummaryRowRankReq
	SummaryRowAdventure
)

// SummaryRow is one row of the summary sheet.
//
// For SummaryRowRankReq: Dates holds each scout's completion date (nil if
// not completed). Percents is unused in the renderer but populated for
// callers that want it. Rank requirements are binary (done/not-done) so
// showing a date is more informative than 0/100%.
//
// For SummaryRowAdventure: Percents holds each scout's percent-completed on
// the adventure. Dates is unused.
//
// For SummaryRowSectionHeader: both slices are empty.
type SummaryRow struct {
	Kind         SummaryRowKind
	Label        string
	Percents     []float64
	Dates        []*string
	AllCompleted bool
}

// AdventureRow is a single requirement row within an AdventureSheet.
type AdventureRow struct {
	Label          string
	DatesCompleted []*string
	AllCompleted   bool
}

// AdventureSheet is a per-adventure sheet in the report.
type AdventureSheet struct {
	AdventureID int
	Name        string
	ShortName   string
	Rows        []AdventureRow
	OverallPcts []float64
}

// ReportModel is the full report returned by BuildReport.
type ReportModel struct {
	DenLabel    string
	RankName    string
	Scouts      []ScoutCol
	SummaryRows []SummaryRow
	Adventures  []AdventureSheet
	Warnings    []string
}

// BuildReport turns a slice of ScoutInput into a ReportModel, ready for rendering.
func BuildReport(denType, denNumber, rankName string, scouts []ScoutInput) ReportModel {
	model := ReportModel{
		DenLabel: fmt.Sprintf("%s %s", denType, denNumber),
		RankName: rankName,
	}

	// Filter out unresolved scouts (UserID == 0) and record warnings.
	resolved := make([]ScoutInput, 0, len(scouts))
	for _, s := range scouts {
		if s.UserID == 0 {
			model.Warnings = append(model.Warnings,
				fmt.Sprintf("Skipping scout %q: unresolved userID", s.FullName))
			continue
		}
		resolved = append(resolved, s)
	}

	// Stable sort by FirstName, case-insensitive.
	sort.SliceStable(resolved, func(i, j int) bool {
		return strings.ToLower(resolved[i].FirstName) < strings.ToLower(resolved[j].FirstName)
	})

	// Build scout columns.
	model.Scouts = make([]ScoutCol, len(resolved))
	for i, s := range resolved {
		model.Scouts[i] = ScoutCol{
			FirstName: s.FirstName,
			FullName:  s.FullName,
			UserID:    s.UserID,
		}
	}

	// Pick the first resolved scout with populated RankReqs as the template for
	// the ordered rank-requirement list. All scouts in a den share the same rank.
	var rankTemplate []scouting.RankRequirement
	for _, s := range resolved {
		if len(s.RankReqs.Requirements) > 0 {
			rankTemplate = s.RankReqs.Requirements
			break
		}
	}

	// Keep only leaf requirements — ones with a non-empty requirementNumber.
	// Parent "group" rows like "Complete the six required adventures:" have
	// requirementNumber == "" and a non-empty childrenRequired; we skip those.
	leafReqs := filterLeafRankReqs(rankTemplate)
	sortRankReqs(leafReqs)

	// Summary rows: Rank Requirements section.
	model.SummaryRows = append(model.SummaryRows, SummaryRow{
		Kind:  SummaryRowSectionHeader,
		Label: "Rank Requirements",
	})
	for _, tmpl := range leafReqs {
		row := SummaryRow{
			Kind:     SummaryRowRankReq,
			Label:    fmt.Sprintf("%s — %s", tmpl.RequirementNumber, tmpl.Name),
			Percents: make([]float64, len(resolved)),
			Dates:    make([]*string, len(resolved)),
		}
		allDone := len(resolved) > 0
		for i, s := range resolved {
			for _, r := range s.RankReqs.Requirements {
				if r.RequirementNumber == tmpl.RequirementNumber {
					row.Percents[i] = r.PercentCompleted
					if r.Completed || r.PercentCompleted >= 1.0 {
						row.Dates[i] = r.DateCompleted
					} else {
						allDone = false
					}
					break
				}
			}
			if row.Dates[i] == nil {
				allDone = false
			}
		}
		row.AllCompleted = allDone
		model.SummaryRows = append(model.SummaryRows, row)
	}

	// Determine which adventures were started by any resolved scout.
	startedAdv := map[int]bool{}
	advMetaByID := map[int]struct {
		name      string
		shortName string
	}{}
	var firstSeen []int // order we first saw each adventure id
	for _, s := range resolved {
		for _, a := range s.Adventures {
			if _, seen := advMetaByID[a.AdventureID]; !seen {
				advMetaByID[a.AdventureID] = struct {
					name      string
					shortName string
				}{name: a.AdventureName, shortName: a.ShortName}
				firstSeen = append(firstSeen, a.AdventureID)
			}
			if a.PercentCompleted > 0 {
				startedAdv[a.AdventureID] = true
			}
		}
	}

	// Build the ordered adventure list for the summary + sheets:
	//   1. Adventures linked directly to a rank requirement, in rank-req order
	//      (only ones we have metadata for — i.e., at least one scout has
	//      them in their list).
	//   2. Then any other started adventures — falls through to first-seen order.
	orderedAdvIDs := orderedAdventureIDs(leafReqs, advMetaByID, startedAdv, firstSeen)

	// Adventures section header.
	model.SummaryRows = append(model.SummaryRows, SummaryRow{
		Kind:  SummaryRowSectionHeader,
		Label: "Adventures",
	})
	for _, advID := range orderedAdvIDs {
		meta := advMetaByID[advID]
		row := SummaryRow{
			Kind:     SummaryRowAdventure,
			Label:    meta.name,
			Percents: make([]float64, len(resolved)),
		}
		allDone := len(resolved) > 0
		for i, s := range resolved {
			pct := 0.0
			for _, a := range s.Adventures {
				if a.AdventureID == advID {
					pct = a.PercentCompleted
					break
				}
			}
			row.Percents[i] = pct
			if pct < 1.0 {
				allDone = false
			}
		}
		row.AllCompleted = allDone
		model.SummaryRows = append(model.SummaryRows, row)
	}

	// Per-adventure sheets, same order as the summary.
	for _, advID := range orderedAdvIDs {
		meta := advMetaByID[advID]
		sheet := AdventureSheet{
			AdventureID: advID,
			Name:        meta.name,
			ShortName:   meta.shortName,
			OverallPcts: make([]float64, len(resolved)),
		}

		// Collect the union of requirements across scouts' detail for this
		// adventure. Skip rows where RequirementNumber == "" — those are
		// "Note:" blurbs the API emits with the numbered requirements.
		reqByNum := map[string]scouting.Requirement{}
		var reqNumbers []string
		for _, s := range resolved {
			detail, ok := s.AdventureReqs[advID]
			if !ok {
				continue
			}
			for _, r := range detail.Requirements {
				if r.RequirementNumber == "" {
					continue
				}
				if _, seen := reqByNum[r.RequirementNumber]; !seen {
					reqByNum[r.RequirementNumber] = r
					reqNumbers = append(reqNumbers, r.RequirementNumber)
				}
			}
		}
		// Order by the requirement's sortOrder (float).
		sort.SliceStable(reqNumbers, func(i, j int) bool {
			return reqByNum[reqNumbers[i]].SortOrder < reqByNum[reqNumbers[j]].SortOrder
		})

		// Per-scout overall percent for this adventure.
		for i, s := range resolved {
			for _, a := range s.Adventures {
				if a.AdventureID == advID {
					sheet.OverallPcts[i] = a.PercentCompleted
					break
				}
			}
		}

		// Build requirement rows: label is "<number> — <requirement text>".
		// Prefer the long requirementName; fall back to shortName if empty.
		for _, num := range reqNumbers {
			tmpl := reqByNum[num]
			text := tmpl.RequirementName
			if text == "" {
				text = tmpl.ShortName
			}
			row := AdventureRow{
				Label:          fmt.Sprintf("%s — %s", num, text),
				DatesCompleted: make([]*string, len(resolved)),
			}
			allDone := len(resolved) > 0
			for i, s := range resolved {
				detail, ok := s.AdventureReqs[advID]
				if !ok {
					allDone = false
					continue
				}
				found := false
				for _, r := range detail.Requirements {
					if r.RequirementNumber == num {
						row.DatesCompleted[i] = r.DateCompleted
						found = true
						break
					}
				}
				if !found || row.DatesCompleted[i] == nil {
					allDone = false
				}
			}
			row.AllCompleted = allDone
			sheet.Rows = append(sheet.Rows, row)
		}

		model.Adventures = append(model.Adventures, sheet)
	}

	return model
}

// filterLeafRankReqs returns only the rank requirements with a non-empty
// RequirementNumber. Parent "group" requirements (e.g. "Complete the six
// required adventures:") have empty RequirementNumber and ChildrenRequired
// populated, so they're excluded from the per-scout summary table.
func filterLeafRankReqs(reqs []scouting.RankRequirement) []scouting.RankRequirement {
	out := make([]scouting.RankRequirement, 0, len(reqs))
	for _, r := range reqs {
		if r.RequirementNumber == "" {
			continue
		}
		out = append(out, r)
	}
	return out
}

// sortRankReqs sorts rank requirements by their dotted SortOrder string
// ("1.1", "1.2", ..., "2.1", "2.2") as a tuple of ints. This yields the
// natural 1a/1b/1c/1d/1e/1f/2a/2b order Scouting uses to number Webelos
// requirements — rather than the interleaved order the API returns.
func sortRankReqs(reqs []scouting.RankRequirement) {
	sort.SliceStable(reqs, func(i, j int) bool {
		return sortOrderLess(reqs[i].SortOrder, reqs[j].SortOrder)
	})
}

// sortOrderLess compares two dotted version-style sort keys ("1.2" < "1.10"
// < "2.1"). Falls back to string compare if a segment isn't numeric.
func sortOrderLess(a, b string) bool {
	aParts := strings.Split(a, ".")
	bParts := strings.Split(b, ".")
	for i := 0; i < len(aParts) && i < len(bParts); i++ {
		ai, aerr := strconv.Atoi(aParts[i])
		bi, berr := strconv.Atoi(bParts[i])
		if aerr != nil || berr != nil {
			// Fall back to string compare on this segment.
			if aParts[i] != bParts[i] {
				return aParts[i] < bParts[i]
			}
			continue
		}
		if ai != bi {
			return ai < bi
		}
	}
	return len(aParts) < len(bParts)
}

// orderedAdventureIDs returns the ordered list of adventure IDs to include
// in the summary and the per-adventure sheets.
//
// Ordering rules:
//  1. Adventures linked directly to a rank requirement (by
//     LinkedAdventureID), in rank-req order — regardless of whether any
//     scout has started them. This pins required adventures to the top in
//     their natural order (1a/1b/1c/...).
//  2. Then any other adventures a scout has started, in the order we first
//     encountered them across the scouts' adventure lists. This covers the
//     elective slots — we only include electives at least one scout is
//     actively working on.
func orderedAdventureIDs(
	rankReqs []scouting.RankRequirement,
	advMetaByID map[int]struct {
		name      string
		shortName string
	},
	startedAdv map[int]bool,
	firstSeenAdv []int,
) []int {
	var out []int
	added := map[int]bool{}

	// Pass 1: rank-req-linked adventures, in rank-req order. Required
	// adventures are included even if nobody has started them — they're the
	// rank's backbone and a 0% row is still meaningful. Skipped only when we
	// have no metadata for the adventure (i.e., no scout has it in their
	// list), since we'd have nothing to render.
	for _, r := range rankReqs {
		if r.LinkedAdventureID == nil {
			continue
		}
		id := *r.LinkedAdventureID
		if added[id] {
			continue
		}
		if _, ok := advMetaByID[id]; !ok {
			continue
		}
		out = append(out, id)
		added[id] = true
	}

	// Pass 2: other started adventures, in first-seen order.
	for _, advID := range firstSeenAdv {
		if added[advID] || !startedAdv[advID] {
			continue
		}
		out = append(out, advID)
		added[advID] = true
	}

	return out
}
