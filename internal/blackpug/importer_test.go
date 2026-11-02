package blackpug

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pkaeding/scoutbook-xls/internal/scouting"
)

const (
	testOrgGUID      = "55555555-5555-5555-5555-555555555555"
	testLeaderUserID = 10000001 // uid in testdata/fixtures/jwt.txt

	geordiMember, geordiUser = "130000003", 20000003 // fresh: nothing on record
	dataMember, dataUser     = "130000004", 20000004 // everything already on record
	deannaMember, deannaUser = "130000005", 20000005 // 2a already on record
	worfMember               = "130000006"           // export has a requirement number that doesn't exist

	leatherwork2a = 7211
)

var leatherworkReqIDs = []int{7230, 7231, 7211, 7212, 7213, 7214, 7216, 7217, 7218, 7219, 7221, 7232, 7225}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(fixturePath(t, name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func fixtureRows(t *testing.T) []Row {
	t.Helper()
	rows, err := ParseCSV(bytes.NewReader(readFixture(t, "blackpug_export_troop0123.csv")))
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

// fakeAPI serves the sanitized fixtures and records what the importer writes.
type fakeAPI struct {
	t   *testing.T
	srv *httptest.Server

	mu          sync.Mutex
	posts       [][]scouting.MeritBadgeScoutEntry
	postESB     []string
	postHeaders http.Header
	progressQs  []string
	// failRequirement, if set, makes the server reject that requirement.
	failRequirement int
	// postStatus, if set, makes the POST return this HTTP status.
	postStatus int
	// postHits counts every POST to the write endpoint, including failures.
	postHits int
	// serveOldVersion also serves the 2014 version (187) with 2017's rows.
	serveOldVersion bool
	// badgeStatus is the badge status reported after a write.
	badgeStatus string
}

func newFakeAPI(t *testing.T) *fakeAPI {
	t.Helper()
	f := &fakeAPI{t: t, badgeStatus: "Leader Approved"}
	mux := http.NewServeMux()

	mux.HandleFunc("/organizations/v2/"+testOrgGUID+"/orgYouths", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "want POST", http.StatusMethodNotAllowed)
			return
		}
		_, _ = w.Write(readFixture(t, "orgyouths_troop0123.json"))
	})
	mux.HandleFunc("/advancements/v2/meritBadges", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(readFixture(t, "meritbadges_catalog.json"))
	})
	mux.HandleFunc("/advancements/meritBadges/71/requirements", func(w http.ResponseWriter, r *http.Request) {
		if v := r.URL.Query().Get("versionId"); v != "254" && !(f.serveOldVersion && v == "187") {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(readFixture(t, "mb_71_requirements_v254.json"))
	})
	mux.HandleFunc("/advancements/v2/organization/"+testOrgGUID+"/meritBadges/71/userRequirements", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.progressQs = append(f.progressQs, r.URL.Query().Get("memberId"))
		f.mu.Unlock()
		var out []map[string]any
		for _, m := range strings.Split(r.URL.Query().Get("memberId"), ",") {
			out = append(out, f.progressFor(m))
		}
		_ = json.NewEncoder(w).Encode(out)
	})
	mux.HandleFunc("/advancements/v2/youth/meritBadges/71/requirements", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.postHits++
		f.mu.Unlock()
		if f.postStatus != 0 {
			w.WriteHeader(f.postStatus)
			return
		}
		var body []scouting.MeritBadgeScoutEntry
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		f.posts = append(f.posts, body)
		f.postESB = append(f.postESB, r.Header.Get("x-esb-url"))
		f.postHeaders = r.Header.Clone()
		f.mu.Unlock()

		var out []map[string]any
		for _, e := range body {
			var reqs []map[string]any
			for _, rq := range e.Requirements {
				status := "Success"
				if rq.ID == f.failRequirement {
					status = "Failure"
				}
				reqs = append(reqs, map[string]any{"id": rq.ID, "status": status, "message": "from fake server"})
			}
			out = append(out, map[string]any{"userId": e.UserID, "meritBadgeId": 71, "percentCompleted": 1, "status": f.badgeStatus, "requirements": reqs})
		}
		_ = json.NewEncoder(w).Encode(out)
	})

	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

