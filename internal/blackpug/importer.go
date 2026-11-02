package blackpug

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pkaeding/scoutbook-xls/internal/scouting"
)

// Outcome says what happened (or, in a dry run, would happen) to one scout's
// work on one merit badge.
type Outcome string

const (
	// OutcomePlanned: dry run; the requirements would be recorded with --apply.
	OutcomePlanned Outcome = "would record"
	// OutcomeApplied: the requirements were recorded.
	OutcomeApplied Outcome = "recorded"
	// OutcomeAlreadyEntered: every requirement already has a completion date.
	OutcomeAlreadyEntered Outcome = "already entered"
	// OutcomeSkipped: nothing was sent for this scout and badge; see Reason.
	OutcomeSkipped Outcome = "skipped"
	// OutcomeFailed: the server rejected some or all of the requirements.
	OutcomeFailed Outcome = "FAILED"
)

// Result is the outcome for one (scout, badge, version).
type Result struct {
	Scout    string
	MemberID string
	Badge    string
	Version  string
	Outcome  Outcome
	// Reason explains a skip or failure.
	Reason string
	// Total is the number of distinct requirements the export lists as
	// complete; AlreadyDone of them were already on record, and Sent are the
	// remainder that were (or would be) recorded.
	Total       int
	AlreadyDone int
	Sent        int
	// ParentsSkipped counts export rows for parent requirements ("2." above
	// 2a-2d). Scouting.org tracks completion on sub-requirements only, so
	// these are deliberately not sent.
	ParentsSkipped int
	// Unmatched lists export requirement numbers with no catalog match.
	Unmatched []string
}

// Report is everything an import did or would do.
type Report struct {
	Results  []Result
	Warnings []string
}

// Options configures an import.
type Options struct {
	// OrgGUID is the unit (troop) the scouts are registered in.
	OrgGUID string
	// LeaderUserID is recorded as the approving leader (the JWT's uid).
	LeaderUserID int
	// Today is recorded as the date entered and approved.
	Today time.Time
	// Apply writes to Scouting.org. Without it the import is a dry run that
	// still reads from the API to validate everything.
	Apply bool
}

type groupKey struct{ member, badge, version string }

type group struct {
	key  groupKey
	rows []Row
}

