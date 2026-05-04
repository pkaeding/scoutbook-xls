package scouting

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// loadFixture reads a JSON fixture from testdata/fixtures/<name> relative
// to this test package (i.e. ../../testdata/fixtures/<name>).
func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "fixtures", name)
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open fixture %q: %v", path, err)
	}
	defer f.Close()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %q: %v", path, err)
	}
	return data
}

func TestUnmarshalRoster(t *testing.T) {
	data := loadFixture(t, "roster_pack0123.json")

	var roster Roster
	if err := json.Unmarshal(data, &roster); err != nil {
		t.Fatalf("unmarshal roster: %v", err)
	}

	// Find the Youth Member position and read its personsAssigned.
	var youth []YouthMember
	for _, p := range roster.Positions {
		if p.PositionLong == "Youth Member" {
			youth = p.PersonsAssigned
			break
		}
	}
	if got, want := len(youth), 24; got != want {
		t.Errorf("youth member count = %d, want %d", got, want)
	}

	// The sanitized fixture uses plain ASCII spaces in fullName. (The live
	// API returns U+00A0 between first and last names; the renderer
	// normalizes that downstream.)
	if len(youth) > 0 {
		if got, want := youth[0].FullName, "James Kirk"; got != want {
			t.Errorf("first youth fullName = %q, want %q", got, want)
		}
		if got, want := youth[0].PersonGUID, "30000001-0001-0001-0001-300000000001"; got != want {
			t.Errorf("first youth personGuid = %q, want %q", got, want)
		}
	}

	// Wesley should be present.
	foundWesley := false
	for _, y := range youth {
		if y.PersonGUID == "22222222-1111-1111-1111-222222222222" {
			foundWesley = true
			if got, want := y.FullName, "Wesley Crusher"; got != want {
				t.Errorf("wesley fullName = %q, want %q", got, want)
			}
			break
		}
	}
	if !foundWesley {
		t.Errorf("did not find Wesley Crusher in youth roster")
	}
}

func TestUnmarshalPersonProfileByGuid(t *testing.T) {
	data := loadFixture(t, "profile_wesley_by_guid.json")

	var p PersonProfile
	if err := json.Unmarshal(data, &p); err != nil {
		t.Fatalf("unmarshal person profile by guid: %v", err)
	}

	if got, want := p.Profile.FullName, "Wesley Crusher"; got != want {
		t.Errorf("profile.fullName = %q, want %q", got, want)
	}

	// UserID is numeric in the by-guid response. Dereference the pointer.
	if p.Profile.UserID == nil {
		t.Fatalf("profile.userID = nil, want 20000001")
	}
	if got, want := *p.Profile.UserID, 20000001; got != want {
		t.Errorf("profile.userID = %d, want %d", got, want)
	}

	// currentProgramsAndRanks is absent/null in the by-guid response.
	if got := len(p.CurrentProgramsAndRanks); got != 0 {
		t.Errorf("currentProgramsAndRanks len = %d, want 0", got)
	}
}

func TestUnmarshalPersonProfileByUserId(t *testing.T) {
	data := loadFixture(t, "profile_wesley_by_userid.json")

	var p PersonProfile
	if err := json.Unmarshal(data, &p); err != nil {
		t.Fatalf("unmarshal person profile by userID: %v", err)
	}

	if got, want := p.Profile.FullName, "Wesley Crusher"; got != want {
		t.Errorf("profile.fullName = %q, want %q", got, want)
	}

	// UserID is null in the by-userID response.
	if p.Profile.UserID != nil {
		t.Errorf("profile.userID = %v, want nil", *p.Profile.UserID)
	}

	if got, want := len(p.CurrentProgramsAndRanks), 1; got != want {
		t.Fatalf("currentProgramsAndRanks len = %d, want %d", got, want)
	}
	pr := p.CurrentProgramsAndRanks[0]
	if got, want := pr.DenType, "Webelos"; got != want {
		t.Errorf("denType = %q, want %q", got, want)
	}
	if got, want := pr.DenNumber, "1"; got != want {
		t.Errorf("denNumber = %q, want %q", got, want)
	}
	if got, want := pr.DenID, 99999; got != want {
		t.Errorf("denId = %d, want %d", got, want)
	}
	if got, want := pr.RankID, 10; got != want {
		t.Errorf("rankID = %d, want %d", got, want)
	}
}

func TestUnmarshalMeProfileOrganizationPositions(t *testing.T) {
	data := loadFixture(t, "me_profile.json")

	var p PersonProfile
	if err := json.Unmarshal(data, &p); err != nil {
		t.Fatalf("unmarshal me profile: %v", err)
	}

	if got, want := len(p.OrganizationPositions), 3; got != want {
		t.Fatalf("organizationPositions len = %d, want %d", got, want)
	}

	var pack *OrganizationPosition
	for i := range p.OrganizationPositions {
		if p.OrganizationPositions[i].UnitType == "Pack" {
			pack = &p.OrganizationPositions[i]
			break
		}
	}
	if pack == nil {
		t.Fatalf("no Pack organizationPosition found")
	}
	if got, want := pack.OrganizationGUID, "44444444-4444-4444-4444-444444444444"; got != want {
		t.Errorf("pack organizationGuid = %q, want %q", got, want)
	}
}

