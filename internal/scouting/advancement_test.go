package scouting

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestFetchAdventures pins the endpoint path and fixture shape for
// GET /advancements/v2/youth/{userId}/adventures.
func TestFetchAdventures(t *testing.T) {
	fixture := loadFixture(t, "adventures_wesley.json")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := fmt.Sprintf("/advancements/v2/youth/%d/adventures", wesleyUserId)
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

	adventures, err := FetchAdventures(ctx, client, wesleyUserId)
	if err != nil {
		t.Fatalf("FetchAdventures returned error: %v", err)
	}
	if got, want := len(adventures), 68; got != want {
		t.Errorf("adventures len = %d, want %d", got, want)
	}

	foundWebelos := false
	for _, a := range adventures {
		if a.RankId == 11 {
			foundWebelos = true
			break
		}
	}
	if !foundWebelos {
		t.Errorf("expected at least one adventure with RankId == 11")
	}
}

// TestFilterAdventuresByRank pins the pure filter helper.
func TestFilterAdventuresByRank(t *testing.T) {
	data := loadFixture(t, "adventures_wesley.json")

	var adventures []Adventure
	if err := json.Unmarshal(data, &adventures); err != nil {
		t.Fatalf("unmarshal adventures fixture: %v", err)
	}

	filtered := FilterAdventuresByRank(adventures, 11)
	if got, want := len(filtered), 18; got != want {
		t.Errorf("filtered adventures len = %d, want %d", got, want)
	}
	for _, a := range filtered {
		if a.RankId != 11 {
			t.Errorf("FilterAdventuresByRank returned adventure with RankId=%d, want 11; adventure=%+v", a.RankId, a)
		}
	}
}

// TestFetchRankRequirements pins
// GET /advancements/v2/youth/{userId}/ranks/{rankId}/requirements.
func TestFetchRankRequirements(t *testing.T) {
	fixture := loadFixture(t, "rank_webelos_wesley.json")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := fmt.Sprintf("/advancements/v2/youth/%d/ranks/%d/requirements", wesleyUserId, 11)
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

	rank, err := FetchRankRequirements(ctx, client, wesleyUserId, 11)
	if err != nil {
		t.Fatalf("FetchRankRequirements returned error: %v", err)
	}

	if got, want := rank.Id, 11; got != want {
		t.Errorf("rank.Id = %d, want %d", got, want)
	}
	if got, want := rank.Name, "Webelos"; got != want {
		t.Errorf("rank.Name = %q, want %q", got, want)
	}
	if got, want := rank.PercentCompleted, 0.63; got != want {
		t.Errorf("rank.PercentCompleted = %v, want %v", got, want)
	}
}

// TestFetchAdventureRequirements pins
// GET /advancements/v2/youth/{userId}/adventures/{adventureId}/requirements.
func TestFetchAdventureRequirements(t *testing.T) {
	fixture := loadFixture(t, "adventure_140_myfamily_wesley.json")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := fmt.Sprintf("/advancements/v2/youth/%d/adventures/%d/requirements", wesleyUserId, 140)
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

	adv, err := FetchAdventureRequirements(ctx, client, wesleyUserId, 140)
	if err != nil {
		t.Fatalf("FetchAdventureRequirements returned error: %v", err)
	}

	if got, want := adv.AdventureId, 140; got != want {
		t.Errorf("adv.AdventureId = %d, want %d", got, want)
	}
	if got, want := adv.PercentCompleted, 0.25; got != want {
		t.Errorf("adv.PercentCompleted = %v, want %v", got, want)
	}
	if got, want := len(adv.Requirements), 6; got != want {
		t.Errorf("len(adv.Requirements) = %d, want %d", got, want)
	}
}

// TestDetermineTargetRankUnanimous: all scouts share the same rankId.
func TestDetermineTargetRankUnanimous(t *testing.T) {
	scouts := []ScoutWithDen{
		{FullName: "Scout A", PersonGuid: "aaa", RankId: 11},
		{FullName: "Scout B", PersonGuid: "bbb", RankId: 11},
		{FullName: "Scout C", PersonGuid: "ccc", RankId: 11},
	}

	target, warnings := DetermineTargetRank(scouts)
	if got, want := target, 11; got != want {
		t.Errorf("target = %d, want %d", got, want)
	}
	if got := len(warnings); got != 0 {
		t.Errorf("len(warnings) = %d, want 0; warnings=%v", got, warnings)
	}
}

// TestDetermineTargetRankMixed: majority wins, outliers are surfaced.
func TestDetermineTargetRankMixed(t *testing.T) {
	scouts := []ScoutWithDen{
		{FullName: "Alice Webelos", PersonGuid: "aaa", RankId: 11},
		{FullName: "Bob Webelos", PersonGuid: "bbb", RankId: 11},
		{FullName: "Charlie Bear", PersonGuid: "ccc", RankId: 10},
	}

	target, warnings := DetermineTargetRank(scouts)
	if got, want := target, 11; got != want {
		t.Errorf("target = %d, want %d (majority rankId)", got, want)
	}
	if len(warnings) == 0 {
		t.Fatalf("expected non-empty warnings, got none")
	}

	joined := strings.Join(warnings, " | ")
	if !strings.Contains(joined, "Charlie") {
		t.Errorf("warnings = %q, want to contain outlier scout name %q", joined, "Charlie")
	}
}

// TestDetermineTargetRankTie: tie-break by smallest rankId (deterministic).
func TestDetermineTargetRankTie(t *testing.T) {
	scouts := []ScoutWithDen{
		{FullName: "Alice Webelos", PersonGuid: "aaa", RankId: 11},
		{FullName: "Charlie Bear", PersonGuid: "ccc", RankId: 10},
	}

	target, warnings := DetermineTargetRank(scouts)
	if got, want := target, 10; got != want {
		t.Errorf("target = %d, want %d (tie broken by smallest rankId)", got, want)
	}
	if len(warnings) == 0 {
		t.Fatalf("expected non-empty warnings for tie, got none")
	}

	joined := strings.Join(warnings, " | ")
	if !strings.Contains(joined, "10") {
		t.Errorf("warnings = %q, want to mention rankId %q", joined, "10")
	}
	if !strings.Contains(joined, "11") {
		t.Errorf("warnings = %q, want to mention rankId %q", joined, "11")
	}
}

// TestDetermineTargetRankEmpty: pin the empty-input contract as
// (0, []string{"no scouts"}).
func TestDetermineTargetRankEmpty(t *testing.T) {
	target, warnings := DetermineTargetRank(nil)
	if got, want := target, 0; got != want {
		t.Errorf("target = %d, want %d", got, want)
	}
	if len(warnings) == 0 {
		t.Fatalf("expected a warning on empty input, got none")
	}
	joined := strings.Join(warnings, " | ")
	if !strings.Contains(strings.ToLower(joined), "no scouts") {
		t.Errorf("warnings = %q, want to contain \"no scouts\"", joined)
	}
}