// Import matches the export's merit badge requirement rows against
// Scouting.org and, when opts.Apply is set, records them.
//
// A scout's badge is all-or-nothing: if any row can't be matched to a
// catalog requirement, nothing is sent for that scout and badge, so a
// numbering mismatch is reported instead of silently leaving a badge
// half-entered.
func Import(ctx context.Context, c *scouting.Client, rows []Row, opts Options) (Report, error) {
	var rep Report
	groups, badgeRows := groupRows(rows, &rep)
	if len(groups) == 0 {
		return rep, nil
	}

	youths, err := scouting.FetchUnitYouths(ctx, c, opts.OrgGUID)
	if err != nil {
		return rep, fmt.Errorf("fetch unit roster: %w", err)
	}
	userByMember := map[string]int{}
	for _, y := range youths {
		if y.UserID != 0 {
			userByMember[y.MemberID] = y.UserID
		}
	}

	catalog, err := scouting.FetchMeritBadges(ctx, c, opts.OrgGUID)
	if err != nil {
		return rep, fmt.Errorf("fetch merit badge catalog: %w", err)
	}

	// Resolve each group to a catalog badge+version, then work badge by badge
	// so each badge costs one progress lookup and one write.
	type target struct {
		badge   scouting.MeritBadge
		version scouting.MeritBadgeVerRef
		groups  []group
	}
	targets := map[string]*target{} // keyed by "badgeID/versionID"
	var targetOrder []string
	for _, g := range groups {
		res := Result{Scout: g.rows[0].Scout(), MemberID: g.key.member, Badge: g.key.badge, Version: g.key.version, Outcome: OutcomeSkipped}
		if _, ok := userByMember[g.key.member]; !ok {
			res.Reason = "BSA member ID not found in the unit roster"
			rep.Results = append(rep.Results, res)
			continue
		}
		mb, ok := findBadge(catalog, g.key.badge)
		if !ok {
			res.Reason = "no merit badge with this name in the catalog"
			rep.Results = append(rep.Results, res)
			continue
		}
		ver, ok := findVersion(mb, g.key.version)
		if !ok {
			res.Reason = fmt.Sprintf("catalog has no %s version %q", mb.Name, g.key.version)
			rep.Results = append(rep.Results, res)
			continue
		}
		k := fmt.Sprintf("%d/%s", mb.ID, ver.VersionID)
		if targets[k] == nil {
			targets[k] = &target{badge: mb, version: ver}
			targetOrder = append(targetOrder, k)
		}
		targets[k].groups = append(targets[k].groups, g)
	}

	// Collect what to send per badge ID.
	type pending struct {
		result  int // index into rep.Results
		userID  int
		version int
		reqs    []scouting.MeritBadgeRequirementEntry
	}
	pendingByBadge := map[int][]pending{}
	var badgeOrder []int

	for _, k := range targetOrder {
		t := targets[k]
		versionID, _ := strconv.Atoi(t.version.VersionID)
		reqs, err := scouting.FetchMeritBadgeRequirements(ctx, c, opts.OrgGUID, t.badge.ID, versionID)
		if err != nil {
			return rep, fmt.Errorf("fetch %s requirements: %w", t.badge.Name, err)
		}
		idx := NewRequirementIndex(reqs.Requirements)

		members := make([]string, 0, len(t.groups))
		for _, g := range t.groups {
			members = append(members, g.key.member)
		}
		progress, err := scouting.FetchMeritBadgeProgress(ctx, c, opts.OrgGUID, t.badge.ID, members)
		if err != nil {
			return rep, fmt.Errorf("fetch %s progress: %w", t.badge.Name, err)
		}
		progressByMember := map[string]scouting.MeritBadgeProgress{}
		for _, p := range progress {
			progressByMember[p.MemberID] = p
		}

		for _, g := range t.groups {
			res := Result{Scout: g.rows[0].Scout(), MemberID: g.key.member, Badge: t.badge.Name, Version: t.version.Version, Outcome: OutcomeSkipped}
			prog := progressByMember[g.key.member]
			if prog.MeritBadgeID != 0 && prog.VersionID != versionID {
				res.Reason = fmt.Sprintf("scout already has this badge on a different version (id %d); not mixing versions", prog.VersionID)
				rep.Results = append(rep.Results, res)
				continue
			}

			matched := map[int]Row{}    // requirement ID -> row (first wins)
			parentRows := map[int]Row{} // parent requirement ID -> row
			for _, row := range g.rows {
				_, num := row.BadgeAndRequirement()
				id, ok := idx.Lookup(num)
				if !ok {
					res.Unmatched = append(res.Unmatched, num)
					continue
				}
				if idx.IsParent(id) {
					parentRows[id] = row
					continue
				}
				if _, dup := matched[id]; !dup {
					matched[id] = row
				}
			}
			res.Total = len(matched)
			res.ParentsSkipped = len(parentRows)
			leaves := map[int]bool{}
			for id := range matched {
				leaves[id] = true
			}
			for id, row := range parentRows {
				if !idx.HasDescendantIn(id, leaves) {
					_, num := row.BadgeAndRequirement()
					rep.Warnings = append(rep.Warnings, fmt.Sprintf("%s: %s lists parent requirement %s without any of its sub-requirements; nothing was recorded for it", res.Scout, t.badge.Name, num))
				}
			}
			if len(res.Unmatched) == 0 && len(matched) == 0 {
				res.Reason = "the export lists only parent requirements, with no sub-requirements to record"
				rep.Results = append(rep.Results, res)
				continue
			}
			if len(res.Unmatched) > 0 {
				res.Reason = fmt.Sprintf("%d requirement number(s) don't match the %s %s catalog", len(res.Unmatched), t.badge.Name, t.version.Version)
				rep.Results = append(rep.Results, res)
				continue
			}

			var ids []int
			for id := range matched {
				if _, done := prog.Completed[id]; done {
					res.AlreadyDone++
					continue
				}
				ids = append(ids, id)
			}
			idx.SortByCatalogOrder(ids)
			res.Sent = len(ids)
			if len(ids) == 0 {
				res.Outcome = OutcomeAlreadyEntered
				rep.Results = append(rep.Results, res)
				continue
			}

			today := opts.Today.Format("2006-01-02")
			entries := make([]scouting.MeritBadgeRequirementEntry, 0, len(ids))
			for _, id := range ids {
				entries = append(entries, scouting.MeritBadgeRequirementEntry{
					ID:                   id,
					Started:              true,
					DateCompleted:        matched[id].Completed.Format("2006-01-02"),
					MarkedCompletedDate:  today,
					Completed:            true,
					Approved:             true,
					LeaderApprovedUserID: opts.LeaderUserID,
					LeaderApprovedDate:   today,
				})
			}
			res.Outcome = OutcomePlanned
			rep.Results = append(rep.Results, res)
			if _, seen := pendingByBadge[t.badge.ID]; !seen {
				badgeOrder = append(badgeOrder, t.badge.ID)
			}
			pendingByBadge[t.badge.ID] = append(pendingByBadge[t.badge.ID], pending{
				result: len(rep.Results) - 1, userID: userByMember[g.key.member], version: versionID, reqs: entries,
			})
		}
	}

	if !opts.Apply {
		return rep, nil
	}

	for _, badgeID := range badgeOrder {
		ps := pendingByBadge[badgeID]
		body := make([]scouting.MeritBadgeScoutEntry, 0, len(ps))
		for _, p := range ps {
			body = append(body, scouting.MeritBadgeScoutEntry{
				OrganizationGUID: opts.OrgGUID, UserID: p.userID, VersionID: p.version, Requirements: p.reqs,
			})
		}
		results, err := scouting.PostMeritBadgeRequirements(ctx, c, opts.OrgGUID, badgeID, body)
		if err != nil {
			if errors.Is(err, scouting.ErrTokenExpired) {
				return rep, err
			}
			// Unknown whether the write landed; say so rather than guess.
			for _, p := range ps {
				r := &rep.Results[p.result]
				r.Outcome = OutcomeFailed
				r.Reason = fmt.Sprintf("request failed (the server may or may not have recorded it; re-run to find out): %v", err)
			}
			continue
		}
		byUser := map[int]scouting.MeritBadgeScoutResult{}
		for _, r := range results {
			byUser[r.UserID] = r
		}
		for _, p := range ps {
			r := &rep.Results[p.result]
			sr, ok := byUser[p.userID]
			if !ok {
				r.Outcome = OutcomeFailed
				r.Reason = "server response did not mention this scout"
				continue
			}
			var bad []string
			for _, rr := range sr.Requirements {
				if rr.Status != "Success" {
					bad = append(bad, fmt.Sprintf("requirement %d: %s %s", rr.ID, rr.Status, rr.Message))
				}
			}
			if len(sr.Requirements) < len(p.reqs) {
				bad = append(bad, fmt.Sprintf("server confirmed %d of %d requirements", len(sr.Requirements), len(p.reqs)))
			}
			if len(bad) > 0 {
				r.Outcome = OutcomeFailed
				r.Reason = strings.Join(bad, "; ")
				continue
			}
			r.Outcome = OutcomeApplied
			if badgeRows[badgeKey{r.MemberID, strings.ToLower(r.Badge)}] && sr.Status != "Leader Approved" {
				rep.Warnings = append(rep.Warnings, fmt.Sprintf("%s: %s is marked complete in the export, but Scouting.org reports status %q", r.Scout, r.Badge, sr.Status))
			}
		}
	}
	return rep, nil
}

