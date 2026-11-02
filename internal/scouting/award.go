package scouting

import (
	"context"
	"fmt"
)

// esbBulkEntryAwardTarget is the x-esb-url value the SPA uses on the award
// bulk-entry page for the given unit.
func esbBulkEntryAwardTarget(orgGUID string) string {
	return "https://advancements.scouting.org/roster/bulk-entry-award/" + orgGUID
}

// Award is one entry of the award catalog.
type Award struct {
	ID       int
	Name     string
	Versions []AwardVersion
}

// AwardVersion is one published version of an award.
type AwardVersion struct {
	VersionID int
	Version   string
	// Expired is true when the version has an expiry date.
	Expired bool
}

// FetchAwards returns the award catalog (every award, all programs).
func FetchAwards(ctx context.Context, c *Client, orgGUID string) ([]Award, error) {
	var raw struct {
		Awards []struct {
			ID       flexInt `json:"id"`
			Name     string  `json:"name"`
			Versions []struct {
				AwardVersionID flexInt `json:"awardVersionId"`
				Version        string  `json:"version"`
				ExpiryDt       *string `json:"versionExpiryDt"`
			} `json:"versions"`
		} `json:"awards"`
	}
	if err := c.Get(ctx, "/advancements/awards", esbBulkEntryAwardTarget(orgGUID), &raw); err != nil {
		return nil, err
	}
	out := make([]Award, 0, len(raw.Awards))
	for _, a := range raw.Awards {
		award := Award{ID: a.ID.Int(), Name: a.Name}
		for _, v := range a.Versions {
			award.Versions = append(award.Versions, AwardVersion{
				VersionID: v.AwardVersionID.Int(), Version: v.Version,
				Expired: v.ExpiryDt != nil && *v.ExpiryDt != "",
			})
		}
		out = append(out, award)
	}
	return out, nil
}

// FetchAwardRequirements returns the requirement list for one award version.
//
// UNVERIFIED: no capture includes this call. The path and response shape are
// assumed to mirror the merit badge equivalent
// (/advancements/meritBadges/{id}/requirements?versionId=), so the same
// decoder is used. A dry run against the live API will show whether that holds.
func FetchAwardRequirements(ctx context.Context, c *Client, orgGUID string, awardID, versionID int) (MeritBadgeRequirements, error) {
	path := fmt.Sprintf("/advancements/awards/%d/requirements?versionId=%d", awardID, versionID)
	return fetchRequirementList(ctx, c, path, esbBulkEntryAwardTarget(orgGUID))
}

// FetchAwardProgress returns the existing progress of each listed member on
// one award.
func FetchAwardProgress(ctx context.Context, c *Client, orgGUID string, awardID int, memberIDs []string) ([]AdvancementProgress, error) {
	return fetchProgress(ctx, c, orgGUID, "awards", awardID, memberIDs, true)
}

// AwardRequirementEntry is one award requirement being recorded as complete.
// Unlike merit badges, awards use dateEarned and carry no leader user ID.
// Field order matches the SPA's request body.
type AwardRequirementEntry struct {
	ID                  int    `json:"id"`
	DateEarned          string `json:"dateEarned"`
	Started             bool   `json:"started"`
	Completed           bool   `json:"completed"`
	MarkedCompletedDate string `json:"markedCompletedDate"`
	Approved            bool   `json:"approved"`
	LeaderApprovedDate  string `json:"leaderApprovedDate"`
}

// AwardScoutEntry is one scout's batch of completed award requirements.
type AwardScoutEntry struct {
	OrganizationGUID string                  `json:"organizationGuid"`
	UserID           int                     `json:"userId"`
	VersionID        int                     `json:"versionId"`
	Requirements     []AwardRequirementEntry `json:"requirements"`
}

// PostAwardRequirements records the entries against one award, the same
// call the SPA's award bulk-entry page makes. A 200 does not mean every
// requirement was accepted; callers must inspect each result.
func PostAwardRequirements(ctx context.Context, c *Client, orgGUID string, awardID int, entries []AwardScoutEntry) ([]AdvancementScoutResult, error) {
	var out []AdvancementScoutResult
	path := fmt.Sprintf("/advancements/v2/youth/awards/%d/requirements", awardID)
	if err := c.Post(ctx, path, esbBulkEntryAwardTarget(orgGUID), entries, &out); err != nil {
		return nil, err
	}
	return out, nil
}
