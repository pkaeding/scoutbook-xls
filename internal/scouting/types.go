package scouting

import "encoding/json"

type Roster struct {
	Status    bool            `json:"status"`
	OrgType   string          `json:"orgType"`
	Positions []RosterPosition `json:"Positions"`
}

type RosterPosition struct {
	PositionLong    string        `json:"positionLong"`
	PositionId      int           `json:"PositionId"`
	PersonsAssigned []YouthMember `json:"personsAssigned"`
}

type YouthMember struct {
	FirstName  string `json:"firstName"`
	LastName   string `json:"lastName"`
	MiddleName string `json:"middleName"`
	FullName   string `json:"fullName"`
	PersonGuid string `json:"personGuid"`
	OrgGuid    string `json:"orgguid"`
	OrgType    string `json:"orgType"`
	Gender     string `json:"gender"`
	Age        int    `json:"age"`
}

type PersonProfile struct {
	Profile                 Profile                `json:"profile"`
	OrganizationPositions   []OrganizationPosition `json:"organizationPositions"`
	CurrentProgramsAndRanks []ProgramAndRank       `json:"currentProgramsAndRanks"`
}

type Profile struct {
	FullName   string `json:"fullName"`
	PersonGuid string `json:"personGuid"`
	UserId     *int   `json:"userId"`
	FirstName  string `json:"firstName"`
	LastName   string `json:"lastName"`
}

type OrganizationPosition struct {
	OrganizationGuid string `json:"organizationGuid"`
	OrganizationName string `json:"organizationName"`
	UnitType         string `json:"unitType"`
	UnitNumber       string `json:"unitNumber"`
}

// ProgramAndRank is one entry in a person's currentProgramsAndRanks list.
//
// The Scouting API is inconsistent about numeric IDs: in some responses
// rankId/denId come back as JSON numbers, in others as JSON strings. A
// custom UnmarshalJSON below tolerates both.
type ProgramAndRank struct {
	ProgramId  string `json:"programId"`
	Program    string `json:"program"`
	UnitName   string `json:"unitName"`
	UnitType   string `json:"unitType"`
	UnitNumber string `json:"unitNumber"`
	UnitId     string `json:"unitId"`
	DenType    string `json:"denType"`
	DenId      int    `json:"-"`
	DenNumber  string `json:"denNumber"`
	RankId     int    `json:"-"`
	Rank       string `json:"rank"`
	EarnedDate string `json:"earnedDate"`
}

func (p *ProgramAndRank) UnmarshalJSON(data []byte) error {
	// Shadow type avoids recursive UnmarshalJSON. Uses flexInt for the two
	// fields where the API flips between string and number representations.
	type shadow struct {
		ProgramId  string   `json:"programId"`
		Program    string   `json:"program"`
		UnitName   string   `json:"unitName"`
		UnitType   string   `json:"unitType"`
		UnitNumber string   `json:"unitNumber"`
		UnitId     string   `json:"unitId"`
		DenType    string   `json:"denType"`
		DenId      flexInt  `json:"denId"`
		DenNumber  string   `json:"denNumber"`
		RankId     flexInt  `json:"rankId"`
		Rank       string   `json:"rank"`
		EarnedDate string   `json:"earnedDate"`
	}
	var s shadow
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	*p = ProgramAndRank{
		ProgramId:  s.ProgramId,
		Program:    s.Program,
		UnitName:   s.UnitName,
		UnitType:   s.UnitType,
		UnitNumber: s.UnitNumber,
		UnitId:     s.UnitId,
		DenType:    s.DenType,
		DenId:      s.DenId.Int(),
		DenNumber:  s.DenNumber,
		RankId:     s.RankId.Int(),
		Rank:       s.Rank,
		EarnedDate: s.EarnedDate,
	}
	return nil
}

type Adventure struct {
	AdventureId      int     `json:"adventureId"`
	AdventureName    string  `json:"adventureName"`
	ShortName        string  `json:"shortName"`
	RankId           int     `json:"rankId"`
	IsRequired       bool    `json:"isRequired"`
	PercentCompleted float64 `json:"percentCompleted"`
	Status           string  `json:"status"`
}

type AdventureRequirements struct {
	AdventureId      int           `json:"adventureId"`
	AdventureName    string        `json:"adventureName"`
	RankId           int           `json:"rankId"`
	PercentCompleted float64       `json:"percentCompleted"`
	Status           string        `json:"status"`
	Requirements     []Requirement `json:"requirements"`
}

type Requirement struct {
	RequirementId     int     `json:"requirementId"`
	RequirementNumber string  `json:"requirementNumber"`
	RequirementName   string  `json:"requirementName"`
	ShortName         string  `json:"shortName"`
	IsRequired        bool    `json:"isRequired"`
	IsOptional        bool    `json:"isOptional"`
	IsStarted         bool    `json:"isStarted"`
	IsCompleted       bool    `json:"isCompleted"`
	PercentCompleted  float64 `json:"percentCompleted"`
	Status            string  `json:"status"`
	DateCompleted     *string `json:"dateCompleted"`
}

type RankRequirements struct {
	Id               int               `json:"id"`
	Name             string            `json:"name"`
	PercentCompleted float64           `json:"percentCompleted"`
	Status           string            `json:"status"`
	ProgramId        int               `json:"programId"`
	Program          string            `json:"program"`
	Requirements     []RankRequirement `json:"requirements"`
}

type RankRequirement struct {
	Id                       int               `json:"id"`
	Name                     string            `json:"name"`
	RequirementNumber        string            `json:"requirementNumber"`
	ElectiveAdventure        bool              `json:"electiveAdventure"`
	Required                 bool              `json:"required"`
	Started                  bool              `json:"started"`
	Completed                bool              `json:"completed"`
	PercentCompleted         float64           `json:"percentCompleted"`
	Status                   string            `json:"status"`
	LinkedAdventureId        *int              `json:"linkedAdventureId"`
	LinkedAdventure          LinkedAdventure   `json:"linkedAdventure"`
	LinkedElectiveAdventures []LinkedAdventure `json:"linkedElectiveAdventures"`
}

type LinkedAdventure struct {
	Id               int     `json:"id"`
	Name             string  `json:"name"`
	ShortName        string  `json:"shortName"`
	RankId           int     `json:"rankId"`
	PercentCompleted float64 `json:"percentCompleted"`
	Status           string  `json:"status"`
}
