package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"
)

func TestImportCmd_Flags(t *testing.T) {
	for _, key := range []string{"SCOUTBOOK_TOKEN", "SCOUTBOOK_TROOP_ORG_GUID", "SCOUTBOOK_APPLY"} {
		t.Setenv(key, "")
	}
	t.Chdir(t.TempDir()) // no ambient scoutbook-xls.yaml

	run := func(args ...string) ImportConfig {
		t.Helper()
		var got ImportConfig
		root := NewRootCmd(nil)
		// Swap the subcommand's runner by rebuilding it.
		for _, c := range root.Commands() {
			root.RemoveCommand(c)
		}
		root.AddCommand(newImportCmd(func(_ context.Context, cfg ImportConfig) error { got = cfg; return nil }))
		root.SetArgs(append([]string{"import-blackpug"}, args...))
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
		return got
	}

	got := run("--token", "tok", "--troop-org-guid", "org", "camp.csv")
	if got.Token != "tok" || got.TroopOrgGUID != "org" || got.CSVPath != "camp.csv" || got.Apply {
		t.Errorf("got %+v; want a dry run by default", got)
	}
	if got := run("--badge", "Leatherwork,Pioneering", "--badge", "Camping", "camp.csv"); len(got.Badges) != 3 || got.Badges[2] != "Camping" {
		t.Errorf("badges = %q", got.Badges)
	}
	if got := run("--apply", "camp.csv"); !got.Apply {
		t.Error("--apply not honored")
	}

	// Env and config files must not be able to turn on writes.
	t.Setenv("SCOUTBOOK_APPLY", "true")
	if err := os.WriteFile("scoutbook-xls.yaml", []byte("apply: true\ntoken: fromfile\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got = run("camp.csv")
	if got.Apply {
		t.Error("apply was enabled by env/config; it must be flag-only")
	}
	if got.Token != "fromfile" {
		t.Errorf("token from config = %q", got.Token)
	}
}

func TestRunImport_Validation(t *testing.T) {
	jwt := strings.TrimSpace(readFixtureFile(t, "jwt.txt"))
	tests := []struct {
		name string
		cfg  ImportConfig
		want string
	}{
		{"no token", ImportConfig{TroopOrgGUID: "o", CSVPath: "x"}, "token is required"},
		{"no org", ImportConfig{Token: jwt, CSVPath: "x"}, "troop-org-guid is required"},
		{"bad token", ImportConfig{Token: "nope", TroopOrgGUID: "o", CSVPath: "x"}, "parse token"},
		{"missing csv", ImportConfig{Token: jwt, TroopOrgGUID: "o", CSVPath: filepath.Join(t.TempDir(), "nope.csv")}, "no such file"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runImport(context.Background(), tt.cfg)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want containing %q", err, tt.want)
			}
		})
	}
}