type badgeKey struct{ member, badge string }

// groupRows buckets requirement rows by (scout, badge, version), keeping
// first-seen order, and notes rows it deliberately ignores in rep.Warnings.
// The returned set holds the (scout, badge) pairs the export lists as a
// completed merit badge.
func groupRows(rows []Row, rep *Report) ([]group, map[badgeKey]bool) {
	var groups []group
	index := map[groupKey]int{}
	completedBadges := map[badgeKey]bool{}
	ignoredAwards := map[string]int{}
	unapproved := map[string]int{}

	for _, row := range rows {
		switch row.Type {
		case TypeMeritBadge:
			completedBadges[badgeKey{row.MemberID, strings.ToLower(row.Advancement)}] = true
		case TypeMeritBadgeRequirement:
			if !row.Approved {
				unapproved[row.Scout()]++
				continue
			}
			badge, _ := row.BadgeAndRequirement()
			k := groupKey{row.MemberID, strings.ToLower(badge), row.Version}
			i, ok := index[k]
			if !ok {
				i = len(groups)
				index[k] = i
				groups = append(groups, group{key: groupKey{row.MemberID, badge, row.Version}})
			}
			groups[i].rows = append(groups[i].rows, row)
		default:
			badge, _ := row.BadgeAndRequirement()
			ignoredAwards[row.Scout()+"\x00"+row.Type+"\x00"+badge]++
		}
	}

	var ignored []string
	for k, n := range ignoredAwards {
		parts := strings.Split(k, "\x00")
		ignored = append(ignored, fmt.Sprintf("ignored %d %q row(s) for %s: %s (only merit badge requirements can be imported)", n, parts[1], parts[0], parts[2]))
	}
	sort.Strings(ignored)
	rep.Warnings = append(rep.Warnings, ignored...)
	var skipped []string
	for scout, n := range unapproved {
		skipped = append(skipped, fmt.Sprintf("ignored %d requirement row(s) for %s that are not marked approved", n, scout))
	}
	sort.Strings(skipped)
	rep.Warnings = append(rep.Warnings, skipped...)
	return groups, completedBadges
}

