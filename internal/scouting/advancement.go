package scouting

import (
	"context"
	"fmt"
)

// FetchAdventures returns every adventure across every Cub Scout rank for
// the given scout's userID. The response is a flat array.
func FetchAdventures(ctx context.Context, c *Client, userID int) ([]Adventure, error) {
	var out []Adventure
	path := fmt.Sprintf("/advancements/v2/youth/%d/adventures", userID)
	if err := c.Get(ctx, path, esbYouthProfileTarget(userID), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// FilterAdventuresByRank returns only the adventures whose RankID matches.
func FilterAdventuresByRank(adventures []Adventure, rankID int) []Adventure {
	var out []Adventure
	for _, a := range adventures {
		if a.RankID == rankID {
			out = append(out, a)
		}
	}
	return out
}

// FetchRankRequirements returns the rank object + per-requirement progress
// for (scout, rank).
func FetchRankRequirements(ctx context.Context, c *Client, userID, rankID int) (RankRequirements, error) {
	var r RankRequirements
	path := fmt.Sprintf("/advancements/v2/youth/%d/ranks/%d/requirements", userID, rankID)
	if err := c.Get(ctx, path, esbYouthProfileTarget(userID), &r); err != nil {
		return RankRequirements{}, err
	}
	return r, nil
}

// FetchAdventureRequirements returns one adventure's requirement list with
// per-requirement progress for the given scout.
func FetchAdventureRequirements(ctx context.Context, c *Client, userID, adventureID int) (AdventureRequirements, error) {
	var a AdventureRequirements
	path := fmt.Sprintf("/advancements/v2/youth/%d/adventures/%d/requirements", userID, adventureID)
	if err := c.Get(ctx, path, esbYouthProfileTarget(userID), &a); err != nil {
		return AdventureRequirements{}, err
	}
	return a, nil
}
