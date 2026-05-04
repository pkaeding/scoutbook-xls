package scouting

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestParseJWT(t *testing.T) {
	data := loadFixture(t, "jwt.txt")
	token := strings.TrimSpace(string(data))
	token = strings.TrimPrefix(token, "Bearer ")

	claims, err := ParseJWT(token)
	if err != nil {
		t.Fatalf("ParseJWT: unexpected error: %v", err)
	}

	if got, want := claims.Pgu, "11111111-1111-1111-1111-111111111111"; got != want {
		t.Errorf("claims.Pgu = %q, want %q", got, want)
	}
	if got, want := claims.UID, 10000001; got != want {
		t.Errorf("claims.UID = %d, want %d", got, want)
	}
	if got, want := claims.Exp, int64(9999999999); got != want {
		t.Errorf("claims.Exp = %d, want %d", got, want)
	}
}

func TestParseJWTMalformed(t *testing.T) {
	if _, err := ParseJWT(""); err == nil {
		t.Errorf("ParseJWT(\"\") = nil error, want non-nil")
	}
	if _, err := ParseJWT("not.a.jwt"); err == nil {
		t.Errorf("ParseJWT(\"not.a.jwt\") = nil error, want non-nil")
	}
}

func TestParseJWTNotThreeSegments(t *testing.T) {
	if _, err := ParseJWT("only.two"); err == nil {
		t.Errorf("ParseJWT(\"only.two\") = nil error, want non-nil")
	}
}

func TestDiscoverPackOrgGuidSinglePack(t *testing.T) {
	data := loadFixture(t, "me_profile.json")

	var p PersonProfile
	if err := json.Unmarshal(data, &p); err != nil {
		t.Fatalf("unmarshal me profile: %v", err)
	}

	guid, err := DiscoverPackOrgGUID(p)
	if err != nil {
		t.Fatalf("DiscoverPackOrgGUID: unexpected error: %v", err)
	}
	if got, want := guid, "44444444-4444-4444-4444-444444444444"; got != want {
		t.Errorf("guid = %q, want %q", got, want)
	}
}

func TestDiscoverPackOrgGuidNoPack(t *testing.T) {
	data := loadFixture(t, "me_profile_no_packs.json")

	var p PersonProfile
	if err := json.Unmarshal(data, &p); err != nil {
		t.Fatalf("unmarshal me profile: %v", err)
	}

	guid, err := DiscoverPackOrgGUID(p)
	if err == nil {
		t.Fatalf("DiscoverPackOrgGUID returned guid=%q, want non-nil error", guid)
	}
	if !errors.Is(err, ErrNoPack) {
		t.Errorf("errors.Is(err, ErrNoPack) = false, want true; err = %v", err)
	}
}

func TestDiscoverPackOrgGuidMultiplePacks(t *testing.T) {
	data := loadFixture(t, "me_profile_multi_pack.json")

	var p PersonProfile
	if err := json.Unmarshal(data, &p); err != nil {
		t.Fatalf("unmarshal me profile: %v", err)
	}

	guid, err := DiscoverPackOrgGUID(p)
	if err == nil {
		t.Fatalf("DiscoverPackOrgGUID returned guid=%q, want non-nil error", guid)
	}
	if !errors.Is(err, ErrMultiplePacks) {
		t.Errorf("errors.Is(err, ErrMultiplePacks) = false, want true; err = %v", err)
	}
	msg := err.Error()
	if !strings.Contains(msg, "0123") {
		t.Errorf("error message %q does not contain %q", msg, "0123")
	}
	if !strings.Contains(msg, "0456") {
		t.Errorf("error message %q does not contain %q", msg, "0456")
	}
}
