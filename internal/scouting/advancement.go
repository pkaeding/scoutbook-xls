package scouting

import (
	"context"
	"fmt"
	"sort"
)

// FetchAdventures returns every adventure across every Cub Scout rank for
// the given scout's userId. The response is a flat array.
func FetchAdventures(ctx context.Context, c *Client, userId int) ([]Adventure, error) {
	var out []Adventure
	path := fmt.Sprintf("/advancements/v2/youth/%d/adventures", userId)
	if err := c.Get(ctx, path, esbYouthProfileTarget(userId), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// FilterAdventuresByRank returns only the adventures whose RankId matches.
func FilterAdventuresByRank(adventures []Adventure, rankId int) []Adventure {
	var out []Adventure
	for _, a := range adventures {
		if a.RankId == rankId {
			out = append(out, a)
		}
	}
	return out
}

// FetchRankRequirements returns the rank object + per-requirement progress
// for (scout, rank).
func FetchRankRequirements(ctx context.Context, c *Client, userId, rankId int) (RankRequirements, error) {
	var r RankRequirements
	path := fmt.Sprintf("/advancements/v2/youth/%d/ranks/%d/requirements", userId, rankId)
	if err := c.Get(ctx, path, esbYouthProfileTarget(userId), &r); err != nil {
		return RankRequirements{}, err
	}
	return r, nil
}

// FetchAdventureRequirements returns one adventure's requirement list with
// per-requirement progress for the given scout.
func FetchAdventureRequirements(ctx context.Context, c *Client, userId, adventureId int) (AdventureRequirements, error) {
	var a AdventureRequirements
	path := fmt.Sprintf("/advancements/v2/youth/%d/adventures/%d/requirements", userId, adventureId)
	if err := c.Get(ctx, path, esbYouthProfileTarget(userId), &a); err != nil {
		return AdventureRequirements{}, err
	}
	return a, nil
}

// DetermineTargetRank picks the rankId held by the majority of scouts.
// Ties are broken by smallest rankId (deterministic). Any scout whose
// rankId differs from the winner is surfaced by name in warnings. If the
// input is empty the result is (0, []string{"no scouts"}).
func DetermineTargetRank(scouts []ScoutWithDen) (int, []string) {
	if len(scouts) == 0 {
		return 0, []string{"no scouts"}
	}

	counts := map[int]int{}
	for _, s := range scouts {
		counts[s.RankId]++
	}

	// Rank ids sorted ascending so equal counts resolve to the smallest id.
	ids := make([]int, 0, len(counts))
	for id := range counts {
		ids = append(ids, id)
	}
	sort.Ints(ids)

	target := ids[0]
	best := counts[target]
	for _, id := range ids[1:] {
		if counts[id] > best {
			target = id
			best = counts[id]
		}
	}

	// Unanimous? No warnings.
	if len(ids) == 1 {
		return target, nil
	}

	var warnings []string
	// Include a summary of the tie/split so warnings reference each non-target
	// rankId (needed for the tie test which asserts both "10" and "11" appear).
	for _, id := range ids {
		if id == target {
			continue
		}
		var names []string
		for _, s := range scouts {
			if s.RankId == id {
				names = append(names, s.FullName)
			}
		}
		warnings = append(warnings, fmt.Sprintf(
			"rankId %d (%d scouts) differs from target rankId %d: %v",
			id, counts[id], target, names,
		))
	}
	return target, warnings
}
