package blackpug

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
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

type groupKey struct{ member, name, version string }

type group struct {
	key  groupKey
	rows []Row
}

// itemKey identifies a (scout, export name, export version) the export lists
// as complete. The name is lower-cased export text, not the catalog name.
type itemKey struct{ member, name, version string }

// Import matches the export's merit badge and award requirement rows
// against Scouting.org and, when opts.Apply is set, records them.
//
// A scout's item is all-or-nothing: if any row can't be matched to a
// catalog requirement, nothing is sent for that scout and item, so a
// numbering mismatch is reported instead of silently leaving it
// half-entered.
func Import(ctx context.Context, c *scouting.Client, rows []Row, opts Options) (Report, error) {
	var rep Report
	warnIgnored(rows, &rep)

	var userByMember map[string]int
	for _, k := range kinds {
		groups, done := groupRows(rows, k, &rep)
		if len(groups) == 0 {
			continue
		}
		if userByMember == nil {
			youths, err := scouting.FetchUnitYouths(ctx, c, opts.OrgGUID)
			if err != nil {
				return rep, fmt.Errorf("fetch unit roster: %w", err)
			}
			userByMember = map[string]int{}
			for _, y := range youths {
				if y.UserID != 0 {
					userByMember[y.MemberID] = y.UserID
				}
			}
		}
		if err := importKind(ctx, c, k, groups, done, userByMember, opts, &rep); err != nil {
			return rep, err
		}
	}
	return rep, nil
}