func TestUnmarshalAdventuresList(t *testing.T) {
	data := loadFixture(t, "adventures_wesley.json")

	var adventures []Adventure
	if err := json.Unmarshal(data, &adventures); err != nil {
		t.Fatalf("unmarshal adventures: %v", err)
	}

	if got, want := len(adventures), 68; got != want {
		t.Errorf("adventures len = %d, want %d", got, want)
	}

	webelosCount := 0
	for _, a := range adventures {
		if a.RankID == 11 {
			webelosCount++
		}
	}
	if got, want := webelosCount, 18; got != want {
		t.Errorf("Webelos (rankID=11) adventures count = %d, want %d", got, want)
	}

	hasProgress := false
	hasZero := false
	for _, a := range adventures {
		if a.PercentCompleted > 0 {
			hasProgress = true
		}
		if a.PercentCompleted == 0 {
			hasZero = true
		}
	}
	if !hasProgress {
		t.Errorf("expected at least one adventure with percentCompleted > 0")
	}
	if !hasZero {
		t.Errorf("expected at least one adventure with percentCompleted == 0")
	}
}

func TestUnmarshalRankWebelosRequirements(t *testing.T) {
	data := loadFixture(t, "rank_webelos_wesley.json")

	var rank RankRequirements
	if err := json.Unmarshal(data, &rank); err != nil {
		t.Fatalf("unmarshal rank webelos: %v", err)
	}

	if got, want := rank.ID, 11; got != want {
		t.Errorf("rank.id = %d, want %d", got, want)
	}
	if got, want := rank.Name, "Webelos"; got != want {
		t.Errorf("rank.name = %q, want %q", got, want)
	}
	if got, want := rank.PercentCompleted, 0.63; got != want {
		t.Errorf("rank.percentCompleted = %v, want %v", got, want)
	}

	var req2a *RankRequirement
	var req1a *RankRequirement
	for i := range rank.Requirements {
		switch rank.Requirements[i].RequirementNumber {
		case "2a":
			req2a = &rank.Requirements[i]
		case "1a":
			req1a = &rank.Requirements[i]
		}
	}

	if req2a == nil {
		t.Fatalf("did not find requirement 2a")
	}
	// NOTE: plan said 19 but the fixture actually has 20 linkedElectiveAdventures.
	if got, want := len(req2a.LinkedElectiveAdventures), 20; got != want {
		t.Errorf("req 2a linkedElectiveAdventures len = %d, want %d", got, want)
	}

	if req1a == nil {
		t.Fatalf("did not find requirement 1a")
	}
	if got, want := req1a.LinkedAdventure.ID, 132; got != want {
		t.Errorf("req 1a linkedAdventure.id = %d, want %d", got, want)
	}
	if got, want := req1a.LinkedAdventure.Name, "Bobcat (Webelos)"; got != want {
		t.Errorf("req 1a linkedAdventure.name = %q, want %q", got, want)
	}
}

func TestUnmarshalAdventure140Requirements(t *testing.T) {
	data := loadFixture(t, "adventure_140_myfamily_wesley.json")

	var adv AdventureRequirements
	if err := json.Unmarshal(data, &adv); err != nil {
		t.Fatalf("unmarshal adventure 140: %v", err)
	}

	if got, want := adv.AdventureID, 140; got != want {
		t.Errorf("adventureID = %d, want %d", got, want)
	}
	if got, want := adv.PercentCompleted, 0.25; got != want {
		t.Errorf("percentCompleted = %v, want %v", got, want)
	}

	// NOTE: plan said 3 requirements but the fixture actually has 6.
	if got, want := len(adv.Requirements), 6; got != want {
		t.Errorf("requirements len = %d, want %d", got, want)
	}

	var req2 *Requirement
	for i := range adv.Requirements {
		if adv.Requirements[i].RequirementNumber == "2" {
			req2 = &adv.Requirements[i]
			break
		}
	}
	if req2 == nil {
		t.Fatalf("did not find requirement 2 in adventure 140")
	}
	if !req2.IsCompleted {
		t.Errorf("req 2 isCompleted = false, want true")
	}
	if req2.DateCompleted == nil {
		t.Fatalf("req 2 dateCompleted = nil, want pointer to \"2025-12-11\"")
	}
	if got, want := *req2.DateCompleted, "2025-12-11"; got != want {
		t.Errorf("req 2 dateCompleted = %q, want %q", got, want)
	}
}
