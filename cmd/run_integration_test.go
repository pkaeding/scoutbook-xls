package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	excelize "github.com/xuri/excelize/v2"
)

const (
	fixtureMyPgu       = "11111111-1111-1111-1111-111111111111"
	fixturePackOrgGUID = "44444444-4444-4444-4444-444444444444"
	fixtureWesleyGUID  = "22222222-1111-1111-1111-222222222222"
	fixtureWesleyUID   = "20000001"

	// A second resolvable scout, derived from Wesley's fixtures. The GUID is
	// Spock Vulcan's, as it appears in roster_pack0123.json.
	fixtureSpockGUID = "30000004-0004-0004-0004-300000000004"
	fixtureSpockUID  = "20000002"
)

// runFixtureOpts customizes the fake Scouting API built by newRunFixture.
type runFixtureOpts struct {
	// SecondScout also resolves Spock Vulcan, serving him the same profile
	// shape, adventure list and rank requirements as Wesley.
	SecondScout bool
	// AdventuresJSON overrides the adventure list served to every resolved
	// scout. Nil serves adventures_wesley.json.
	AdventuresJSON []byte
	// AdventureRequirements returns the JSON array for the "requirements"
	// field of a per-adventure response, for any adventure other than the
	// real fixture (140, Wesley only). Nil, or an empty return, serves [].
	// It is called from server goroutines, so it must be safe for
	// concurrent use.
	AdventureRequirements func(userID string, adventureID int) string
}

