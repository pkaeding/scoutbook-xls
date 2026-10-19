package cmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// recordingRunner returns a RunnerFunc that stores the Config it is invoked
// with into *capture, and the number of times it was called into *calls.
func recordingRunner(capture *Config, calls *int) RunnerFunc {
	return func(_ context.Context, cfg Config) error {
		*capture = cfg
		*calls++
		return nil
	}
}

// writeYAML writes contents to path, failing the test on error.
func writeYAML(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// clearScoutbookEnv ensures none of the SCOUTBOOK_* env vars leak in from the
// outside environment and pollute tests that want to prove env is NOT set.
// t.Setenv with "" still records a value, so we explicitly unset by setting
// empty - viper treats empty env vars as unset for GetString fallback.
func clearScoutbookEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"SCOUTBOOK_TOKEN",
		"SCOUTBOOK_ORG_GUID",
		"SCOUTBOOK_DEN_TYPE",
		"SCOUTBOOK_DEN_NUMBER",
		"SCOUTBOOK_OUTPUT",
	} {
		t.Setenv(key, "")
	}
}

// TestFlagsPopulateConfig exercises the simplest path: all values supplied via
// CLI flags land in the Config passed to the runner.
func TestFlagsPopulateConfig(t *testing.T) {
	clearScoutbookEnv(t)
	// Ensure no ambient config file is picked up from cwd.
	t.Chdir(t.TempDir())

	var got Config
	calls := 0
	cmd := NewRootCmd(recordingRunner(&got, &calls))
	cmd.SetArgs([]string{
		"--token=abc",
		"--org-guid=ORG-1",
		"--den-type=Webelos",
		"--den-number=1",
		"--output=/tmp/x.xlsx",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if calls != 1 {
		t.Fatalf("runner called %d times, want 1", calls)
	}

	want := Config{
		Token:     "abc",
		OrgGUID:   "ORG-1",
		DenType:   "Webelos",
		DenNumber: "1",
		Output:    "/tmp/x.xlsx",
	}
	if got != want {
		t.Errorf("Config = %+v, want %+v", got, want)
	}
}

// TestEnvVarsPopulateConfig: no flags, values come from SCOUTBOOK_* env vars.
func TestEnvVarsPopulateConfig(t *testing.T) {
	// Ensure no ambient config file is picked up from cwd.
	t.Chdir(t.TempDir())

	t.Setenv("SCOUTBOOK_TOKEN", "env-token")
	t.Setenv("SCOUTBOOK_ORG_GUID", "ENV-ORG")
	t.Setenv("SCOUTBOOK_DEN_TYPE", "Wolf")
	t.Setenv("SCOUTBOOK_DEN_NUMBER", "7")
	t.Setenv("SCOUTBOOK_OUTPUT", "/tmp/env.xlsx")

	var got Config
	calls := 0
	cmd := NewRootCmd(recordingRunner(&got, &calls))
	cmd.SetArgs([]string{})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if calls != 1 {
		t.Fatalf("runner called %d times, want 1", calls)
	}

	want := Config{
		Token:     "env-token",
		OrgGUID:   "ENV-ORG",
		DenType:   "Wolf",
		DenNumber: "7",
		Output:    "/tmp/env.xlsx",
	}
	if got != want {
		t.Errorf("Config = %+v, want %+v", got, want)
	}
}

// TestConfigFile: a scoutbook-xls.yaml in cwd is auto-discovered.
func TestConfigFile(t *testing.T) {
	clearScoutbookEnv(t)

	dir := t.TempDir()
	writeYAML(t, filepath.Join(dir, "scoutbook-xls.yaml"), `token: cfg-token
org-guid: CFG-ORG
den-type: Bear
den-number: "3"
output: /tmp/cfg.xlsx
`)
	t.Chdir(dir)

	var got Config
	calls := 0
	cmd := NewRootCmd(recordingRunner(&got, &calls))
	cmd.SetArgs([]string{})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if calls != 1 {
		t.Fatalf("runner called %d times, want 1", calls)
	}

	want := Config{
		Token:     "cfg-token",
		OrgGUID:   "CFG-ORG",
		DenType:   "Bear",
		DenNumber: "3",
		Output:    "/tmp/cfg.xlsx",
	}
	if got != want {
		t.Errorf("Config = %+v, want %+v", got, want)
	}
}

// TestPrecedenceFlagOverridesEnv: when both flag and env are set, flag wins.
func TestPrecedenceFlagOverridesEnv(t *testing.T) {
	t.Chdir(t.TempDir())

	t.Setenv("SCOUTBOOK_TOKEN", "env-token")
	t.Setenv("SCOUTBOOK_ORG_GUID", "ENV-ORG")
	t.Setenv("SCOUTBOOK_DEN_TYPE", "Wolf")
	t.Setenv("SCOUTBOOK_DEN_NUMBER", "7")
	t.Setenv("SCOUTBOOK_OUTPUT", "/tmp/env.xlsx")

	var got Config
	calls := 0
	cmd := NewRootCmd(recordingRunner(&got, &calls))
	cmd.SetArgs([]string{
		"--token=flag-token",
		"--org-guid=FLAG-ORG",
		"--den-type=Webelos",
		"--den-number=1",
		"--output=/tmp/flag.xlsx",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if calls != 1 {
		t.Fatalf("runner called %d times, want 1", calls)
	}

	want := Config{
		Token:     "flag-token",
		OrgGUID:   "FLAG-ORG",
		DenType:   "Webelos",
		DenNumber: "1",
		Output:    "/tmp/flag.xlsx",
	}
	if got != want {
		t.Errorf("Config = %+v, want %+v", got, want)
	}
}

// TestPrecedenceEnvOverridesConfig: with config file AND env vars set (no
// flags), env wins.
func TestPrecedenceEnvOverridesConfig(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, filepath.Join(dir, "scoutbook-xls.yaml"), `token: cfg-token
org-guid: CFG-ORG
den-type: Bear
den-number: "3"
output: /tmp/cfg.xlsx
`)
	t.Chdir(dir)

	t.Setenv("SCOUTBOOK_TOKEN", "env-token")
	t.Setenv("SCOUTBOOK_ORG_GUID", "ENV-ORG")
	t.Setenv("SCOUTBOOK_DEN_TYPE", "Wolf")
	t.Setenv("SCOUTBOOK_DEN_NUMBER", "7")
	t.Setenv("SCOUTBOOK_OUTPUT", "/tmp/env.xlsx")

	var got Config
	calls := 0
	cmd := NewRootCmd(recordingRunner(&got, &calls))
	cmd.SetArgs([]string{})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if calls != 1 {
		t.Fatalf("runner called %d times, want 1", calls)
	}

	want := Config{
		Token:     "env-token",
		OrgGUID:   "ENV-ORG",
		DenType:   "Wolf",
		DenNumber: "7",
		Output:    "/tmp/env.xlsx",
	}
	if got != want {
		t.Errorf("Config = %+v, want %+v", got, want)
	}
}

// TestDefaultOutputFilename: if --output is not provided anywhere, the default
// pattern is "{denType}-{denNumber}-progress.xlsx". We pin the exact string
// here so the implementation has to match.
func TestDefaultOutputFilename(t *testing.T) {
	clearScoutbookEnv(t)
	t.Chdir(t.TempDir())

	var got Config
	calls := 0
	cmd := NewRootCmd(recordingRunner(&got, &calls))
	cmd.SetArgs([]string{
		"--token=abc",
		"--org-guid=ORG-1",
		"--den-type=Webelos",
		"--den-number=1",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if calls != 1 {
		t.Fatalf("runner called %d times, want 1", calls)
	}

	// Pinned default pattern: "{denType}-{denNumber}-progress.xlsx".
	if got, want := got.Output, "Webelos-1-progress.xlsx"; got != want {
		t.Errorf("Config.Output = %q, want %q", got, want)
	}
}

// TestCanonicalDenType covers the lenient spellings users actually type.
func TestCanonicalDenType(t *testing.T) {
	cases := map[string]string{
		"Arrow of Light":   "Arrow of Light",
		"Arrow Of Light":   "Arrow of Light", // as the API spells it
		"arrow of light":   "Arrow of Light",
		"  Arrow of Light": "Arrow of Light",
		"webelos":          "Webelos",
		"WOLF":             "Wolf",
	}
	for in, want := range cases {
		got, ok := canonicalDenType(in)
		if !ok || got != want {
			t.Errorf("canonicalDenType(%q) = %q, %v; want %q, true", in, got, ok, want)
		}
	}

	for _, in := range []string{"", "Eagle", "Arrow of Lite"} {
		if got, ok := canonicalDenType(in); ok {
			t.Errorf("canonicalDenType(%q) = %q, true; want not found", in, got)
		}
	}
}

// TestDefaultOutputFilenameCanonicalizesDenType: a lowercase --den-type still
// produces a properly-cased default filename.
func TestDefaultOutputFilenameCanonicalizesDenType(t *testing.T) {
	clearScoutbookEnv(t)
	t.Chdir(t.TempDir())

	var got Config
	calls := 0
	cmd := NewRootCmd(recordingRunner(&got, &calls))
	cmd.SetArgs([]string{
		"--token=abc",
		"--org-guid=ORG-1",
		"--den-type=arrow Of light",
		"--den-number=1",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got, want := got.DenType, "Arrow of Light"; got != want {
		t.Errorf("Config.DenType = %q, want %q", got, want)
	}
	if got, want := got.Output, "Arrow of Light-1-progress.xlsx"; got != want {
		t.Errorf("Config.Output = %q, want %q", got, want)
	}
}

// TestRunRejectsUnknownDenType: an unrecognized den type fails before any
// network work, with a message listing the valid values.
func TestRunRejectsUnknownDenType(t *testing.T) {
	err := Run(context.Background(), Config{
		Token:     "abc",
		OrgGUID:   "ORG-1",
		DenType:   "Eagle",
		DenNumber: "1",
		BaseURL:   "http://127.0.0.1:1", // must not be dialed
	})
	if err == nil {
		t.Fatal("Run: expected error for unknown den-type, got nil")
	}
	if !strings.Contains(err.Error(), "unknown den-type") {
		t.Errorf("Run error = %v, want it to mention \"unknown den-type\"", err)
	}
}

// TestConfigFlagOverridesSearchPath: when --config points at an explicit path,
// that file must be loaded even if scoutbook-xls.yaml is present in cwd.
func TestConfigFlagOverridesSearchPath(t *testing.T) {
	clearScoutbookEnv(t)

	cwd := t.TempDir()
	// Decoy config in the default search path - should NOT be loaded.
	writeYAML(t, filepath.Join(cwd, "scoutbook-xls.yaml"), `token: decoy-token
org-guid: DECOY-ORG
den-type: Decoy
den-number: "99"
output: /tmp/decoy.xlsx
`)
	t.Chdir(cwd)

	// The real config we want the --config flag to point at.
	otherDir := t.TempDir()
	otherPath := filepath.Join(otherDir, "other.yaml")
	writeYAML(t, otherPath, `token: other-token
org-guid: OTHER-ORG
den-type: Tiger
den-number: "2"
output: /tmp/other.xlsx
`)

	var got Config
	calls := 0
	cmd := NewRootCmd(recordingRunner(&got, &calls))
	cmd.SetArgs([]string{"--config=" + otherPath})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if calls != 1 {
		t.Fatalf("runner called %d times, want 1", calls)
	}

	want := Config{
		Token:     "other-token",
		OrgGUID:   "OTHER-ORG",
		DenType:   "Tiger",
		DenNumber: "2",
		Output:    "/tmp/other.xlsx",
	}
	if got != want {
		t.Errorf("Config = %+v, want %+v", got, want)
	}
}