// End to end through the command: a one-scout export against a fake API,
// dry run and then apply.
func TestRunImport_EndToEnd(t *testing.T) {
	const org = "55555555-5555-5555-5555-555555555555"
	var posted []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/orgYouths"):
			_, _ = w.Write([]byte(readFixtureFile(t, "orgyouths_troop0123.json")))
		case r.URL.Path == "/advancements/v2/meritBadges":
			_, _ = w.Write([]byte(readFixtureFile(t, "meritbadges_catalog.json")))
		case r.URL.Path == "/advancements/meritBadges/71/requirements":
			_, _ = w.Write([]byte(readFixtureFile(t, "mb_71_requirements_v254.json")))
		case strings.HasSuffix(r.URL.Path, "/userRequirements"):
			_, _ = w.Write([]byte(`[{"userId":0,"memberId":130000003,"meritBadgeId":null,"versionId":null,"requirements":[]}]`))
		case r.URL.Path == "/advancements/v2/youth/meritBadges/71/requirements" && r.Method == http.MethodPost:
			if err := json.NewDecoder(r.Body).Decode(&posted); err != nil {
				t.Error(err)
			}
			_, _ = w.Write([]byte(readFixtureFile(t, "mb_71_post_response_geordi.json")))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	// A CSV with just Geordi's rows.
	var csv strings.Builder
	for i, line := range strings.Split(readFixtureFile(t, "blackpug_export_troop0123.csv"), "\n") {
		if i == 0 || strings.Contains(line, ",130000003,") {
			csv.WriteString(line + "\n")
		}
	}
	csvPath := filepath.Join(t.TempDir(), "camp.csv")
	if err := os.WriteFile(csvPath, []byte(csv.String()), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := ImportConfig{
		Token: strings.TrimSpace(readFixtureFile(t, "jwt.txt")), TroopOrgGUID: org, CSVPath: csvPath, BaseURL: srv.URL,
		Now: func() time.Time { return time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC) },
	}

	var out bytes.Buffer
	cfg.Out = &out
	if err := runImport(context.Background(), cfg); err != nil {
		t.Fatalf("dry run: %v\n%s", err, out.String())
	}
	if posted != nil || !strings.Contains(out.String(), "would record") || !strings.Contains(out.String(), "Dry run") {
		t.Errorf("dry run posted=%v out:\n%s", posted, out.String())
	}

	out.Reset()
	cfg.Apply = true
	if err := runImport(context.Background(), cfg); err != nil {
		t.Fatalf("apply: %v\n%s", err, out.String())
	}
	if len(posted) != 1 || posted[0]["userId"] != float64(20000003) {
		t.Fatalf("posted = %v", posted)
	}
	reqs := posted[0]["requirements"].([]any)
	first := reqs[0].(map[string]any)
	if len(reqs) != 13 || first["leaderApprovedUserId"] != float64(10000001) || first["leaderApprovedDate"] != "2026-11-01" || first["dateCompleted"] != "2026-07-09" {
		t.Errorf("first of %d requirements = %v", len(reqs), first)
	}
	if !strings.Contains(out.String(), "recorded") {
		t.Errorf("out:\n%s", out.String())
	}
}

func TestRunImport_ProblemsFailTheCommand(t *testing.T) {
	// Every scout is unknown to the roster, so everything is skipped; the
	// command must exit non-zero so a script doesn't think it all worked.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/orgYouths"):
			_, _ = w.Write([]byte(`{"members":[]}`))
		case r.URL.Path == "/advancements/v2/meritBadges":
			_, _ = w.Write([]byte(readFixtureFile(t, "meritbadges_catalog.json")))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	var out bytes.Buffer
	err := runImport(context.Background(), ImportConfig{
		Token: strings.TrimSpace(readFixtureFile(t, "jwt.txt")), TroopOrgGUID: "o", BaseURL: srv.URL, Out: &out,
		CSVPath: filepath.Join(findFixturesDir(t), "blackpug_export_troop0123.csv"),
	})
	if err == nil || !strings.Contains(out.String(), "not found in the unit roster") {
		t.Errorf("err=%v out:\n%s", err, out.String())
	}
}

func readFixtureFile(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(findFixturesDir(t), name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestWarnRenamedOrgGUID(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	v := viper.New()
	warnRenamedOrgGUID(v) // nothing set: silent
	v.Set("org-guid", "OLD")
	warnRenamedOrgGUID(v)
	os.Stderr = old
	_ = w.Close()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	out := buf.String()
	if strings.Count(out, "warning:") != 1 || !strings.Contains(out, "pack-org-guid") || !strings.Contains(out, "troop-org-guid") {
		t.Errorf("stderr = %q", out)
	}
}

func TestRunImport_BadgeFilter(t *testing.T) {
	// A badge filter that matches nothing is an error before any API call.
	err := runImport(context.Background(), ImportConfig{
		Token: strings.TrimSpace(readFixtureFile(t, "jwt.txt")), TroopOrgGUID: "o", BaseURL: "http://127.0.0.1:1",
		CSVPath: filepath.Join(findFixturesDir(t), "blackpug_export_troop0123.csv"), Badges: []string{"Warp Theory"},
	})
	if err == nil || !strings.Contains(err.Error(), "Warp Theory") {
		t.Errorf("err = %v", err)
	}
}

func TestImportCmd_RuntimeErrorsDontPrintUsage(t *testing.T) {
	t.Setenv("SCOUTBOOK_TOKEN", "")
	t.Chdir(t.TempDir())
	root := NewRootCmd(nil)
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"import-blackpug", "x.csv"}) // no token: a runtime error
	if err := root.Execute(); err == nil {
		t.Fatal("want error")
	}
	if strings.Contains(out.String(), "Usage:") || strings.Contains(out.String(), "Error:") {
		t.Errorf("cobra printed usage/duplicate error:\n%s", out.String())
	}
}
