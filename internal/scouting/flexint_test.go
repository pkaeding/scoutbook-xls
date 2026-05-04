package scouting

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFlexInt_Number(t *testing.T) {
	var f flexInt
	if err := json.Unmarshal([]byte("42"), &f); err != nil {
		t.Fatalf("unmarshal 42: %v", err)
	}
	if f.Int() != 42 {
		t.Errorf("flexInt(42).Int() = %d, want 42", f.Int())
	}
}

func TestFlexInt_String(t *testing.T) {
	var f flexInt
	if err := json.Unmarshal([]byte(`"42"`), &f); err != nil {
		t.Fatalf(`unmarshal "42": %v`, err)
	}
	if f.Int() != 42 {
		t.Errorf(`flexInt("42").Int() = %d, want 42`, f.Int())
	}
}

func TestFlexInt_Null(t *testing.T) {
	var f flexInt
	if err := json.Unmarshal([]byte("null"), &f); err != nil {
		t.Fatalf("unmarshal null: %v", err)
	}
	if f.Int() != 0 {
		t.Errorf("flexInt(null).Int() = %d, want 0", f.Int())
	}
}

func TestFlexInt_EmptyString(t *testing.T) {
	var f flexInt
	if err := json.Unmarshal([]byte(`""`), &f); err != nil {
		t.Fatalf(`unmarshal "": %v`, err)
	}
	if f.Int() != 0 {
		t.Errorf(`flexInt("").Int() = %d, want 0`, f.Int())
	}
}

func TestFlexInt_InvalidString(t *testing.T) {
	var f flexInt
	err := json.Unmarshal([]byte(`"not-a-number"`), &f)
	if err == nil {
		t.Fatalf(`unmarshal "not-a-number" succeeded; want error`)
	}
}

// TestProgramAndRank_RankIdAsString verifies ProgramAndRank tolerates the
// polymorphic rankId/denId values observed in the wild: numbers in some
// responses, quoted numeric strings in others.
func TestProgramAndRank_RankIdAsString(t *testing.T) {
	cases := []struct {
		name, body string
		wantRank   int
		wantDen    int
	}{
		{
			name:     "numeric",
			body:     `{"denType":"Webelos","denNumber":"1","denId":99999,"rankId":11}`,
			wantRank: 11,
			wantDen:  99999,
		},
		{
			name:     "string",
			body:     `{"denType":"Webelos","denNumber":"1","denId":"99999","rankId":"11"}`,
			wantRank: 11,
			wantDen:  99999,
		},
		{
			name:     "mixed",
			body:     `{"denType":"Wolf","denNumber":"2","denId":"501","rankId":9}`,
			wantRank: 9,
			wantDen:  501,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var p ProgramAndRank
			if err := json.Unmarshal([]byte(tc.body), &p); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if p.RankId != tc.wantRank {
				t.Errorf("RankId = %d, want %d", p.RankId, tc.wantRank)
			}
			if p.DenId != tc.wantDen {
				t.Errorf("DenId = %d, want %d", p.DenId, tc.wantDen)
			}
			// DenType and DenNumber should survive unmarshal through the shadow.
			if p.DenType == "" {
				t.Errorf("DenType empty")
			}
			if p.DenNumber == "" {
				t.Errorf("DenNumber empty")
			}
		})
	}
}

// TestProgramAndRank_InvalidRankIdStringPropagates verifies we get a clear
// error for unparseable rankId strings rather than silently defaulting.
func TestProgramAndRank_InvalidRankIdStringPropagates(t *testing.T) {
	var p ProgramAndRank
	err := json.Unmarshal([]byte(`{"rankId":"bogus"}`), &p)
	if err == nil {
		t.Fatalf("expected error on bogus rankId, got nil")
	}
	if !strings.Contains(err.Error(), "bogus") {
		t.Errorf("error = %q, want to mention %q", err.Error(), "bogus")
	}
}
