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
	"testing"

	excelize "github.com/xuri/excelize/v2"
)

// TestRun_EndToEnd_Wesley exercises Run against an httptest.Server serving
// the sanitized fixtures. Only one scout (Wesley Crusher) is fully served;
// other scouts deliberately fail their profile-by-guid lookup with 404, and
// we assert Run handles that gracefully (skips them, continues, produces a
// valid XLSX).
func TestRun_EndToEnd_Wesley(t *testing.T) {
	// Unset any ambient SCOUTBOOK_* env vars that could override cfg.
	for _, key := range []string{
		"SCOUTBOOK_TOKEN", "SCOUTBOOK_ORG_GUID", "SCOUTBOOK_DEN_TYPE",
		"SCOUTBOOK_DEN_NUMBER", "SCOUTBOOK_OUTPUT",
	} {
		t.Setenv(key, "")
	}

	const (
		myPgu       = "11111111-1111-1111-1111-111111111111"
		packOrgGUID = "44444444-4444-4444-4444-444444444444"
		wesleyGUID  = "22222222-1111-1111-1111-222222222222"
		wesleyUID   = "20000001"
	)

	fixturesDir := findFixturesDir(t)
	jwtPath := filepath.Join(fixturesDir, "jwt.txt")
	jwtBytes, err := os.ReadFile(jwtPath)
	if err != nil {
		t.Fatalf("read jwt fixture: %v", err)
	}
	token := strings.TrimSpace(string(jwtBytes))

	// Regexes for dynamic stub handlers:
	//  * Any /advancements/v2/youth/{uid}/adventures/{adv}/requirements
	//    path — Wesley has many started adventures but we only have one
	//    real adventure fixture (140). A minimal stub is fine for the rest.
	//  * Any /advancements/v2/youth/{uid}/ranks/{rid}/requirements path —
	//    Wesley's currentProgramsAndRanks has rankID=10 (still on Bear
	//    while in the Webelos den), so the live request is for rank 10
	//    even though our only rank fixture is Webelos. Serve that fixture
	//    regardless so the test isn't tightly coupled to the fixture's
	//    specific rankID.
	advReqRe := regexp.MustCompile(`^/advancements/v2/youth/(\d+)/adventures/(\d+)/requirements$`)
	rankReqRe := regexp.MustCompile(`^/advancements/v2/youth/(\d+)/ranks/(\d+)/requirements$`)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// Discovery: /persons/v2/{pgu}/personprofile.
		switch path {
		case "/persons/v2/" + myPgu + "/personprofile":
			serveFixture(t, w, fixturesDir, "me_profile.json")
			return
		case "/organizations/positions/" + packOrgGUID:
			serveFixture(t, w, fixturesDir, "roster_pack0123.json")
			return
		case "/persons/v2/" + wesleyGUID + "/personprofile":
			serveFixture(t, w, fixturesDir, "profile_wesley_by_guid.json")
			return
		case "/persons/v2/" + wesleyUID + "/personprofile":
			serveFixture(t, w, fixturesDir, "profile_wesley_by_userid.json")
			return
		case "/advancements/v2/youth/" + wesleyUID + "/adventures":
			serveFixture(t, w, fixturesDir, "adventures_wesley.json")
			return
		case "/advancements/v2/youth/" + wesleyUID + "/adventures/140/requirements":
			serveFixture(t, w, fixturesDir, "adventure_140_myfamily_wesley.json")
			return
		}

		// Rank requirements for any rankID — serve the Webelos fixture.
		if m := rankReqRe.FindStringSubmatch(path); m != nil && m[1] == wesleyUID {
			serveFixture(t, w, fixturesDir, "rank_webelos_wesley.json")
			return
		}

		// Any other per-adventure requirements path: return a minimal stub
		// so the builder has something to iterate (but with no real rows).
		if m := advReqRe.FindStringSubmatch(path); m != nil {
			advID := m[2]
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w,
				`{"adventureId":%s,"adventureName":"Stub","rankId":11,"percentCompleted":0,"status":"","requirements":[]}`,
				advID)
			return
		}

		// Any other /persons/v2/.../personprofile belongs to a non-Wesley
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
	defer srv.Close()

	outDir := t.TempDir()
	outPath := filepath.Join(outDir, "wesley-report.xlsx")

	cfg := Config{
		Token:     token,
		DenType:   "Webelos",
		DenNumber: "1",
		Output:    outPath,
		BaseURL:   srv.URL,
		// OrgGUID intentionally left empty so the auto-discovery path runs.
	}

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
