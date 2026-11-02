package scouting

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// esbBulkEntryMBTarget is the x-esb-url value the SPA uses on the merit badge
// bulk-entry page for the given unit.
func esbBulkEntryMBTarget(orgGUID string) string {
	return "https://advancements.scouting.org/roster/bulk-entry-mb/" + orgGUID
}

// MeritBadge is one entry of the merit badge catalog.
type MeritBadge struct {
	ID       int                `json:"id"`
	Name     string             `json:"name"`
	Versions []MeritBadgeVerRef `json:"versions"`
}

// MeritBadgeVerRef is one published version of a merit badge. The API
// serves both fields as JSON strings.
type MeritBadgeVerRef struct {
	VersionID string `json:"versionId"`
	Version   string `json:"version"`
}

// FetchMeritBadges returns the merit badge catalog, including retired badges
// and versions (status=all) because scouts routinely earn old versions.
func FetchMeritBadges(ctx context.Context, c *Client, orgGUID string) ([]MeritBadge, error) {
	var out struct {
		MeritBadges []MeritBadge `json:"meritBadges"`
	}
	if err := c.Get(ctx, "/advancements/v2/meritBadges?status=all", esbBulkEntryMBTarget(orgGUID), &out); err != nil {
		return nil, err
	}
	return out.MeritBadges, nil
}

// MeritBadgeRequirement is one row of a merit badge's requirement tree.
// Parent rows ("2. Explain the following:") have an empty RequirementNumber;
// their children carry only a relative ListNumber such as "(a)".
type MeritBadgeRequirement struct {
	ID                  int    `json:"-"`
	Name                string `json:"name"`
	ListNumber          string `json:"listNumber"`
	RequirementNumber   string `json:"requirementNumber"`
	SortOrder           string `json:"sortOrder"`
	ParentRequirementID int    `json:"-"`
}

// MeritBadgeRequirements is a badge version's full requirement list.
type MeritBadgeRequirements struct {
	MeritBadgeID int
	Name         string
	VersionID    int
	Version      string
	Requirements []MeritBadgeRequirement
}

// FetchMeritBadgeRequirements returns the canonical requirement list for one
// merit badge version.
func FetchMeritBadgeRequirements(ctx context.Context, c *Client, orgGUID string, badgeID, versionID int) (MeritBadgeRequirements, error) {
	var raw struct {
		MeritBadgeID   flexInt `json:"meritBadgeId"`
		MeritBadgeName string  `json:"meritBadgeName"`
		Version        string  `json:"version"`
		VersionID      flexInt `json:"versionId"`
		Requirements   []struct {
			ID                  flexInt `json:"id"`
			Name                string  `json:"name"`
			ListNumber          string  `json:"listNumber"`
			RequirementNumber   string  `json:"requirementNumber"`
			SortOrder           string  `json:"sortOrder"`
			ParentRequirementID flexInt `json:"parentRequirementId"`
		} `json:"requirements"`
	}
	path := fmt.Sprintf("/advancements/meritBadges/%d/requirements?versionId=%d", badgeID, versionID)
	if err := c.Get(ctx, path, esbBulkEntryMBTarget(orgGUID), &raw); err != nil {
		return MeritBadgeRequirements{}, err
	}
	out := MeritBadgeRequirements{
		MeritBadgeID: raw.MeritBadgeID.Int(),
		Name:         raw.MeritBadgeName,
		VersionID:    raw.VersionID.Int(),
		Version:      raw.Version,
	}
	for _, r := range raw.Requirements {
		out.Requirements = append(out.Requirements, MeritBadgeRequirement{
			ID:                  r.ID.Int(),
			Name:                r.Name,
			ListNumber:          r.ListNumber,
			RequirementNumber:   r.RequirementNumber,
			SortOrder:           r.SortOrder,
			ParentRequirementID: r.ParentRequirementID.Int(),
		})
	}
	return out, nil
}

// UnitYouth is a registered youth of a unit, as listed by orgYouths. It is
// the only place the BSA member ID (what Black Pug exports) is paired with
// the Scoutbook user ID (what the advancement endpoints take).
type UnitYouth struct {
	MemberID string `json:"memberId"`
	UserID   int    `json:"sbUserId"`
	FullName string `json:"personFullName"`
}

