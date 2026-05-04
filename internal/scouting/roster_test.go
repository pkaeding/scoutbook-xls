package scouting

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// orgGuid used across roster tests — matches the fixture shape.
const testOrgGuid = "44444444-4444-4444-4444-444444444444"

const (
	wesleyGuid   = "22222222-1111-1111-1111-222222222222"
	wesleyUserId = 20000001
	kirkGuid     = "30000001-0001-0001-0001-300000000001"
	kirkUserId   = 20000101
)

func TestFetchRoster(t *testing.T) {
	fixture := loadFixture(t, "roster_pack0123.json")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := "/organizations/positions/" + testOrgGuid
		if r.URL.Path != wantPath {
			t.Errorf("unexpected path: got %q, want %q", r.URL.Path, wantPath)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture)
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "test-token", WithRetryBaseDelay(1*time.Millisecond))

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	roster, err := FetchRoster(ctx, client, testOrgGuid)
	if err != nil {
		t.Fatalf("FetchRoster returned error: %v", err)
	}
	if len(roster.Positions) == 0 {
		t.Fatalf("roster.Positions is empty")
	}

	foundYouthMember := false
	for _, p := range roster.Positions {
		if p.PositionLong == "Youth Member" {
			foundYouthMember = true
			break
		}
	}
	if !foundYouthMember {
		t.Errorf("did not find a \"Youth Member\" position in roster.Positions")
	}
}

func TestExtractYouthMembers(t *testing.T) {
	data := loadFixture(t, "roster_pack0123.json")
	var roster Roster
	if err := json.Unmarshal(data, &roster); err != nil {
		t.Fatalf("unmarshal roster: %v", err)
	}

	youth := ExtractYouthMembers(roster)
	if got, want := len(youth), 24; got != want {
		t.Errorf("youth member count = %d, want %d", got, want)
	}

	foundWesley := false
	for _, y := range youth {
		if y.PersonGuid == wesleyGuid {
			foundWesley = true
			break
		}
	}
	if !foundWesley {
		t.Errorf("did not find Wesley in ExtractYouthMembers result")
	}
}

