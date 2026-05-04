package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"sync"

	"github.com/pkaeding/scoutbook-xls/internal/report"
	"github.com/pkaeding/scoutbook-xls/internal/scouting"
)

// defaultBaseURL is the production Scouting API host. Exposed as a variable
// (not a const) purely so tests could override if needed; normally callers set
// Config.BaseURL instead.
const defaultBaseURL = "https://api.scouting.org"

// maxConcurrency is the cap on parallel API requests per fan-out stage.
// The scouting-api skill documents 8 as a reasonable value.
const maxConcurrency = 8

// rankNameByID is a small fallback lookup for human-readable rank names. It's
// only used if the rank-requirements response didn't populate the rank name
// (which should be rare — the API does populate it). Keys come from the
// scouting-api SKILL.md rankID table.
var rankNameByID = map[int]string{
	14: "Lion",
	13: "Bobcat",
	8:  "Tiger",
	9:  "Wolf",
	10: "Bear",
	11: "Webelos",
	12: "Arrow of Light",
}

// rankIDByDenType maps the denType a scout is assigned to (what they're
// working toward) to the rankID whose requirements we should pull. A
// Webelos-den scout who hasn't earned Webelos yet still has a "current"
// rankID of Bear, so deriving the target from the scout's rankID is wrong.
// Den types correspond to the rank the den is earning.
var rankIDByDenType = map[string]int{
	"Lion":           14,
	"Tiger":          8,
	"Wolf":           9,
	"Bear":           10,
	"Webelos":        11,
	"Arrow of Light": 12,
}

