package scouting

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestFetchAdventures pins the endpoint path and fixture shape for
// GET /advancements/v2/youth/{userID}/adventures.
func TestFetchAdventures(t *testing.T) {
	fixture := loadFixture(t, "adventures_wesley.json")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := fmt.Sprintf("/advancements/v2/youth/%d/adventures", wesleyUserID)
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

	adventures, err := FetchAdventures(ctx, client, wesleyUserID)
	if err != nil {
		t.Fatalf("FetchAdventures returned error: %v", err)
	}
	if got, want := len(adventures), 68; got != want {
		t.Errorf("adventures len = %d, want %d", got, want)
	}

	foundWebelos := false
	for _, a := range adventures {
		if a.RankID == 11 {
			foundWebelos = true
			break
		}
	}
	if !foundWebelos {
		t.Errorf("expected at least one adventure with RankID == 11")
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
		if a.RankID != 11 {
			t.Errorf("FilterAdventuresByRank returned adventure with RankID=%d, want 11; adventure=%+v", a.RankID, a)
		}
	}
}

// TestFetchRankRequirements pins
// GET /advancements/v2/youth/{userID}/ranks/{rankID}/requirements.
func TestFetchRankRequirements(t *testing.T) {
	fixture := loadFixture(t, "rank_webelos_wesley.json")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := fmt.Sprintf("/advancements/v2/youth/%d/ranks/%d/requirements", wesleyUserID, 11)
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

	rank, err := FetchRankRequirements(ctx, client, wesleyUserID, 11)
	if err != nil {
		t.Fatalf("FetchRankRequirements returned error: %v", err)
	}

	if got, want := rank.ID, 11; got != want {
		t.Errorf("rank.ID = %d, want %d", got, want)
	}
	if got, want := rank.Name, "Webelos"; got != want {
		t.Errorf("rank.Name = %q, want %q", got, want)
	}
	if got, want := rank.PercentCompleted, 0.63; got != want {
		t.Errorf("rank.PercentCompleted = %v, want %v", got, want)
	}
}

// TestFetchAdventureRequirements pins
// GET /advancements/v2/youth/{userID}/adventures/{adventureID}/requirements.
func TestFetchAdventureRequirements(t *testing.T) {
	fixture := loadFixture(t, "adventure_140_myfamily_wesley.json")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := fmt.Sprintf("/advancements/v2/youth/%d/adventures/%d/requirements", wesleyUserID, 140)
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

	adv, err := FetchAdventureRequirements(ctx, client, wesleyUserID, 140)
	if err != nil {
		t.Fatalf("FetchAdventureRequirements returned error: %v", err)
	}

	if got, want := adv.AdventureID, 140; got != want {
		t.Errorf("adv.AdventureID = %d, want %d", got, want)
	}
	if got, want := adv.PercentCompleted, 0.25; got != want {
		t.Errorf("adv.PercentCompleted = %v, want %v", got, want)
	}
	if got, want := len(adv.Requirements), 6; got != want {
		t.Errorf("len(adv.Requirements) = %d, want %d", got, want)
	}
}