// progressFor builds one member's userRequirements entry.
func (f *fakeAPI) progressFor(member string) map[string]any {
	memberNum := 0
	for _, c := range member {
		memberNum = memberNum*10 + int(c-'0')
	}
	entry := map[string]any{"memberId": memberNum, "meritBadge": "Leatherwork", "requirements": []any{}}
	completed := map[int]bool{}
	switch member {
	case dataMember:
		for _, id := range leatherworkReqIDs {
			completed[id] = true
		}
	case deannaMember:
		completed[leatherwork2a] = true
	}
	if len(completed) == 0 {
		entry["userId"] = 0
		return entry // no record: meritBadgeId and versionId are null
	}
	entry["meritBadgeId"], entry["versionId"], entry["version"] = 71, 254, 2017
	var reqs []map[string]any
	for _, id := range leatherworkReqIDs {
		r := map[string]any{"id": id, "dateCompleted": nil}
		if completed[id] {
			r["dateCompleted"] = "2026-07-09"
		}
		reqs = append(reqs, r)
	}
	entry["requirements"] = reqs
	return entry
}

func (f *fakeAPI) importRows(t *testing.T, apply bool) (Report, error) {
	t.Helper()
	token := strings.TrimSpace(string(readFixture(t, "jwt.txt")))
	client := scouting.NewClient(f.srv.URL, token, scouting.WithRetryBaseDelay(time.Millisecond))
	return Import(context.Background(), client, fixtureRows(t), Options{
		OrgGUID:      testOrgGUID,
		LeaderUserID: testLeaderUserID,
		Today:        time.Date(2026, 11, 1, 20, 30, 0, 0, time.UTC),
		Apply:        apply,
	})
}

func resultFor(t *testing.T, rep Report, member string) Result {
	t.Helper()
	for _, r := range rep.Results {
		if r.MemberID == member {
			return r
		}
	}
	t.Fatalf("no result for member %s in %+v", member, rep.Results)
	return Result{}
}