// Run is the default RunnerFunc. It fetches data from the Scouting API and
// writes the XLSX file. Exported so tests and main can wire it.
func Run(ctx context.Context, cfg Config) error {
	// 1. Validate required config.
	if cfg.Token == "" {
		return errors.New("token is required (set --token, SCOUTBOOK_TOKEN, or token: in config)")
	}
	if cfg.DenType == "" {
		return errors.New("den-type is required (e.g. --den-type=Webelos)")
	}
	if cfg.DenNumber == "" {
		return errors.New("den-number is required (e.g. --den-number=1)")
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	// 2. Build the HTTP client.
	client := scouting.NewClient(baseURL, cfg.Token)

	// 3. Resolve orgGUID, either from config or via /personprofile on the
	//    logged-in user.
	orgGUID := cfg.OrgGUID
	if orgGUID == "" {
		discovered, err := discoverOrgGUID(ctx, client, cfg.Token)
		if err != nil {
			return handleTokenExpired(err)
		}
		orgGUID = discovered
	}

	// 4. Fetch roster.
	roster, err := scouting.FetchRoster(ctx, client, orgGUID)
	if err != nil {
		return handleTokenExpired(err)
	}

	// 5. Extract youth members.
	youth := scouting.ExtractYouthMembers(roster)
	if len(youth) == 0 {
		return fmt.Errorf("no youth members found in org %s", orgGUID)
	}

	// 6. Resolve each youth's den (two-step personprofile).
	scouts, resolveErrs := scouting.ResolveScoutDens(ctx, client, youth, maxConcurrency)
	for _, e := range resolveErrs {
		// If any of these errors is a token-expired, bail early with a
		// user-friendly message. Otherwise just print and move on.
		if errors.Is(e, scouting.ErrTokenExpired) {
			return handleTokenExpired(e)
		}
		fmt.Fprintf(os.Stderr, "warning: resolving scout: %v\n", e)
	}

	// 7. Filter to the requested den.
	filtered := scouting.FilterByDen(scouts, cfg.DenType, cfg.DenNumber)
	if len(filtered) == 0 {
		return fmt.Errorf("no scouts matched den %s %s; available dens: %s",
			cfg.DenType, cfg.DenNumber, summarizeDens(scouts))
	}

	// 8. Pick the target rank from the den type — i.e., what the den is
	//    working toward, not what the scouts have most recently earned.
	//    A Webelos-den scout still finishing Bear shouldn't force the whole
	//    report onto Bear requirements.
	rankID, ok := rankIDByDenType[cfg.DenType]
	if !ok {
		return fmt.Errorf("unknown den-type %q; expected one of Lion, Tiger, Wolf, Bear, Webelos, Arrow of Light", cfg.DenType)
	}
	// Still surface any scouts whose currently-earned rankID differs from the
	// den's target rank — that's a useful data-quality warning but not an error.
	for _, s := range filtered {
		if s.RankID != 0 && s.RankID != rankID {
			if name, ok := rankNameByID[s.RankID]; ok {
				fmt.Fprintf(os.Stderr, "note: %s currently on rank %s (still earning toward den rank)\n", s.FullName, name)
			}
		}
	}

	// 9. Fetch adventures + rank reqs for each filtered scout in parallel.
	type scoutFetchResult struct {
		scout          scouting.ScoutWithDen
		allAdventures  []scouting.Adventure
		rankAdventures []scouting.Adventure
		rankReqs       scouting.RankRequirements
		err            error
	}
	results := make([]scoutFetchResult, len(filtered))
	if err := runBounded(ctx, len(filtered), maxConcurrency, func(i int) error {
		s := filtered[i]
		res := scoutFetchResult{scout: s}

		advs, err := scouting.FetchAdventures(ctx, client, s.UserID)
		if err != nil {
			res.err = fmt.Errorf("fetch adventures for %s: %w", s.FullName, err)
			results[i] = res
			return nil
		}
		res.allAdventures = advs
		res.rankAdventures = scouting.FilterAdventuresByRank(advs, rankID)

		rankReqs, err := scouting.FetchRankRequirements(ctx, client, s.UserID, rankID)
		if err != nil {
			res.err = fmt.Errorf("fetch rank reqs for %s: %w", s.FullName, err)
			results[i] = res
			return nil
		}
		res.rankReqs = rankReqs

		results[i] = res
		return nil
	}); err != nil {
		return err
	}

	// Check for token expiry before moving on; it always deserves an early
	// exit with a clear message.
	for _, r := range results {
		if r.err != nil && errors.Is(r.err, scouting.ErrTokenExpired) {
			return handleTokenExpired(r.err)
		}
	}
	for _, r := range results {
		if r.err != nil {
			fmt.Fprintf(os.Stderr, "warning: %v\n", r.err)
		}
	}

	// 10. Collect the set of adventures started by any scout for this rank.
	startedAdvIDs := map[int]bool{}
	for _, r := range results {
		for _, a := range r.rankAdventures {
			if a.PercentCompleted > 0 {
				startedAdvIDs[a.AdventureID] = true
			}
		}
	}

	// 11. For each (scout, started-adventure) pair, fetch per-adventure detail
	//     in parallel. We collect pairs into a slice so we can fan-out evenly.
	//
	//     Only build pairs where THIS scout has the adventure in their own
	//     rankAdventures list. Requesting a different scout's adventure detail
	//     returns 404 from the API — the adventure set is per-scout even
	//     though the identifiers are global.
	type advPair struct {
		scoutIdx    int
		adventureID int
	}
	var pairs []advPair
	for i, r := range results {
		if r.err != nil {
			continue
		}
		scoutHas := map[int]bool{}
		for _, a := range r.rankAdventures {
			scoutHas[a.AdventureID] = true
		}
		for advID := range startedAdvIDs {
			if !scoutHas[advID] {
				continue
			}
			pairs = append(pairs, advPair{scoutIdx: i, adventureID: advID})
		}
	}
	// Deterministic ordering keeps test output stable even if map iteration
	// re-orders pair generation.
	sort.Slice(pairs, func(a, b int) bool {
		if pairs[a].scoutIdx != pairs[b].scoutIdx {
			return pairs[a].scoutIdx < pairs[b].scoutIdx
		}
		return pairs[a].adventureID < pairs[b].adventureID
	})

	advDetails := make([]map[int]scouting.AdventureRequirements, len(results))
	for i := range advDetails {
		advDetails[i] = map[int]scouting.AdventureRequirements{}
	}
	var detailsMu sync.Mutex

	if err := runBounded(ctx, len(pairs), maxConcurrency, func(pairIdx int) error {
		p := pairs[pairIdx]
		scout := results[p.scoutIdx].scout
		detail, err := scouting.FetchAdventureRequirements(ctx, client, scout.UserID, p.adventureID)
		if err != nil {
			if errors.Is(err, scouting.ErrTokenExpired) {
				return err
			}
			fmt.Fprintf(os.Stderr, "warning: fetch adventure %d for %s: %v\n",
				p.adventureID, scout.FullName, err)
			return nil
		}
		detailsMu.Lock()
		advDetails[p.scoutIdx][p.adventureID] = detail
		detailsMu.Unlock()
		return nil
	}); err != nil {
		return handleTokenExpired(err)
	}

	// 12. Build ScoutInput slice.
	scoutInputs := make([]report.ScoutInput, 0, len(results))
	for i, r := range results {
		if r.err != nil || r.scout.UserID == 0 {
			continue
		}
		scoutInputs = append(scoutInputs, report.ScoutInput{
			FirstName:     r.scout.FirstName,
			LastName:      r.scout.LastName,
			FullName:      r.scout.FullName,
			UserID:        r.scout.UserID,
			RankReqs:      r.rankReqs,
			Adventures:    r.rankAdventures,
			AdventureReqs: advDetails[i],
		})
	}

	if len(scoutInputs) == 0 {
		return errors.New("no scouts could be resolved successfully for the requested den")
	}

	// 13. Derive rank name. Prefer the name returned by the API; fall back to
	//     our static table by rankID if the API didn't populate it.
	rankName := ""
	for _, s := range scoutInputs {
		if s.RankReqs.Name != "" {
			rankName = s.RankReqs.Name
			break
		}
	}
	if rankName == "" {
		if n, ok := rankNameByID[rankID]; ok {
			rankName = n
		} else {
			rankName = fmt.Sprintf("Rank %d", rankID)
		}
	}

	// 14. Build and render the report.
	model := report.BuildReport(cfg.DenType, cfg.DenNumber, rankName, scoutInputs)
	for _, w := range model.Warnings {
		fmt.Fprintf(os.Stderr, "warning: %s\n", w)
	}

	if err := report.RenderXLSX(model, cfg.Output); err != nil {
		return fmt.Errorf("render xlsx: %w", err)
	}

	// 15. Success summary.
	fmt.Printf("Wrote %s: %d scouts, rank %q, %d adventures\n",
		cfg.Output, len(model.Scouts), model.RankName, len(model.Adventures))
	return nil
}

// discoverOrgGUID fetches the logged-in user's profile (derived from the JWT)
// and returns the single Pack organizationGuid. ErrMultiplePacks is wrapped
// with a helpful message.
func discoverOrgGUID(ctx context.Context, client *scouting.Client, token string) (string, error) {
	claims, err := scouting.ParseJWT(token)
	if err != nil {
		return "", fmt.Errorf("parse JWT: %w", err)
	}
	if claims.Pgu == "" {
		return "", errors.New("JWT missing pgu claim; cannot discover orgGUID")
	}

	var me scouting.PersonProfile
	path := "/persons/v2/" + claims.Pgu + "/personprofile"
	if err := client.Get(ctx, path, "https://advancements.scouting.org/", &me); err != nil {
		return "", err
	}

	orgGUID, err := scouting.DiscoverPackOrgGUID(me)
	if err != nil {
		if errors.Is(err, scouting.ErrMultiplePacks) {
			return "", fmt.Errorf("%w — set --org-guid to disambiguate", err)
		}
		return "", err
	}
	return orgGUID, nil
}

// handleTokenExpired prints a user-friendly message for ErrTokenExpired and
// returns the underlying error. Other errors are returned unchanged.
func handleTokenExpired(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, scouting.ErrTokenExpired) {
		fmt.Fprintln(os.Stderr,
			"error: Scouting API rejected the token (401). "+
				"Grab a fresh JWT from DevTools → Network → any XHR to api.scouting.org → copy the authorization header value.")
	}
	return err
}