// newRunFixture starts an httptest server that serves the sanitized
// fixtures and returns a Config pointed at it, with the discovery path
// (empty OrgGUID) and Webelos den 1 selected. Every other roster scout
// 404s on profile lookup, which Run must skip gracefully.
func newRunFixture(t *testing.T, opts runFixtureOpts) Config {
	t.Helper()
	// Unset any ambient SCOUTBOOK_* env vars that could override cfg.
	for _, key := range []string{
		"SCOUTBOOK_TOKEN", "SCOUTBOOK_ORG_GUID", "SCOUTBOOK_DEN_TYPE",
		"SCOUTBOOK_DEN_NUMBER", "SCOUTBOOK_OUTPUT",
	} {
		t.Setenv(key, "")
	}

	fixturesDir := findFixturesDir(t)
	read := func(name string) []byte {
		b, err := os.ReadFile(filepath.Join(fixturesDir, name))
		if err != nil {
			t.Fatalf("read fixture %s: %v", name, err)
		}
		return b
	}
	token := strings.TrimSpace(string(read("jwt.txt")))

	adventures := opts.AdventuresJSON
	if adventures == nil {
		adventures = read("adventures_wesley.json")
	}

	// Per-scout profile bodies, keyed by request path.
	profiles := map[string][]byte{
		"/persons/v2/" + fixtureWesleyGUID + "/personprofile": read("profile_wesley_by_guid.json"),
		"/persons/v2/" + fixtureWesleyUID + "/personprofile":  read("profile_wesley_by_userid.json"),
	}
	scoutUIDs := []string{fixtureWesleyUID}
	if opts.SecondScout {
		spock := strings.NewReplacer(
			"Wesley Crusher", "Spock Vulcan",
			"Wesley", "Spock",
			"Crusher", "Vulcan",
			fixtureWesleyGUID, fixtureSpockGUID,
			fixtureWesleyUID, fixtureSpockUID,
			"130000002", "130000003",
			"40000002", "40000003",
		)
		profiles["/persons/v2/"+fixtureSpockGUID+"/personprofile"] =
			[]byte(spock.Replace(string(read("profile_wesley_by_guid.json"))))
		profiles["/persons/v2/"+fixtureSpockUID+"/personprofile"] =
			[]byte(spock.Replace(string(read("profile_wesley_by_userid.json"))))
		scoutUIDs = append(scoutUIDs, fixtureSpockUID)
	}

	// Any /advancements/v2/youth/{uid}/ranks/{rid}/requirements path:
	// Wesley's currentProgramsAndRanks has rankID=10 (still on Bear while in
	// the Webelos den), so the live request is for rank 10 even though our
	// only rank fixture is Webelos. Serve that fixture regardless so the
	// test isn't tightly coupled to the fixture's specific rankID.
	advReqRe := regexp.MustCompile(`^/advancements/v2/youth/(\d+)/adventures/(\d+)/requirements$`)
	rankReqRe := regexp.MustCompile(`^/advancements/v2/youth/(\d+)/ranks/(\d+)/requirements$`)
	isScout := func(uid string) bool {
		for _, u := range scoutUIDs {
			if u == uid {
				return true
			}
		}
		return false
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		serveJSON := func(b []byte) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(b)
		}

		switch path {
		case "/persons/v2/" + fixtureMyPgu + "/personprofile":
			serveFixture(t, w, fixturesDir, "me_profile.json")
			return
		case "/organizations/positions/" + fixturePackOrgGUID:
			serveFixture(t, w, fixturesDir, "roster_pack0123.json")
			return
		}
		if b, ok := profiles[path]; ok {
			serveJSON(b)
			return
		}
		if m := regexp.MustCompile(`^/advancements/v2/youth/(\d+)/adventures$`).FindStringSubmatch(path); m != nil && isScout(m[1]) {
			serveJSON(adventures)
			return
		}
		if m := rankReqRe.FindStringSubmatch(path); m != nil && isScout(m[1]) {
			serveFixture(t, w, fixturesDir, "rank_webelos_wesley.json")
			return
		}
		if m := advReqRe.FindStringSubmatch(path); m != nil && isScout(m[1]) {
			// Wesley has many started adventures but we only have one real
			// adventure fixture (140). A minimal stub is fine for the rest.
			if m[1] == fixtureWesleyUID && m[2] == "140" {
				serveFixture(t, w, fixturesDir, "adventure_140_myfamily_wesley.json")
				return
			}
			var advID int
			_, _ = fmt.Sscan(m[2], &advID)
			reqs := "[]"
			if opts.AdventureRequirements != nil {
				if got := opts.AdventureRequirements(m[1], advID); got != "" {
					reqs = got
				}
			}
			serveJSON([]byte(fmt.Sprintf(
				`{"adventureId":%d,"adventureName":"Stub","rankId":11,"percentCompleted":0,"status":"","requirements":%s}`,
				advID, reqs)))
			return
		}

		// Any other /persons/v2/.../personprofile belongs to a non-fixture
		// scout in the roster. Returning 404 is fast (non-retryable) and
		// the orchestrator reports a warning and skips the scout.
		if strings.HasPrefix(path, "/persons/v2/") && strings.HasSuffix(path, "/personprofile") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		// Anything else is unexpected for this test — fail loudly.
		t.Errorf("unexpected request: %s %s", r.Method, path)
		http.Error(w, "unexpected", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	return Config{
		Token:     token,
		DenType:   "Webelos",
		DenNumber: "1",
		Output:    filepath.Join(t.TempDir(), "report.xlsx"),
		BaseURL:   srv.URL,
		// OrgGUID intentionally left empty so the auto-discovery path runs.
	}
}

// TestRun_EndToEnd_Wesley exercises Run against an httptest.Server serving
// the sanitized fixtures. Only one scout (Wesley Crusher) is fully served;
// other scouts deliberately fail their profile-by-guid lookup with 404, and
// we assert Run handles that gracefully (skips them, continues, produces a
// valid XLSX).
func TestRun_EndToEnd_Wesley(t *testing.T) {
	cfg := newRunFixture(t, runFixtureOpts{})
	outPath := cfg.Output

	if err := Run(context.Background(), cfg); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	// The output file should exist.
	info, err := os.Stat(outPath)
	if err != nil {
		t.Fatalf("stat output: %v", err)
	}
	if info.Size() == 0 {
		t.Fatalf("output file is empty")
	}

	// Open the XLSX and make structural assertions.
	f, err := excelize.OpenFile(outPath)
	if err != nil {
		t.Fatalf("open xlsx: %v", err)
	}
	defer func() { _ = f.Close() }()

	// The summary sheet should be named after the den label "Webelos 1".
	sheets := f.GetSheetList()
	foundSummary := false
	for _, s := range sheets {
		if s == "Webelos 1" {
			foundSummary = true
			break
		}
	}
	if !foundSummary {
		t.Errorf("summary sheet %q not found; sheets = %v", "Webelos 1", sheets)
	}

	// Wesley is the only successfully-resolved scout, so his FirstName
	// should be in B1 (column B = first scout column) of the summary sheet.
	got, err := f.GetCellValue("Webelos 1", "B1")
	if err != nil {
		t.Fatalf("read B1: %v", err)
	}
	if got != "Wesley" {
		t.Errorf("Webelos 1!B1 = %q, want %q", got, "Wesley")
	}

	// The summary should have at least the two section-header rows plus
	// some real data rows (rank reqs and at least one adventure row). We
	// just assert there's *any* row beyond the header.
	rows, err := f.GetRows("Webelos 1")
	if err != nil {
		t.Fatalf("get rows: %v", err)
	}
	if len(rows) < 3 {
		t.Errorf("summary sheet has %d rows, want at least 3", len(rows))
	}

	// At least one per-adventure sheet should be present. Because Wesley
	// has 18 started Webelos adventures, we expect many — just require >0.
	if len(sheets) < 2 {
		t.Errorf("expected at least one per-adventure sheet in addition to summary; got %v", sheets)
	}
}

// findFixturesDir walks up from the test's working directory until it finds
// the testdata/fixtures dir. Tests run with CWD = package dir, so the
// fixtures are one level up.
func findFixturesDir(t *testing.T) string {
	t.Helper()
	// cmd/ is one level below the project root.
	candidates := []string{
		filepath.Join("..", "testdata", "fixtures"),
		filepath.Join("testdata", "fixtures"),
	}
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && info.IsDir() {
			abs, err := filepath.Abs(c)
			if err == nil {
				return abs
			}
			return c
		}
	}
	t.Fatalf("could not locate testdata/fixtures; tried %v", candidates)
	return ""
}