func findBadge(catalog []scouting.MeritBadge, name string) (scouting.MeritBadge, bool) {
	for _, mb := range catalog {
		if strings.EqualFold(strings.TrimSpace(mb.Name), name) {
			return mb, true
		}
	}
	return scouting.MeritBadge{}, false
}

func findVersion(mb scouting.MeritBadge, version string) (scouting.MeritBadgeVerRef, bool) {
	for _, v := range mb.Versions {
		if v.Version == version {
			return v, true
		}
	}
	return scouting.MeritBadgeVerRef{}, false
}

// Write prints a human-readable summary of the report.
func (r Report) Write(w io.Writer, apply bool) {
	scoutW, badgeW, outcomeW := 0, 0, 0
	for _, res := range r.Results {
		scoutW = max(scoutW, len(res.Scout))
		badgeW = max(badgeW, len(fmt.Sprintf("%s (%s)", res.Badge, res.Version)))
		outcomeW = max(outcomeW, len(res.Outcome))
	}
	for _, res := range r.Results {
		fmt.Fprintf(w, "%-*s  %-*s  %-*s", scoutW, res.Scout, badgeW, fmt.Sprintf("%s (%s)", res.Badge, res.Version), outcomeW, res.Outcome)
		switch res.Outcome {
		case OutcomePlanned, OutcomeApplied:
			fmt.Fprintf(w, " %d requirement(s)", res.Sent)
			if res.AlreadyDone > 0 {
				fmt.Fprintf(w, ", %d already on record", res.AlreadyDone)
			}
			if res.ParentsSkipped > 0 {
				fmt.Fprintf(w, ", %d parent row(s) not needed", res.ParentsSkipped)
			}
		case OutcomeAlreadyEntered:
			fmt.Fprintf(w, " all %d requirement(s) already on record", res.Total)
		default:
			fmt.Fprintf(w, " %s", res.Reason)
		}
		fmt.Fprintln(w)
		if len(res.Unmatched) > 0 {
			fmt.Fprintf(w, "    unmatched requirement numbers: %s\n", strings.Join(res.Unmatched, ", "))
		}
	}
	for _, warn := range r.Warnings {
		fmt.Fprintf(w, "warning: %s\n", warn)
	}
	if !apply {
		fmt.Fprintln(w, "\nDry run: nothing was changed. Re-run with --apply to record the above.")
	}
}

// Problems reports whether any result needs the user's attention.
func (r Report) Problems() bool {
	for _, res := range r.Results {
		if res.Outcome == OutcomeSkipped || res.Outcome == OutcomeFailed {
			return true
		}
	}
	return false
}