// summarizeDens returns a sorted, comma-separated list of distinct
// "{DenType} {DenNumber}" pairs present in scouts. Used to make the
// "no scouts matched den" error actually helpful.
func summarizeDens(scouts []scouting.ScoutWithDen) string {
	seen := map[string]bool{}
	var labels []string
	for _, s := range scouts {
		label := fmt.Sprintf("%s %s", s.DenType, s.DenNumber)
		if !seen[label] {
			seen[label] = true
			labels = append(labels, label)
		}
	}
	sort.Strings(labels)
	if len(labels) == 0 {
		return "(none)"
	}
	out := labels[0]
	for _, l := range labels[1:] {
		out += ", " + l
	}
	return out
}

// runBounded fans out n calls of fn(i) across up to concurrency goroutines.
// It propagates ctx cancellation and returns the first non-nil error from fn.
// Uses only stdlib sync primitives (no errgroup).
func runBounded(ctx context.Context, n, concurrency int, fn func(i int) error) error {
	if n == 0 {
		return nil
	}
	if concurrency < 1 {
		concurrency = 1
	}

	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	var (
		errMu    sync.Mutex
		firstErr error
	)
	setErr := func(err error) {
		errMu.Lock()
		defer errMu.Unlock()
		if firstErr == nil {
			firstErr = err
		}
	}

schedule:
	for i := 0; i < n; i++ {
		// Check for early cancellation / recorded error before scheduling more.
		if ctx.Err() != nil {
			setErr(ctx.Err())
			break
		}
		errMu.Lock()
		stop := firstErr != nil
		errMu.Unlock()
		if stop {
			break
		}

		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			setErr(ctx.Err())
			break schedule
		}

		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			defer func() { <-sem }()
			if err := fn(idx); err != nil {
				setErr(err)
			}
		}(i)
	}

	wg.Wait()
	return firstErr
}
