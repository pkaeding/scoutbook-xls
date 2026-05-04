package report

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/pkaeding/scoutbook-xls/internal/scouting"
)

// ScoutInput is the per-scout input to BuildReport.
type ScoutInput struct {
	FirstName     string
	LastName      string
	FullName      string
	UserId        int
	RankReqs      scouting.RankRequirements
	Adventures    []scouting.Adventure
	AdventureReqs map[int]scouting.AdventureRequirements
}

// ScoutCol is a per-scout column in the report summary sheet.
type ScoutCol struct {
	FirstName string
	FullName  string
	UserId    int
}

// SummaryRowKind distinguishes the kind of a SummaryRow.
type SummaryRowKind int

const (
	SummaryRowSectionHeader SummaryRowKind = iota
	SummaryRowRankReq
	SummaryRowAdventure
)

// SummaryRow is one row of the summary sheet.
type SummaryRow struct {
	Kind     SummaryRowKind
	Label    string
	Percents []float64
}

// AdventureRow is a single requirement row within an AdventureSheet.
type AdventureRow struct {
	Label          string
	DatesCompleted []*string
}

// AdventureSheet is a per-adventure sheet in the report.
type AdventureSheet struct {
	AdventureId int
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

	// Filter out unresolved scouts (UserId == 0) and record warnings.
	resolved := make([]ScoutInput, 0, len(scouts))
	for _, s := range scouts {
		if s.UserId == 0 {
			model.Warnings = append(model.Warnings,
				fmt.Sprintf("Skipping scout %q: unresolved userId", s.FullName))
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
			UserId:    s.UserId,
		}
	}

	// Summary rows: Rank Requirements section.
	model.SummaryRows = append(model.SummaryRows, SummaryRow{
		Kind:  SummaryRowSectionHeader,
		Label: "Rank Requirements",
	})

	// Use the first resolved scout with populated RankReqs as the template for
	// the rank requirement row list. (All scouts in a den should share the
	// same rank.) If no scout has rank reqs, skip rank rows.
	var rankTemplate []scouting.RankRequirement
	for _, s := range resolved {
		if len(s.RankReqs.Requirements) > 0 {
			rankTemplate = s.RankReqs.Requirements
			break
		}
	}

	for _, tmpl := range rankTemplate {
		row := SummaryRow{
			Kind:     SummaryRowRankReq,
			Label:    fmt.Sprintf("%s — %s", tmpl.RequirementNumber, tmpl.Name),
			Percents: make([]float64, len(resolved)),
		}
		for i, s := range resolved {
			// Find this requirement by number in the scout's rank reqs.
			for _, r := range s.RankReqs.Requirements {
				if r.RequirementNumber == tmpl.RequirementNumber {
					row.Percents[i] = r.PercentCompleted
					break
				}
			}
		}
		model.SummaryRows = append(model.SummaryRows, row)
	}

	// Adventures section header.
	model.SummaryRows = append(model.SummaryRows, SummaryRow{
		Kind:  SummaryRowSectionHeader,
		Label: "Adventures",
	})

	// Collect the ordered set of adventure ids started by at least one scout.
	type advMeta struct {
		id        int
		name      string
		shortName string
	}
	var advOrder []advMeta
	seenAdv := map[int]bool{}
	startedAdv := map[int]bool{}
	for _, s := range resolved {
		for _, a := range s.Adventures {
			if !seenAdv[a.AdventureId] {
				seenAdv[a.AdventureId] = true
				advOrder = append(advOrder, advMeta{
					id:        a.AdventureId,
					name:      a.AdventureName,
					shortName: a.ShortName,
				})
			}
			if a.PercentCompleted > 0 {
				startedAdv[a.AdventureId] = true
			}
		}
	}

	// Filter to adventures actually started.
	var includedAdvs []advMeta
	for _, m := range advOrder {
		if startedAdv[m.id] {
			includedAdvs = append(includedAdvs, m)
		}
	}

	for _, m := range includedAdvs {
		row := SummaryRow{
			Kind:     SummaryRowAdventure,
			Label:    m.name,
			Percents: make([]float64, len(resolved)),
		}
		for i, s := range resolved {
			for _, a := range s.Adventures {
				if a.AdventureId == m.id {
					row.Percents[i] = a.PercentCompleted
					break
				}
			}
		}
		model.SummaryRows = append(model.SummaryRows, row)
	}

	// Per-adventure sheets for each included adventure.
	for _, m := range includedAdvs {
		sheet := AdventureSheet{
			AdventureId: m.id,
			Name:        m.name,
			ShortName:   m.shortName,
			OverallPcts: make([]float64, len(resolved)),
		}

		// Collect the union of requirement numbers across scouts' detail for
		// this adventure, and remember a display name for each number.
		reqNames := map[string]string{}
		var reqNumbers []string
		for _, s := range resolved {
			detail, ok := s.AdventureReqs[m.id]
			if !ok {
				continue
			}
			for _, r := range detail.Requirements {
				if _, seen := reqNames[r.RequirementNumber]; !seen {
					reqNames[r.RequirementNumber] = r.RequirementName
					reqNumbers = append(reqNumbers, r.RequirementNumber)
				}
			}
		}
		slices.Sort(reqNumbers)

		// Per-scout overall percent for this adventure.
		for i, s := range resolved {
			for _, a := range s.Adventures {
				if a.AdventureId == m.id {
					sheet.OverallPcts[i] = a.PercentCompleted
					break
				}
			}
		}

		// Build the rows. Label uses the adventure's shortName per spec.
		for _, num := range reqNumbers {
			row := AdventureRow{
				Label:          fmt.Sprintf("%s — %s", num, m.shortName),
				DatesCompleted: make([]*string, len(resolved)),
			}
			for i, s := range resolved {
				detail, ok := s.AdventureReqs[m.id]
				if !ok {
					continue
				}
				for _, r := range detail.Requirements {
					if r.RequirementNumber == num {
						row.DatesCompleted[i] = r.DateCompleted
						break
					}
				}
			}
			sheet.Rows = append(sheet.Rows, row)
		}

		model.Adventures = append(model.Adventures, sheet)
	}

	return model
}
