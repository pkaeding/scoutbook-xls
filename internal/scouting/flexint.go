package scouting

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// flexInt unmarshals from either a JSON number or a JSON string holding a
// number. The Scouting API is inconsistent: some responses return numeric
// IDs (e.g. rankId, denId, programId) as JSON numbers, others return the
// same fields as JSON strings. Callers should treat this as a plain int.
type flexInt int

func (f *flexInt) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		*f = 0
		return nil
	}
	// Numeric form.
	if data[0] != '"' {
		var n int
		if err := json.Unmarshal(data, &n); err != nil {
			return fmt.Errorf("flexInt: %w", err)
		}
		*f = flexInt(n)
		return nil
	}
	// String form.
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("flexInt: %w", err)
	}
	if s == "" {
		*f = 0
		return nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return fmt.Errorf("flexInt: parse %q: %w", s, err)
	}
	*f = flexInt(n)
	return nil
}

// Int returns the underlying int.
func (f flexInt) Int() int { return int(f) }
