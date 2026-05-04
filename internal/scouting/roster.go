package scouting

import (
	"context"
	"fmt"
	"sync"
)

// ScoutWithDen is a flattened per-scout record combining fields from the
// roster and both shapes of the /personprofile response. It's the unit the
// rest of the pipeline operates on.
type ScoutWithDen struct {
	PersonGuid string
	FullName   string
	FirstName  string
	LastName   string
	UserId     int
	DenType    string
	DenNumber  string
	DenId      int
	RankId     int
}

// esbRosterTarget mimics the value the SPA puts in x-esb-url when loading
// the pack roster page. Any plausible string works, but matching the real
// SPA keeps request fingerprints close to a browser's.
const esbRosterTarget = "https://advancements.scouting.org/roster"

// esbYouthProfileTarget builds the x-esb-url value the SPA uses when
// loading a youth's profile/advancement page.
func esbYouthProfileTarget(userId int) string {
	return fmt.Sprintf("https://advancements.scouting.org/youthProfile/%d", userId)
}

// FetchRoster returns the Pack roster (all registered positions + the
// people assigned to each) for the given organizationGuid.
func FetchRoster(ctx context.Context, c *Client, orgGuid string) (Roster, error) {
	var r Roster
	path := "/organizations/positions/" + orgGuid
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

// fetchProfileByGuid calls the polymorphic /personprofile endpoint with a
// personGuid. The response shape yields profile.userId.
func fetchProfileByGuid(ctx context.Context, c *Client, personGuid string) (PersonProfile, error) {
	var p PersonProfile
	path := "/persons/v2/" + personGuid + "/personprofile"
	// The SPA uses the scout's userId in the x-esb-url — but at this point
	// we don't know it yet. The target is advisory only; use the roster page.
	if err := c.Get(ctx, path, esbRosterTarget, &p); err != nil {
		return PersonProfile{}, err
	}
	return p, nil
}

// fetchProfileByUserId calls the polymorphic /personprofile endpoint with
// a numeric userId. The response shape yields currentProgramsAndRanks.
func fetchProfileByUserId(ctx context.Context, c *Client, userId int) (PersonProfile, error) {
	var p PersonProfile
	path := fmt.Sprintf("/persons/v2/%d/personprofile", userId)
	if err := c.Get(ctx, path, esbYouthProfileTarget(userId), &p); err != nil {
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

			// Step 1: personGuid → userId.
			byGuid, err := fetchProfileByGuid(ctx, c, y.PersonGuid)
			if err != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("%s: fetch profile by guid: %w", y.FullName, err))
				mu.Unlock()
				return
			}
			if byGuid.Profile.UserId == nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("%s: profile by guid missing userId", y.FullName))
				mu.Unlock()
				return
			}
			userId := *byGuid.Profile.UserId

			// Step 2: userId → den info.
			byUserId, err := fetchProfileByUserId(ctx, c, userId)
			if err != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("%s: fetch profile by userId: %w", y.FullName, err))
				mu.Unlock()
				return
			}
			if len(byUserId.CurrentProgramsAndRanks) == 0 {
				mu.Lock()
				errs = append(errs, fmt.Errorf("%s: profile by userId has no currentProgramsAndRanks", y.FullName))
				mu.Unlock()
				return
			}
			prog := byUserId.CurrentProgramsAndRanks[0]

			s := ScoutWithDen{
				PersonGuid: y.PersonGuid,
				FullName:   y.FullName,
				FirstName:  y.FirstName,
				LastName:   y.LastName,
				UserId:     userId,
				DenType:    prog.DenType,
				DenNumber:  prog.DenNumber,
				DenId:      prog.DenId,
				RankId:     prog.RankId,
			}

			mu.Lock()
			scouts = append(scouts, s)
			mu.Unlock()
		}()
	}

	wg.Wait()
	return scouts, errs
}

// FilterByDen returns the scouts whose DenType and DenNumber match exactly.
func FilterByDen(scouts []ScoutWithDen, denType, denNumber string) []ScoutWithDen {
	var out []ScoutWithDen
	for _, s := range scouts {
		if s.DenType == denType && s.DenNumber == denNumber {
			out = append(out, s)
		}
	}
	return out
}
