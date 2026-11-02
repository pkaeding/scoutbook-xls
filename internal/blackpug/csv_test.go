package blackpug

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixturePath(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join("..", "..", "testdata", "fixtures", name)
}

func TestParseCSV_Fixture(t *testing.T) {
	f, err := os.Open(fixturePath(t, "blackpug_export_troop0123.csv"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	rows, err := ParseCSV(f)
	if err != nil {
		t.Fatalf("ParseCSV: %v", err)
	}
	if len(rows) != 48 {
		t.Fatalf("rows = %d, want 48", len(rows))
	}
	first := rows[0]
	if first.Scout() != "Geordi La Forge" || first.MemberID != "130000003" {
		t.Errorf("first row scout = %q / %q", first.Scout(), first.MemberID)
	}
	badge, req := first.BadgeAndRequirement()
	if badge != "Leatherwork" || req != "1a" {
		t.Errorf("BadgeAndRequirement = %q, %q", badge, req)
	}
	if got := first.Completed.Format("2006-01-02"); got != "2026-07-09" {
		t.Errorf("Completed = %s, want 2026-07-09 (MM-DD-YYYY input)", got)
	}
	if !first.Approved || first.Awarded {
		t.Errorf("Approved/Awarded = %v/%v, want true/false", first.Approved, first.Awarded)
	}
}

func TestParseCSV_ColumnOrderAndBOM(t *testing.T) {
	in := "\xef\xbb\xbfAdvancement,Approved,BSA Member ID,First Name,Last Name,Advancement Type,Version,Date Completed\n" +
		"\"Signs, Signals, and Codes #4(c)(1)\",1,130000003,Geordi,La Forge,Merit Badge Requirement,2026,07-10-2026\n"
	rows, err := ParseCSV(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	badge, req := rows[0].BadgeAndRequirement()
	if len(rows) != 1 || badge != "Signs, Signals, and Codes" || req != "4(c)(1)" {
		t.Errorf("got %d rows, badge %q req %q", len(rows), badge, req)
	}
}

func TestParseCSV_Errors(t *testing.T) {
	tests := map[string]string{
		"empty":          "",
		"missing column": "BSA Member ID,First Name\n1,2\n",
		"bad date": "BSA Member ID,First Name,Last Name,Advancement Type,Advancement,Version,Date Completed,Approved\n" +
			"1,A,B,Merit Badge,X,2020,2026-07-09,1\n",
	}
	for name, in := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseCSV(strings.NewReader(in)); err == nil {
				t.Error("want error, got nil")
			}
		})
	}
}

func TestFilterBadges(t *testing.T) {
	f, err := os.Open(fixturePath(t, "blackpug_export_troop0123.csv"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	rows, err := ParseCSV(f)
	if err != nil {
		t.Fatal(err)
	}

	got, err := FilterBadges(rows, []string{"leatherwork"})
	if err != nil {
		t.Fatal(err)
	}
	// 3 scouts x (13 requirements + 1 badge row) + Worf's 3 requirement rows + Q's 1.
	if len(got) != 3*14+3+1 {
		t.Errorf("leatherwork rows = %d", len(got))
	}
	for _, r := range got {
		if b, _ := r.BadgeAndRequirement(); b != "Leatherwork" && r.Advancement != "Leatherwork" {
			t.Errorf("kept %q", r.Advancement)
		}
	}

	if got, err := FilterBadges(rows, nil); err != nil || len(got) != len(rows) {
		t.Errorf("no filter should keep everything: %d, %v", len(got), err)
	}
	// Award rows are selectable by their own name.
	if got, err := FilterBadges(rows, []string{"Kayaking BSA"}); err != nil || len(got) != 2 {
		t.Errorf("Kayaking BSA rows = %d, %v", len(got), err)
	}
	if _, err := FilterBadges(rows, []string{"Leatherwork", "Warp Theory"}); err == nil || !strings.Contains(err.Error(), "Warp Theory") {
		t.Errorf("err = %v, want it to name the unmatched badge", err)
	}
}