// buildResolveScoutDensServer constructs a dispatching httptest server for the
// polymorphic /personprofile endpoint used by ResolveScoutDens. Any paths that
// aren't explicitly handled return 404.
func buildResolveScoutDensServer(t *testing.T, wesleyGuidBody, wesleyUserIdBody []byte) *httptest.Server {
	t.Helper()

	kirkGuidBody := []byte(fmt.Sprintf(
		`{"profile":{"fullName":"James Kirk","userId":%d}}`, kirkUserId,
	))
	kirkUserIdBody := []byte(
		`{"profile":{"fullName":"James Kirk"},` +
			`"currentProgramsAndRanks":[{"denType":"Wolf","denNumber":"2","denId":99,"rankId":9}]}`,
	)

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/personprofile") {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/persons/v2/" + wesleyGuid + "/personprofile":
			_, _ = w.Write(wesleyGuidBody)
		case fmt.Sprintf("/persons/v2/%d/personprofile", wesleyUserId):
			_, _ = w.Write(wesleyUserIdBody)
		case "/persons/v2/" + kirkGuid + "/personprofile":
			_, _ = w.Write(kirkGuidBody)
		case fmt.Sprintf("/persons/v2/%d/personprofile", kirkUserId):
			_, _ = w.Write(kirkUserIdBody)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestResolveScoutDens(t *testing.T) {
	wesleyByGuid := loadFixture(t, "profile_wesley_by_guid.json")
	wesleyByUserId := loadFixture(t, "profile_wesley_by_userid.json")

	srv := buildResolveScoutDensServer(t, wesleyByGuid, wesleyByUserId)
	defer srv.Close()

	client := NewClient(srv.URL, "test-token", WithRetryBaseDelay(1*time.Millisecond))

	youth := []YouthMember{
		{FullName: "Wesley Crusher", FirstName: "Wesley", LastName: "Crusher", PersonGuid: wesleyGuid},
		{FullName: "James Kirk", FirstName: "James", LastName: "Kirk", PersonGuid: kirkGuid},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	scouts, errs := ResolveScoutDens(ctx, client, youth, 4)

	if len(errs) != 0 {
		t.Fatalf("ResolveScoutDens returned unexpected errors: %v", errs)
	}
	if got, want := len(scouts), 2; got != want {
		t.Fatalf("scouts len = %d, want %d", got, want)
	}

	byGuid := map[string]ScoutWithDen{}
	for _, s := range scouts {
		byGuid[s.PersonGuid] = s
	}

	g, ok := byGuid[wesleyGuid]
	if !ok {
		t.Fatalf("did not find Wesley in resolved scouts")
	}
	if got, want := g.UserId, wesleyUserId; got != want {
		t.Errorf("wesley.UserId = %d, want %d", got, want)
	}
	if got, want := g.DenType, "Webelos"; got != want {
		t.Errorf("wesley.DenType = %q, want %q", got, want)
	}
	if got, want := g.DenNumber, "1"; got != want {
		t.Errorf("wesley.DenNumber = %q, want %q", got, want)
	}
	if got, want := g.DenId, 99999; got != want {
		t.Errorf("wesley.DenId = %d, want %d", got, want)
	}
	if got, want := g.RankId, 10; got != want {
		t.Errorf("wesley.RankId = %d, want %d", got, want)
	}

	l, ok := byGuid[kirkGuid]
	if !ok {
		t.Fatalf("did not find Kirk in resolved scouts")
	}
	if got, want := l.UserId, kirkUserId; got != want {
		t.Errorf("kirk.UserId = %d, want %d", got, want)
	}
	if got, want := l.DenType, "Wolf"; got != want {
		t.Errorf("kirk.DenType = %q, want %q", got, want)
	}
	if got, want := l.DenNumber, "2"; got != want {
		t.Errorf("kirk.DenNumber = %q, want %q", got, want)
	}
	if got, want := l.DenId, 99; got != want {
		t.Errorf("kirk.DenId = %d, want %d", got, want)
	}
	if got, want := l.RankId, 9; got != want {
		t.Errorf("kirk.RankId = %d, want %d", got, want)
	}
}

func TestResolveScoutDensSurfacesErrorsButContinues(t *testing.T) {
	// Kirk responses — same fabricated JSON as the happy-path server.
	kirkGuidBody := []byte(fmt.Sprintf(
		`{"profile":{"fullName":"James Kirk","userId":%d}}`, kirkUserId,
	))
	kirkUserIdBody := []byte(
		`{"profile":{"fullName":"James Kirk"},` +
			`"currentProgramsAndRanks":[{"denType":"Wolf","denNumber":"2","denId":99,"rankId":9}]}`,
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/persons/v2/" + wesleyGuid + "/personprofile",
			fmt.Sprintf("/persons/v2/%d/personprofile", wesleyUserId):
			// Always fail for Wesley. Retry-exhausting 500 keeps it simple.
			w.WriteHeader(http.StatusInternalServerError)
		case "/persons/v2/" + kirkGuid + "/personprofile":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(kirkGuidBody)
		case fmt.Sprintf("/persons/v2/%d/personprofile", kirkUserId):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(kirkUserIdBody)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "test-token",
		WithRetryBaseDelay(1*time.Millisecond),
		WithMaxRetries(2),
	)

	youth := []YouthMember{
		{FullName: "Wesley Crusher", FirstName: "Wesley", LastName: "Crusher", PersonGuid: wesleyGuid},
		{FullName: "James Kirk", FirstName: "James", LastName: "Kirk", PersonGuid: kirkGuid},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	scouts, errs := ResolveScoutDens(ctx, client, youth, 2)

	// Kirk should be present with correct den data.
	var kirk *ScoutWithDen
	for i := range scouts {
		if scouts[i].PersonGuid == kirkGuid {
			kirk = &scouts[i]
			break
		}
	}
	if kirk == nil {
		t.Fatalf("Kirk missing from resolved scouts; got %+v", scouts)
	}
	if got, want := kirk.UserId, kirkUserId; got != want {
		t.Errorf("kirk.UserId = %d, want %d", got, want)
	}
	if got, want := kirk.DenType, "Wolf"; got != want {
		t.Errorf("kirk.DenType = %q, want %q", got, want)
	}
	if got, want := kirk.DenNumber, "2"; got != want {
		t.Errorf("kirk.DenNumber = %q, want %q", got, want)
	}

	// Wesley must be absent from the returned slice. We pin "absent" (not
	// "present-with-UserId==0") because the impl should skip a scout once
	// the first profile call fails — no point emitting a stub.
	for _, s := range scouts {
		if s.PersonGuid == wesleyGuid {
			t.Errorf("Wesley should be absent from resolved scouts, got %+v", s)
		}
	}

	// errors slice should reference Wesley somehow (by name).
	if len(errs) == 0 {
		t.Fatalf("expected at least one error, got none")
	}
	mentionsWesley := false
	for _, e := range errs {
		if e == nil {
			continue
		}
		if strings.Contains(e.Error(), "Wesley") {
			mentionsWesley = true
			break
		}
	}
	if !mentionsWesley {
		t.Errorf("expected at least one error mentioning \"Wesley\"; got: %v", errs)
	}
}

func TestFilterByDen(t *testing.T) {
	scouts := []ScoutWithDen{
		{FullName: "Wesley Crusher", PersonGuid: wesleyGuid, DenType: "Webelos", DenNumber: "1"},
		{FullName: "Other Webelos", PersonGuid: "11111111-1111-1111-1111-111111111111", DenType: "Webelos", DenNumber: "1"},
		{FullName: "James Kirk", PersonGuid: kirkGuid, DenType: "Wolf", DenNumber: "2"},
	}

	got := FilterByDen(scouts, "Webelos", "1")
	if want := 2; len(got) != want {
		t.Fatalf("FilterByDen len = %d, want %d (got=%+v)", len(got), want, got)
	}
	for _, s := range got {
		if s.DenType != "Webelos" || s.DenNumber != "1" {
			t.Errorf("FilterByDen returned non-matching scout: %+v", s)
		}
	}
}

func TestResolveScoutDensHonorsConcurrency(t *testing.T) {
	const concurrency = 2

	var (
		inFlight atomic.Int32
		peak     atomic.Int32
	)

	// Build six fake scouts plus the matching server responses. Each scout
	// needs both a guid-response (returning a userId) and a userId-response
	// (returning den info).
	type scoutFake struct {
		guid   string
		userId int
	}
	fakes := make([]scoutFake, 6)
	youth := make([]YouthMember, 6)
	for i := range fakes {
		fakes[i] = scoutFake{
			guid:   fmt.Sprintf("00000000-0000-0000-0000-00000000000%d", i+1),
			userId: 1000 + i,
		}
		youth[i] = YouthMember{
			FullName:   fmt.Sprintf("Scout %d", i+1),
			PersonGuid: fakes[i].guid,
		}
	}

	guidToUserId := make(map[string]int, len(fakes))
	userIdValid := make(map[int]bool, len(fakes))
	for _, f := range fakes {
		guidToUserId[f.guid] = f.userId
		userIdValid[f.userId] = true
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Track concurrent handlers.
		cur := inFlight.Add(1)
		for {
			p := peak.Load()
			if cur <= p || peak.CompareAndSwap(p, cur) {
				break
			}
		}
		defer inFlight.Add(-1)

		time.Sleep(10 * time.Millisecond)

		// Parse path: "/persons/v2/{id}/personprofile".
		const prefix = "/persons/v2/"
		const suffix = "/personprofile"
		if !strings.HasPrefix(r.URL.Path, prefix) || !strings.HasSuffix(r.URL.Path, suffix) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, prefix), suffix)

		w.Header().Set("Content-Type", "application/json")
		if uid, ok := guidToUserId[id]; ok {
			// guid-shaped response
			_, _ = fmt.Fprintf(w, `{"profile":{"fullName":"Scout","userId":%d}}`, uid)
			return
		}
		// numeric userId?
		var uid int
		if _, err := fmt.Sscanf(id, "%d", &uid); err == nil && userIdValid[uid] {
			_, _ = w.Write([]byte(
				`{"profile":{"fullName":"Scout"},` +
					`"currentProgramsAndRanks":[{"denType":"Wolf","denNumber":"2","denId":99,"rankId":9}]}`,
			))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "test-token", WithRetryBaseDelay(1*time.Millisecond))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	scouts, errs := ResolveScoutDens(ctx, client, youth, concurrency)

	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if got, want := len(scouts), len(youth); got != want {
		t.Fatalf("resolved scouts len = %d, want %d", got, want)
	}

	if got := peak.Load(); got > int32(concurrency) {
		t.Errorf("peak in-flight requests = %d, want <= %d", got, concurrency)
	}
}