func importKind(ctx context.Context, c *scouting.Client, k kind, groups []group, done map[itemKey]bool, userByMember map[string]int, opts Options, rep *Report) (retErr error) {
	firstResult := len(rep.Results)
	if opts.Apply {
		// If the token dies partway, entries never sent must not read as
		// "would record" in an apply-mode report.
		defer func() {
			if !errors.Is(retErr, scouting.ErrTokenExpired) {
				return
			}
			for i := firstResult; i < len(rep.Results); i++ {
				if r := &rep.Results[i]; r.Outcome == OutcomePlanned {
					r.Outcome = OutcomeSkipped
					r.Reason = "not sent: the token expired before this was recorded"
				}
			}
		}()
	}
	catalog, err := k.catalog(ctx, c, opts.OrgGUID)
	if err != nil {
		return fmt.Errorf("fetch %s catalog: %w", k.noun, err)
	}

	// Resolve each group to a catalog item+version, then work item by item so
	// each costs one progress lookup and one write.
	type target struct {
		item    catalogItem
		version catalogVersion
		groups  []group
	}
	targets := map[string]*target{} // keyed by "itemID/versionID"
	var targetOrder []string
	for _, g := range groups {
		res := Result{Scout: g.rows[0].Scout(), MemberID: g.key.member, Badge: g.key.name, Version: g.key.version, Outcome: OutcomeSkipped}
		if _, ok := userByMember[g.key.member]; !ok {
			res.Reason = "BSA member ID not found in the unit roster"
			rep.Results = append(rep.Results, res)
			continue
		}
		item, why := k.resolveItem(catalog, g.key.name)
		if why != "" {
			res.Reason = why
			rep.Results = append(rep.Results, res)
			continue
		}
		ver, why := k.resolveVersion(item, g.key.version)
		if why != "" {
			res.Reason = why
			rep.Results = append(rep.Results, res)
			continue
		}
		key := fmt.Sprintf("%d/%d", item.ID, ver.ID)
		if targets[key] == nil {
			targets[key] = &target{item: item, version: ver}
			targetOrder = append(targetOrder, key)
		}
		targets[key].groups = append(targets[key].groups, g)
	}

	type pending struct {
		result int // index into rep.Results
		scout  plannedScout
		done   itemKey // the export's completion row, for the status warning
	}
	pendingByItem := map[int][]pending{}
	var itemOrder []int
	// queued guards against two export groups for one scout resolving to the
	// same catalog item (e.g. "Kayaking BSA" and "Kayaking Award"): one POST
	// must not carry two entries for one user.
	type queuedKey struct{ item, user int }
	queued := map[queuedKey]bool{}

	for _, key := range targetOrder {
		t := targets[key]
		skipAll := func(reason string) {
			for _, g := range t.groups {
				rep.Results = append(rep.Results, Result{Scout: g.rows[0].Scout(), MemberID: g.key.member, Badge: t.item.Name,
					Version: t.version.Version, Outcome: OutcomeSkipped, Reason: reason})
			}
		}
		reqs, err := k.requirements(ctx, c, opts.OrgGUID, t.item.ID, t.version.ID)
		if err != nil {
			if errors.Is(err, scouting.ErrTokenExpired) {
				return err
			}
			skipAll(fmt.Sprintf("couldn't fetch the %s's requirement list: %v", k.noun, err))
			continue
		}
		idx := NewRequirementIndex(reqs.Requirements)

		members := make([]string, 0, len(t.groups))
		for _, g := range t.groups {
			members = append(members, g.key.member)
		}
		progress, err := k.progress(ctx, c, opts.OrgGUID, t.item.ID, members)
		if err != nil {
			if errors.Is(err, scouting.ErrTokenExpired) {
				return err
			}
			skipAll(fmt.Sprintf("couldn't fetch existing progress for the %s: %v", k.noun, err))
			continue
		}
		progressByMember := map[string]scouting.AdvancementProgress{}
		for _, p := range progress {
			progressByMember[p.MemberID] = p
		}

		for _, g := range t.groups {
			res := Result{Scout: g.rows[0].Scout(), MemberID: g.key.member, Badge: t.item.Name, Version: t.version.Version, Outcome: OutcomeSkipped}
			prog := progressByMember[g.key.member]
			if prog.ItemID != 0 && prog.VersionID != t.version.ID {
				res.Reason = fmt.Sprintf("scout already has this %s on a different version (id %d); not mixing versions", k.noun, prog.VersionID)
				rep.Results = append(rep.Results, res)
				continue
			}

			matched := map[int]Row{}    // requirement ID -> row (first wins)
			parentRows := map[int]Row{} // parent requirement ID -> row
			for _, row := range g.rows {
				_, num := row.BadgeAndRequirement()
				if num == "" {
					res.Unmatched = append(res.Unmatched, fmt.Sprintf("(no requirement number in %q, line %d)", row.Advancement, row.Line))
					continue
				}
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
			parentIDs := make([]int, 0, len(parentRows))
			for id := range parentRows {
				parentIDs = append(parentIDs, id)
			}
			idx.SortByCatalogOrder(parentIDs) // deterministic warning order
			for _, id := range parentIDs {
				row := parentRows[id]
				if !idx.HasDescendantIn(id, leaves) {
					_, num := row.BadgeAndRequirement()
					rep.Warnings = append(rep.Warnings, fmt.Sprintf("%s: %s lists parent requirement %s without any of its sub-requirements; nothing was recorded for it", res.Scout, t.item.Name, num))
				}
			}
			if len(res.Unmatched) == 0 && len(matched) == 0 {
				res.Reason = "the export lists only parent requirements, with no sub-requirements to record"
				rep.Results = append(rep.Results, res)
				continue
			}
			if len(res.Unmatched) > 0 {
				res.Reason = fmt.Sprintf("%d requirement number(s) don't match the %s %s catalog", len(res.Unmatched), t.item.Name, t.version.Version)
				rep.Results = append(rep.Results, res)
				continue
			}

			var ids []int
			for id := range matched {
				if _, already := prog.Completed[id]; already {
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

			ps := plannedScout{UserID: userByMember[g.key.member], VersionID: t.version.ID}
			qk := queuedKey{t.item.ID, ps.UserID}
			if queued[qk] {
				res.Reason = fmt.Sprintf("another export entry for this scout already resolves to this %s; not sending it twice", k.noun)
				rep.Results = append(rep.Results, res)
				continue
			}
			queued[qk] = true
			for _, id := range ids {
				ps.Reqs = append(ps.Reqs, plannedReq{ID: id, Date: matched[id].Completed.Format("2006-01-02")})
			}
			res.Outcome = OutcomePlanned
			rep.Results = append(rep.Results, res)
			if _, seen := pendingByItem[t.item.ID]; !seen {
				itemOrder = append(itemOrder, t.item.ID)
			}
			pendingByItem[t.item.ID] = append(pendingByItem[t.item.ID], pending{result: len(rep.Results) - 1, scout: ps,
				done: itemKey{g.key.member, strings.ToLower(g.key.name), g.key.version}})
		}
	}

	if !opts.Apply {
		return nil
	}

	today := opts.Today.Format("2006-01-02")
	for _, itemID := range itemOrder {
		pend := pendingByItem[itemID]
		scouts := make([]plannedScout, 0, len(pend))
		for _, p := range pend {
			scouts = append(scouts, p.scout)
		}
		results, err := k.post(ctx, c, opts.OrgGUID, itemID, scouts, opts.LeaderUserID, today)
		if err != nil {
			if errors.Is(err, scouting.ErrTokenExpired) {
				return err
			}
			// Unknown whether the write landed; say so rather than guess.
			for _, p := range pend {
				r := &rep.Results[p.result]
				r.Outcome = OutcomeFailed
				r.Reason = fmt.Sprintf("request failed (the server may or may not have recorded it; re-run to find out): %v", err)
			}
			continue
		}
		byUser := map[int]scouting.AdvancementScoutResult{}
		for _, r := range results {
			byUser[r.UserID] = r
		}
		for _, p := range pend {
			r := &rep.Results[p.result]
			sr, ok := byUser[p.scout.UserID]
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
			if len(sr.Requirements) < len(p.scout.Reqs) {
				bad = append(bad, fmt.Sprintf("server confirmed %d of %d requirements", len(sr.Requirements), len(p.scout.Reqs)))
			}
			if len(bad) > 0 {
				r.Outcome = OutcomeFailed
				r.Reason = strings.Join(bad, "; ")
				continue
			}
			r.Outcome = OutcomeApplied
			if done[p.done] && sr.Status != "Leader Approved" {
				rep.Warnings = append(rep.Warnings, fmt.Sprintf("%s: %s is marked complete in the export, but Scouting.org reports status %q", r.Scout, r.Badge, sr.Status))
			}
		}
	}
	return nil
}

// warnIgnored notes rows no kind imports (e.g. rank requirements).
func warnIgnored(rows []Row, rep *Report) {
	ignored := map[string]int{}
	for _, row := range rows {
		if handledType(row.Type) {
			continue
		}
		name, _ := row.BadgeAndRequirement()
		ignored[row.Scout()+"\x00"+row.Type+"\x00"+name]++
	}
	var out []string
	for k, n := range ignored {
		parts := strings.Split(k, "\x00")
		out = append(out, fmt.Sprintf("ignored %d %q row(s) for %s: %s (only merit badges and awards can be imported)", n, parts[1], parts[0], parts[2]))
	}
	sort.Strings(out)
	rep.Warnings = append(rep.Warnings, out...)
}

// groupRows buckets one kind's requirement rows by (scout, name, version),
// keeping first-seen order, and warns about unapproved rows. The returned
// set holds the (scout, name) pairs the export lists as completed.
func groupRows(rows []Row, k kind, rep *Report) ([]group, map[itemKey]bool) {
	var groups []group
	index := map[groupKey]int{}
	completed := map[itemKey]bool{}
	unapproved := map[string]int{}

	for _, row := range rows {
		switch row.Type {
		case k.doneType:
			completed[itemKey{row.MemberID, strings.ToLower(row.Advancement), row.Version}] = true
		case k.reqType:
			if !row.Approved {
				unapproved[row.Scout()]++
				continue
			}
			name, _ := row.BadgeAndRequirement()
			gk := groupKey{row.MemberID, strings.ToLower(name), row.Version}
			i, ok := index[gk]
			if !ok {
				i = len(groups)
				index[gk] = i
				groups = append(groups, group{key: groupKey{row.MemberID, name, row.Version}})
			}
			groups[i].rows = append(groups[i].rows, row)
		}
	}

	var skipped []string
	for scout, n := range unapproved {
		skipped = append(skipped, fmt.Sprintf("ignored %d %s requirement row(s) for %s that are not marked approved", n, k.noun, scout))
	}
	sort.Strings(skipped)
	rep.Warnings = append(rep.Warnings, skipped...)
	return groups, completed
}

// Write prints a human-readable summary of the report.
func (r Report) Write(out io.Writer, apply bool) {
	// Build in memory (strings.Builder writes can't fail), then write once.
	var sb strings.Builder
	w := &sb
	defer func() { _, _ = io.WriteString(out, sb.String()) }()
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