// FetchUnitYouths returns every youth registered in the unit, including
// expired registrations (a camp roster can include a scout who just lapsed).
func FetchUnitYouths(ctx context.Context, c *Client, orgGUID string) ([]UnitYouth, error) {
	var out struct {
		Members []UnitYouth `json:"members"`
	}
	body := map[string]bool{"includeRegistrationDetails": true, "includeExpired": true}
	path := "/organizations/v2/" + orgGUID + "/orgYouths"
	if err := c.Post(ctx, path, "https://advancements.scouting.org/roster", body, &out); err != nil {
		return nil, err
	}
	return out.Members, nil
}

// MeritBadgeProgress is one scout's recorded progress on one merit badge, as
// returned by userRequirements. MeritBadgeID/VersionID are zero when the
// scout has no record for the badge.
type MeritBadgeProgress struct {
	UserID       int
	MemberID     string
	MeritBadgeID int
	VersionID    int
	Status       string
	// Completed maps requirement ID to its recorded completion date, for
	// requirements that already have one.
	Completed map[int]string
}

// FetchMeritBadgeProgress returns the existing progress of each listed
// member on one merit badge.
func FetchMeritBadgeProgress(ctx context.Context, c *Client, orgGUID string, badgeID int, memberIDs []string) ([]MeritBadgeProgress, error) {
	var raw []struct {
		UserID       flexInt `json:"userId"`
		MemberID     flexInt `json:"memberId"`
		MeritBadgeID flexInt `json:"meritBadgeId"`
		VersionID    flexInt `json:"versionId"`
		Status       string  `json:"status"`
		Requirements []struct {
			ID            flexInt `json:"id"`
			DateCompleted *string `json:"dateCompleted"`
		} `json:"requirements"`
	}
	path := fmt.Sprintf("/advancements/v2/organization/%s/meritBadges/%d/userRequirements?memberId=%s",
		orgGUID, badgeID, url.QueryEscape(strings.Join(memberIDs, ",")))
	if err := c.Get(ctx, path, esbBulkEntryMBTarget(orgGUID), &raw); err != nil {
		return nil, err
	}
	out := make([]MeritBadgeProgress, 0, len(raw))
	for _, r := range raw {
		p := MeritBadgeProgress{
			UserID:       r.UserID.Int(),
			MemberID:     strconv.Itoa(r.MemberID.Int()),
			MeritBadgeID: r.MeritBadgeID.Int(),
			VersionID:    r.VersionID.Int(),
			Status:       r.Status,
			Completed:    map[int]string{},
		}
		for _, req := range r.Requirements {
			if req.DateCompleted != nil && *req.DateCompleted != "" {
				p.Completed[req.ID.Int()] = *req.DateCompleted
			}
		}
		out = append(out, p)
	}
	return out, nil
}

// MeritBadgeRequirementEntry is one requirement being recorded as complete.
// Field order matches the SPA's request body.
type MeritBadgeRequirementEntry struct {
	ID                   int    `json:"id"`
	Started              bool   `json:"started"`
	DateCompleted        string `json:"dateCompleted"`
	MarkedCompletedDate  string `json:"markedCompletedDate"`
	Completed            bool   `json:"completed"`
	Approved             bool   `json:"approved"`
	LeaderApprovedUserID int    `json:"leaderApprovedUserId"`
	LeaderApprovedDate   string `json:"leaderApprovedDate"`
}

// MeritBadgeScoutEntry is one scout's batch of completed requirements.
type MeritBadgeScoutEntry struct {
	OrganizationGUID string                       `json:"organizationGuid"`
	UserID           int                          `json:"userId"`
	VersionID        int                          `json:"versionId"`
	Requirements     []MeritBadgeRequirementEntry `json:"requirements"`
}

// MeritBadgeScoutResult is the server's verdict for one scout's batch.
type MeritBadgeScoutResult struct {
	UserID           int     `json:"userId"`
	PercentCompleted float64 `json:"percentCompleted"`
	Status           string  `json:"status"`
	Requirements     []struct {
		ID      int    `json:"id"`
		Status  string `json:"status"`
		Message string `json:"message"`
	} `json:"requirements"`
}

// PostMeritBadgeRequirements records the entries against one merit badge,
// the same call the SPA's merit badge bulk-entry page makes. A 200 does not
// mean every requirement was accepted; callers must inspect each result.
func PostMeritBadgeRequirements(ctx context.Context, c *Client, orgGUID string, badgeID int, entries []MeritBadgeScoutEntry) ([]MeritBadgeScoutResult, error) {
	var out []MeritBadgeScoutResult
	path := fmt.Sprintf("/advancements/v2/youth/meritBadges/%d/requirements", badgeID)
	if err := c.Post(ctx, path, esbBulkEntryMBTarget(orgGUID), entries, &out); err != nil {
		return nil, err
	}
	return out, nil
}