func TestImport_DryRunWritesNothing(t *testing.T) {
	api := newFakeAPI(t)
	rep, err := api.importRows(t, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(api.posts) != 0 {
		t.Fatalf("dry run POSTed %d times", len(api.posts))
	}

	if r := resultFor(t, rep, geordiMember); r.Outcome != OutcomePlanned || r.Sent != 13 || r.AlreadyDone != 0 {
		t.Errorf("Geordi (nothing on record) = %+v", r)
	}
	if r := resultFor(t, rep, dataMember); r.Outcome != OutcomeAlreadyEntered || r.Total != 13 || r.Sent != 0 {
		t.Errorf("Data (fully entered; the re-run case) = %+v", r)
	}
	if r := resultFor(t, rep, deannaMember); r.Outcome != OutcomePlanned || r.Sent != 12 || r.AlreadyDone != 1 {
		t.Errorf("Deanna (2a already on record) = %+v", r)
	}
	if r := resultFor(t, rep, worfMember); r.Outcome != OutcomeSkipped || !reflect.DeepEqual(r.Unmatched, []string{"9z"}) {
		t.Errorf("Worf (bad requirement number) = %+v", r)
	}
	if r := resultFor(t, rep, "130000099"); r.Outcome != OutcomeSkipped || !strings.Contains(r.Reason, "roster") {
		t.Errorf("unknown member = %+v", r)
	}
	if !rep.Problems() {
		t.Error("Problems() = false with skipped scouts")
	}

	var awardWarning bool
	for _, w := range rep.Warnings {
		if strings.Contains(w, "Award Requirement") && strings.Contains(w, "Worf Rozhenko") && strings.Contains(w, "Kayaking BSA") {
			awardWarning = true
		}
	}
	if !awardWarning {
		t.Errorf("want a warning about ignored Kayaking BSA award rows, got %q", rep.Warnings)
	}

	// Progress is looked up once per badge, for exactly the scouts in play.
	if len(api.progressQs) != 1 || api.progressQs[0] != strings.Join([]string{geordiMember, dataMember, deannaMember, worfMember}, ",") {
		t.Errorf("progress lookups = %q", api.progressQs)
	}
}

// The strongest check on the write path: what we send for a fresh scout is
// byte-for-byte (as JSON) what the SPA sent when Leatherwork was entered by
// hand — same field set, same requirement order, same dates.
func TestImport_ApplySendsWhatTheSPASent(t *testing.T) {
	api := newFakeAPI(t)
	rep, err := api.importRows(t, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(api.posts) != 1 {
		t.Fatalf("POSTs = %d, want one per badge", len(api.posts))
	}
	body := api.posts[0]
	if len(body) != 2 || body[0].UserID != geordiUser || body[1].UserID != deannaUser {
		t.Fatalf("POST scouts = %+v; want Geordi then Deanna only (not Data, Worf or the unknown member)", body)
	}

	var want []json.RawMessage
	if err := json.Unmarshal(readFixture(t, "mb_71_post_request_geordi.json"), &want); err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(body[0])
	if !jsonEqual(t, got, want[0]) {
		t.Errorf("Geordi's entry differs from the captured SPA request\n got: %s\nwant: %s", got, want[0])
	}

	// Deanna's 2a is already on record, so it must not be re-sent.
	for _, rq := range body[1].Requirements {
		if rq.ID == leatherwork2a {
			t.Error("re-sent requirement 2a, which already has a completion date")
		}
	}
	if len(body[1].Requirements) != 12 {
		t.Errorf("Deanna requirements = %d, want 12", len(body[1].Requirements))
	}

	if want := "https://advancements.scouting.org/roster/bulk-entry-mb/" + testOrgGUID; decodeESB(t, api.postESB[0]) != want {
		t.Errorf("x-esb-url = %q, want %q", decodeESB(t, api.postESB[0]), want)
	}
	if ct := api.postHeaders.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}

	if r := resultFor(t, rep, geordiMember); r.Outcome != OutcomeApplied {
		t.Errorf("Geordi = %+v", r)
	}
	if r := resultFor(t, rep, dataMember); r.Outcome != OutcomeAlreadyEntered {
		t.Errorf("Data = %+v", r)
	}
	if r := resultFor(t, rep, worfMember); r.Outcome != OutcomeSkipped {
		t.Errorf("Worf = %+v", r)
	}
}

func TestImport_RerunAfterApplyIsNoOp(t *testing.T) {
	// Only the already-complete scout in the export: nothing to send, so no POST.
	api := newFakeAPI(t)
	token := strings.TrimSpace(string(readFixture(t, "jwt.txt")))
	client := scouting.NewClient(api.srv.URL, token)
	var rows []Row
	for _, r := range fixtureRows(t) {
		if r.MemberID == dataMember {
			rows = append(rows, r)
		}
	}
	rep, err := Import(context.Background(), client, rows, Options{OrgGUID: testOrgGUID, LeaderUserID: testLeaderUserID, Today: time.Now(), Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(api.posts) != 0 {
		t.Errorf("re-run POSTed %d times, want 0", len(api.posts))
	}
	if rep.Problems() || len(rep.Results) != 1 || rep.Results[0].Outcome != OutcomeAlreadyEntered {
		t.Errorf("report = %+v", rep)
	}
}

func TestImport_ServerRejectsARequirement(t *testing.T) {
	api := newFakeAPI(t)
	api.failRequirement = 7232
	rep, err := api.importRows(t, true)
	if err != nil {
		t.Fatal(err)
	}
	r := resultFor(t, rep, geordiMember)
	if r.Outcome != OutcomeFailed || !strings.Contains(r.Reason, "7232") {
		t.Errorf("a 200 with a failed requirement must be reported as FAILED: %+v", r)
	}
	if !rep.Problems() {
		t.Error("Problems() = false")
	}
}

func TestImport_PostFailureIsReportedNotRetried(t *testing.T) {
	api := newFakeAPI(t)
	api.postStatus = http.StatusInternalServerError
	rep, err := api.importRows(t, true)
	if err != nil {
		t.Fatal(err)
	}
	if r := resultFor(t, rep, geordiMember); r.Outcome != OutcomeFailed || !strings.Contains(r.Reason, "re-run") {
		t.Errorf("Geordi = %+v", r)
	}
	if api.postHits != 1 {
		t.Errorf("POST attempted %d times; a 5xx on a write must not be retried", api.postHits)
	}
}

func TestImport_ExpiredTokenOnWriteIsSurfaced(t *testing.T) {
	api := newFakeAPI(t)
	api.postStatus = http.StatusUnauthorized
	_, err := api.importRows(t, true)
	if !errors.Is(err, scouting.ErrTokenExpired) {
		t.Errorf("err = %v, want ErrTokenExpired", err)
	}
}

func TestImport_WarnsWhenBadgeNotCompleteAfterWrite(t *testing.T) {
	api := newFakeAPI(t)
	api.badgeStatus = "Started"
	rep, err := api.importRows(t, true)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, w := range rep.Warnings {
		if strings.Contains(w, "Geordi La Forge") && strings.Contains(w, "Started") {
			found = true
		}
	}
	if !found {
		t.Errorf("want a warning that Leatherwork didn't reach Leader Approved, got %q", rep.Warnings)
	}
}

func TestImport_VersionMismatchWithExistingRecordIsSkipped(t *testing.T) {
	api := newFakeAPI(t)
	api.serveOldVersion = true
	token := strings.TrimSpace(string(readFixture(t, "jwt.txt")))
	client := scouting.NewClient(api.srv.URL, token)
	// The export says Data earned the 2014 Leatherwork, but his record is on 2017.
	rows := []Row{{Line: 2, MemberID: dataMember, FirstName: "Data", LastName: "Soong", Type: TypeMeritBadgeRequirement,
		Advancement: "Leatherwork #1a", Version: "2014", Completed: time.Date(2026, 7, 9, 0, 0, 0, 0, time.UTC), Approved: true}}
	rep, err := Import(context.Background(), client, rows, Options{OrgGUID: testOrgGUID, LeaderUserID: 1, Today: time.Now(), Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if r := resultFor(t, rep, dataMember); r.Outcome != OutcomeSkipped || !strings.Contains(r.Reason, "different version") {
		t.Errorf("result = %+v", r)
	}
	if api.postHits != 0 {
		t.Error("wrote against a scout whose record is on a different version")
	}
}

func TestImport_UnknownBadgeAndVersionAreSkipped(t *testing.T) {
	api := newFakeAPI(t)
	token := strings.TrimSpace(string(readFixture(t, "jwt.txt")))
	client := scouting.NewClient(api.srv.URL, token)
	d := time.Date(2026, 7, 9, 0, 0, 0, 0, time.UTC)
	rows := []Row{
		{MemberID: geordiMember, FirstName: "Geordi", LastName: "La Forge", Type: TypeMeritBadgeRequirement, Advancement: "Warp Theory #1", Version: "2017", Completed: d, Approved: true},
		{MemberID: geordiMember, FirstName: "Geordi", LastName: "La Forge", Type: TypeMeritBadgeRequirement, Advancement: "Leatherwork #1a", Version: "1999", Completed: d, Approved: true},
		{MemberID: geordiMember, FirstName: "Geordi", LastName: "La Forge", Type: TypeMeritBadgeRequirement, Advancement: "Leatherwork #1b", Version: "2017", Completed: d, Approved: false},
	}
	rep, err := Import(context.Background(), client, rows, Options{OrgGUID: testOrgGUID, LeaderUserID: 1, Today: d, Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Results) != 2 {
		t.Fatalf("results = %+v", rep.Results)
	}
	for _, r := range rep.Results {
		if r.Outcome != OutcomeSkipped {
			t.Errorf("%s %s = %+v, want skipped", r.Badge, r.Version, r)
		}
	}
	if api.postHits != 0 {
		t.Error("wrote something")
	}
	var unapproved bool
	for _, w := range rep.Warnings {
		unapproved = unapproved || strings.Contains(w, "not marked approved")
	}
	if !unapproved {
		t.Errorf("want unapproved-row warning, got %q", rep.Warnings)
	}
}

func TestImport_NoRequirementRowsMakesNoCalls(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = io.WriteString(w, "{}")
	}))
	defer srv.Close()
	client := scouting.NewClient(srv.URL, "x")
	rep, err := Import(context.Background(), client, nil, Options{OrgGUID: testOrgGUID})
	if err != nil || hits != 0 || len(rep.Results) != 0 {
		t.Errorf("err=%v hits=%d results=%d", err, hits, len(rep.Results))
	}
}

func TestReport_Write(t *testing.T) {
	api := newFakeAPI(t)
	rep, err := api.importRows(t, false)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	rep.Write(&buf, false)
	out := buf.String()
	for _, want := range []string{"Geordi La Forge", "Leatherwork (2017)", "would record", "already entered", "unmatched requirement numbers: 9z", "Dry run"} {
		if !strings.Contains(out, want) {
			t.Errorf("report missing %q:\n%s", want, out)
		}
	}
}

func jsonEqual(t *testing.T, a, b []byte) bool {
	t.Helper()
	var av, bv any
	if err := json.Unmarshal(a, &av); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &bv); err != nil {
		t.Fatal(err)
	}
	return reflect.DeepEqual(av, bv)
}

func decodeESB(t *testing.T, v string) string {
	t.Helper()
	b, err := base64.StdEncoding.DecodeString(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// The SPA never sends parent requirements (7210 "Explain the following" etc.),
// but Black Pug exports do list them (e.g. First Aid "10." beside 10(a)..).
func TestImport_ParentRequirementsAreNotSent(t *testing.T) {
	api := newFakeAPI(t)
	token := strings.TrimSpace(string(readFixture(t, "jwt.txt")))
	client := scouting.NewClient(api.srv.URL, token)
	d := time.Date(2026, 7, 9, 0, 0, 0, 0, time.UTC)
	mk := func(num string) Row {
		return Row{MemberID: geordiMember, FirstName: "Geordi", LastName: "La Forge", Type: TypeMeritBadgeRequirement,
			Advancement: "Leatherwork #" + num, Version: "2017", Completed: d, Approved: true}
	}
	// "2" is a parent with children 2a-2d; "3" is a parent listed with none of its children.
	rows := []Row{mk("2"), mk("2a"), mk("2b"), mk("3"), mk("4")}
	rep, err := Import(context.Background(), client, rows, Options{OrgGUID: testOrgGUID, LeaderUserID: 1, Today: d, Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	r := resultFor(t, rep, geordiMember)
	if r.Outcome != OutcomeApplied || r.Sent != 3 || r.ParentsSkipped != 2 {
		t.Fatalf("result = %+v; want applied, 3 sent (2a, 2b, 4), 2 parents skipped", r)
	}
	var sent []int
	for _, rq := range api.posts[0][0].Requirements {
		sent = append(sent, rq.ID)
	}
	if !reflect.DeepEqual(sent, []int{7211, 7212, 7232}) {
		t.Errorf("sent requirement IDs = %v; parents 7210/7215 must be left out", sent)
	}
	var orphan bool
	for _, w := range rep.Warnings {
		orphan = orphan || (strings.Contains(w, "parent requirement 3") && strings.Contains(w, "without any of its sub-requirements"))
	}
	if !orphan {
		t.Errorf("want a warning about parent 3 listed without children, got %q", rep.Warnings)
	}

	// Only parents listed: nothing to record, and not misreported as done.
	api2 := newFakeAPI(t)
	client2 := scouting.NewClient(api2.srv.URL, token)
	rep, err = Import(context.Background(), client2, []Row{mk("2")}, Options{OrgGUID: testOrgGUID, LeaderUserID: 1, Today: d, Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if r := resultFor(t, rep, geordiMember); r.Outcome != OutcomeSkipped || api2.postHits != 0 {
		t.Errorf("parents-only = %+v, posts=%d", r, api2.postHits)
	}
}

func TestReport_WriteAlignsColumnsToLongestNames(t *testing.T) {
	rep := Report{Results: []Result{
		{Scout: "Data Soong", Badge: "Signs, Signals, and Codes", Version: "2026", Outcome: OutcomePlanned, Sent: 22},
		{Scout: "Geordi La Forge", Badge: "Archery", Version: "2025", Outcome: OutcomeAlreadyEntered, Total: 3},
	}}
	var buf bytes.Buffer
	rep.Write(&buf, true)
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	col := func(line string) int { return strings.Index(line, "would record") }
	if col(lines[0]) < 0 {
		t.Fatalf("output:\n%s", buf.String())
	}
	if a, b := col(lines[0]), strings.Index(lines[1], "already entered"); a != b {
		t.Errorf("outcome column misaligned (%d vs %d):\n%s", a, b, buf.String())
	}
}
