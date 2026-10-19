package scouting

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// ScoutWithDen is a flattened per-scout record combining fields from the
// roster and both shapes of the /personprofile response. It's the unit the
// rest of the pipeline operates on.
type ScoutWithDen struct {
	PersonGUID string
	FullName   string
	FirstName  string
	LastName   string
	UserID     int
	DenType    string
	DenNumber  string
	DenID      int
	RankID     int
}

// esbRosterTarget mimics the value the SPA puts in x-esb-url when loading
// the pack roster page. Any plausible string works, but matching the real
// SPA keeps request fingerprints close to a browser's.
const esbRosterTarget = "https://advancements.scouting.org/roster"

// esbYouthProfileTarget builds the x-esb-url value the SPA uses when
// loading a youth's profile/advancement page.
func esbYouthProfileTarget(userID int) string {
	return fmt.Sprintf("https://advancements.scouting.org/youthProfile/%d", userID)
}

// FetchRoster returns the Pack roster (all registered positions + the
// people assigned to each) for the given organizationGuid.
func FetchRoster(ctx context.Context, c *Client, orgGUID string) (Roster, error) {
	var r Roster
	path := "/organizations/positions/" + orgGUID
	if err := c.Get(ctx, path, esbRosterTarget, &r); err != nil {
		return Roster{}, err
	}
	return r, nil
}

// ExtractYouthMembers returns every YouthMember from every position whose
// positionLong is "Youth Member". In practice there's only one such
// position, but iterating handles the general case without assuming that.
func ExtractYouthMembers(r Roster) []YouthMember {
	var out []YouthMember
	for _, p := range r.Positions {
		if p.PositionLong != "Youth Member" {
			continue
		}
		out = append(out, p.PersonsAssigned...)
	}
	return out
}

// fetchProfileByGUID calls the polymorphic /personprofile endpoint with a
// personGUID. The response shape yields profile.userID.
func fetchProfileByGUID(ctx context.Context, c *Client, personGUID string) (PersonProfile, error) {
	var p PersonProfile
	path := "/persons/v2/" + personGUID + "/personprofile"
	// The SPA uses the scout's userID in the x-esb-url — but at this point
	// we don't know it yet. The target is advisory only; use the roster page.
	if err := c.Get(ctx, path, esbRosterTarget, &p); err != nil {
		return PersonProfile{}, err
	}
	return p, nil
}

// fetchProfileByUserID calls the polymorphic /personprofile endpoint with
// a numeric userID. The response shape yields currentProgramsAndRanks.
func fetchProfileByUserID(ctx context.Context, c *Client, userID int) (PersonProfile, error) {
	var p PersonProfile
	path := fmt.Sprintf("/persons/v2/%d/personprofile", userID)
	if err := c.Get(ctx, path, esbYouthProfileTarget(userID), &p); err != nil {
		return PersonProfile{}, err
	}
	return p, nil
}

// ResolveScoutDens turns each YouthMember into a ScoutWithDen by issuing
// the two-step /personprofile dance in parallel. Concurrency is capped by
// the concurrency argument. Any failure for a given scout is wrapped with
// the scout's full name, appended to the returned errors slice, and that
// scout is omitted from the scouts slice (no stub emitted).
func ResolveScoutDens(ctx context.Context, c *Client, youth []YouthMember, concurrency int) ([]ScoutWithDen, []error) {
	if concurrency < 1 {
		concurrency = 1
	}

	var (
		mu     sync.Mutex
		scouts []ScoutWithDen
		errs   []error
	)

	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for _, y := range youth {
		y := y // capture per iteration

		// Respect ctx cancellation while waiting for a slot.
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			mu.Lock()
			errs = append(errs, fmt.Errorf("%s: %w", y.FullName, ctx.Err()))
			mu.Unlock()
			continue
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			// Step 1: personGUID → userID.
			byGUID, err := fetchProfileByGUID(ctx, c, y.PersonGUID)
			if err != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("%s: fetch profile by guid: %w", y.FullName, err))
				mu.Unlock()
				return
			}
			if byGUID.Profile.UserID == nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("%s: profile by guid missing userID", y.FullName))
				mu.Unlock()
				return
			}
			userID := *byGUID.Profile.UserID

			// Step 2: userID → den info.
			byUserID, err := fetchProfileByUserID(ctx, c, userID)
			if err != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("%s: fetch profile by userID: %w", y.FullName, err))
				mu.Unlock()
				return
			}
			if len(byUserID.CurrentProgramsAndRanks) == 0 {
				mu.Lock()
				errs = append(errs, fmt.Errorf("%s: profile by userID has no currentProgramsAndRanks", y.FullName))
				mu.Unlock()
				return
			}
			prog := byUserID.CurrentProgramsAndRanks[0]

			s := ScoutWithDen{
				PersonGUID: y.PersonGUID,
				FullName:   y.FullName,
				FirstName:  y.FirstName,
				LastName:   y.LastName,
				UserID:     userID,
				DenType:    prog.DenType,
				DenNumber:  prog.DenNumber,
				DenID:      prog.DenID,
				RankID:     prog.RankID,
			}

			mu.Lock()
			scouts = append(scouts, s)
			mu.Unlock()
		}()
	}

	wg.Wait()
	return scouts, errs
}

// FilterByDen returns the scouts in the given den. Matching ignores case and
// surrounding whitespace: the API spells den types inconsistently (e.g.
// "Arrow Of Light"), and users type them the way they'd write them.
func FilterByDen(scouts []ScoutWithDen, denType, denNumber string) []ScoutWithDen {
	denType = strings.TrimSpace(denType)
	denNumber = strings.TrimSpace(denNumber)
	var out []ScoutWithDen
	for _, s := range scouts {
		if strings.EqualFold(strings.TrimSpace(s.DenType), denType) &&
			strings.EqualFold(strings.TrimSpace(s.DenNumber), denNumber) {
			out = append(out, s)
		}
	}
	return out
}
