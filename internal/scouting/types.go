package scouting

import "encoding/json"

type Roster struct {
	Status    bool             `json:"status"`
	OrgType   string           `json:"orgType"`
	Positions []RosterPosition `json:"Positions"`
}

type RosterPosition struct {
	PositionLong    string        `json:"positionLong"`
	PositionID      int           `json:"PositionId"`
	PersonsAssigned []YouthMember `json:"personsAssigned"`
}

type YouthMember struct {
	FirstName  string `json:"firstName"`
	LastName   string `json:"lastName"`
	MiddleName string `json:"middleName"`
	FullName   string `json:"fullName"`
	PersonGUID string `json:"personGuid"`
	OrgGUID    string `json:"orgguid"`
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
	PersonGUID string `json:"personGuid"`
	UserID     *int   `json:"userId"`
	FirstName  string `json:"firstName"`
	LastName   string `json:"lastName"`
}

type OrganizationPosition struct {
	OrganizationGUID string `json:"organizationGuid"`
	OrganizationName string `json:"organizationName"`
	UnitType         string `json:"unitType"`
	UnitNumber       string `json:"unitNumber"`
}

// ProgramAndRank is one entry in a person's currentProgramsAndRanks list.
//
// The Scouting API is inconsistent about numeric IDs: in some responses
// rankID/denId come back as JSON numbers, in others as JSON strings. A
// custom UnmarshalJSON below tolerates both.
type ProgramAndRank struct {
	ProgramID  string `json:"programId"`
	Program    string `json:"program"`
	UnitName   string `json:"unitName"`
	UnitType   string `json:"unitType"`
	UnitNumber string `json:"unitNumber"`
	UnitID     string `json:"unitId"`
	DenType    string `json:"denType"`
	DenID      int    `json:"-"`
	DenNumber  string `json:"denNumber"`
	RankID     int    `json:"-"`
	Rank       string `json:"rank"`
	EarnedDate string `json:"earnedDate"`
}

func (p *ProgramAndRank) UnmarshalJSON(data []byte) error {
	// Shadow type avoids recursive UnmarshalJSON. Uses flexInt for the two
	// fields where the API flips between string and number representations.
	type shadow struct {
		ProgramID  string  `json:"programId"`
		Program    string  `json:"program"`
		UnitName   string  `json:"unitName"`
		UnitType   string  `json:"unitType"`
		UnitNumber string  `json:"unitNumber"`
		UnitID     string  `json:"unitId"`
		DenType    string  `json:"denType"`
		DenID      flexInt `json:"denId"`
		DenNumber  string  `json:"denNumber"`
		RankID     flexInt `json:"rankId"`
		Rank       string  `json:"rank"`
		EarnedDate string  `json:"earnedDate"`
	}
	var s shadow
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	*p = ProgramAndRank{
		ProgramID:  s.ProgramID,
		Program:    s.Program,
		UnitName:   s.UnitName,
		UnitType:   s.UnitType,
		UnitNumber: s.UnitNumber,
		UnitID:     s.UnitID,
		DenType:    s.DenType,
		DenID:      s.DenID.Int(),
		DenNumber:  s.DenNumber,
		RankID:     s.RankID.Int(),
		Rank:       s.Rank,
		EarnedDate: s.EarnedDate,
	}
	return nil
}

type Adventure struct {
	AdventureID      int     `json:"adventureId"`
	AdventureName    string  `json:"adventureName"`
	ShortName        string  `json:"shortName"`
	RankID           int     `json:"rankId"`
	IsRequired       bool    `json:"isRequired"`
	PercentCompleted float64 `json:"percentCompleted"`
	Status           string  `json:"status"`
}

type AdventureRequirements struct {
	AdventureID      int           `json:"adventureId"`
	AdventureName    string        `json:"adventureName"`
	RankID           int           `json:"rankId"`
	PercentCompleted float64       `json:"percentCompleted"`
	Status           string        `json:"status"`
	Requirements     []Requirement `json:"requirements"`
}

type Requirement struct {
	RequirementID     int     `json:"requirementId"`
	RequirementNumber string  `json:"requirementNumber"`
	RequirementName   string  `json:"requirementName"`
	ShortName         string  `json:"shortName"`
	SortOrder         float64 `json:"sortOrder"`
	IsRequired        bool    `json:"isRequired"`
	IsOptional        bool    `json:"isOptional"`
	IsStarted         bool    `json:"isStarted"`
	IsCompleted       bool    `json:"isCompleted"`
	PercentCompleted  float64 `json:"percentCompleted"`
	Status            string  `json:"status"`
	DateCompleted     *string `json:"dateCompleted"`
}

type RankRequirements struct {
	ID               int               `json:"id"`
	Name             string            `json:"name"`
	PercentCompleted float64           `json:"percentCompleted"`
	Status           string            `json:"status"`
	ProgramID        int               `json:"programId"`
	Program          string            `json:"program"`
	Requirements     []RankRequirement `json:"requirements"`
}

type RankRequirement struct {
	ID                       int               `json:"id"`
	Name                     string            `json:"name"`
	RequirementNumber        string            `json:"requirementNumber"`
	SortOrder                string            `json:"sortOrder"`
	ParentRequirementID      *int              `json:"parentRequirementId"`
	ChildrenRequired         string            `json:"childrenRequired"`
	ElectiveAdventure        bool              `json:"electiveAdventure"`
	Required                 bool              `json:"required"`
	Started                  bool              `json:"started"`
	Completed                bool              `json:"completed"`
	PercentCompleted         float64           `json:"percentCompleted"`
	Status                   string            `json:"status"`
	DateCompleted            *string           `json:"dateCompleted"`
	LinkedAdventureID        *int              `json:"linkedAdventureId"`
	LinkedAdventure          LinkedAdventure   `json:"linkedAdventure"`
	LinkedElectiveAdventures []LinkedAdventure `json:"linkedElectiveAdventures"`
}

type LinkedAdventure struct {
	ID               int     `json:"id"`
	Name             string  `json:"name"`
	ShortName        string  `json:"shortName"`
	RankID           int     `json:"rankId"`
	PercentCompleted float64 `json:"percentCompleted"`
	Status           string  `json:"status"`
}