// serveFixture writes the named file's contents to w with a JSON content-type.
func serveFixture(t *testing.T, w http.ResponseWriter, dir, name string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Errorf("read fixture %s: %v", name, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Sanity check: make sure the file is valid JSON.
	var any any
	if err := json.Unmarshal(b, &any); err != nil {
		t.Errorf("invalid JSON in fixture %s: %v", name, err)
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(b)
}

// TestRun_RequirementsForLinkedAdventureNobodyStarted pins that an adventure
// linked from a rank requirement still gets its per-scout requirement lists
// fetched when no scout has started it (so its tab is populated), and that a
// scout's own completion shows up there. An unstarted adventure that no rank
// requirement links to, such as an elective, is not fetched at all.
func TestRun_RequirementsForLinkedAdventureNobodyStarted(t *testing.T) {
	const (
		// Reset to 0% in the served adventure list: "My Community"
		// (linked from the Webelos rank requirements) and "Archery
		// (Webelos)" (an elective, not linked).
		unstartedLinkedID   = 139
		unstartedElectiveID = 130
	)

	fixturesDir := findFixturesDir(t)
	raw, err := os.ReadFile(filepath.Join(fixturesDir, "adventures_wesley.json"))
	if err != nil {
		t.Fatalf("read adventures fixture: %v", err)
	}
	var advs []map[string]any
	if err := json.Unmarshal(raw, &advs); err != nil {
		t.Fatalf("parse adventures fixture: %v", err)
	}
	for _, a := range advs {
		id, _ := a["adventureId"].(float64)
		if int(id) == unstartedLinkedID || int(id) == unstartedElectiveID {
			a["percentCompleted"] = 0.0
			a["dateCompleted"] = nil
			a["status"] = ""
		}
	}
	advsJSON, err := json.Marshal(advs)
	if err != nil {
		t.Fatalf("marshal adventures: %v", err)
	}

	type fetchKey struct {
		userID string
		advID  int
	}
	var mu sync.Mutex
	fetched := map[fetchKey]int{}

	cfg := newRunFixture(t, runFixtureOpts{
		SecondScout:    true,
		AdventuresJSON: advsJSON,
		AdventureRequirements: func(userID string, advID int) string {
			mu.Lock()
			fetched[fetchKey{userID, advID}]++
			mu.Unlock()
			// Only Spock has a dated completion of the requirement, so the
			// tab must be built from a per-scout response.
			completed := "null"
			if userID == fixtureSpockUID {
				completed = `"2026-09-01"`
			}
			return `[{"requirementId":9001,"requirementNumber":"1","requirementName":"Stub requirement","sortOrder":1,` +
				`"isCompleted":false,"percentCompleted":0,"dateCompleted":` + completed + `}]`
		},
	})

	if err := Run(context.Background(), cfg); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	mu.Lock()
	for _, uid := range []string{fixtureWesleyUID, fixtureSpockUID} {
		if got := fetched[fetchKey{uid, unstartedLinkedID}]; got != 1 {
			t.Errorf("scout %s: unstarted linked adventure %d fetched %d times, want 1", uid, unstartedLinkedID, got)
		}
		if got := fetched[fetchKey{uid, unstartedElectiveID}]; got != 0 {
			t.Errorf("scout %s: unstarted elective %d fetched %d times, want 0", uid, unstartedElectiveID, got)
		}
	}
	mu.Unlock()

	f, err := excelize.OpenFile(cfg.Output)
	if err != nil {
		t.Fatalf("open xlsx: %v", err)
	}
	defer func() { _ = f.Close() }()

	sheet := ""
	for _, name := range f.GetSheetList() {
		if strings.Contains(name, "My Community") {
			sheet = name
		}
	}
	if sheet == "" {
		t.Fatalf("no sheet for the unstarted linked adventure; sheets = %v", f.GetSheetList())
	}
	rows, err := f.GetRows(sheet)
	if err != nil {
		t.Fatalf("get rows: %v", err)
	}
	if len(rows) < 2 {
		t.Fatalf("sheet %q has %d rows, want a header and a requirement row; rows = %q", sheet, len(rows), rows)
	}
	// Header: Requirement, then one column per scout (sorted by name).
	header := strings.Join(rows[0], "|")
	if header != "Requirement|Spock|Wesley" {
		t.Errorf("header = %q, want %q", header, "Requirement|Spock|Wesley")
	}
	// Requirement row: Spock's completion date, Wesley's cell empty.
	wantRow := []string{"1 — Stub requirement", "2026-09-01"}
	if got := rows[1]; len(got) != len(wantRow) || got[0] != wantRow[0] || got[1] != wantRow[1] {
		t.Errorf("requirement row = %q, want %q (Wesley's cell empty)", rows[1], wantRow)
	}
}
