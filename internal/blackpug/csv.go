// Package blackpug turns a Black Pug summer-camp export (a Scoutbook-style
// CSV of completed merit badge requirements) into Scouting.org advancement
// entries.
package blackpug

import (
	"bufio"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// Advancement types found in the export's "Advancement Type" column.
const (
	TypeMeritBadge            = "Merit Badge"
	TypeMeritBadgeRequirement = "Merit Badge Requirement"
	TypeAwardRequirement      = "Award Requirement"
)

// Row is one line of the export.
type Row struct {
	Line      int // 1-based line number in the file, for error messages
	Unit      string
	MemberID  string
	FirstName string
	LastName  string
	Type      string
	// Advancement is the badge name for TypeMeritBadge, or "Badge #req" for
	// the requirement types.
	Advancement string
	Version     string
	Completed   time.Time
	Approved    bool
	Awarded     bool
}

// Scout returns the scout's display name.
func (r Row) Scout() string { return strings.TrimSpace(r.FirstName + " " + r.LastName) }

// BadgeAndRequirement splits a requirement row's Advancement value
// ("Leatherwork #3a") into the badge name and requirement number. The
// requirement is empty when there is no " #" separator.
func (r Row) BadgeAndRequirement() (badge, req string) {
	name, num, ok := strings.Cut(r.Advancement, " #")
	if !ok {
		return strings.TrimSpace(r.Advancement), ""
	}
	return strings.TrimSpace(name), strings.TrimSpace(num)
}

// bom is the UTF-8 byte-order mark some exporters prepend to the file.
const bom = "\xef\xbb\xbf"

var requiredColumns = []string{
	"BSA Member ID", "First Name", "Last Name", "Advancement Type",
	"Advancement", "Version", "Date Completed", "Approved",
}

// ParseCSV reads a Black Pug export. Columns are located by header name, so
// column order doesn't matter. Dates are MM-DD-YYYY.
func ParseCSV(r io.Reader) ([]Row, error) {
	// A BOM must go before the csv reader sees it: ahead of a quoted first
	// header it is a bare-quote error.
	br := bufio.NewReader(r)
	if b, err := br.Peek(len(bom)); err == nil && string(b) == bom {
		_, _ = br.Discard(len(bom))
	}
	cr := csv.NewReader(br)
	cr.FieldsPerRecord = -1
	cr.TrimLeadingSpace = true

	header, err := cr.Read()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("blackpug: empty CSV")
		}
		return nil, fmt.Errorf("blackpug: read header: %w", err)
	}
	col := map[string]int{}
	for i, h := range header {
		col[strings.TrimPrefix(strings.TrimSpace(h), bom)] = i
	}
	for _, name := range requiredColumns {
		if _, ok := col[name]; !ok {
			return nil, fmt.Errorf("blackpug: CSV is missing the %q column", name)
		}
	}

	var rows []Row
	for line := 2; ; line++ {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("blackpug: line %d: %w", line, err)
		}
		get := func(name string) string {
			if i, ok := col[name]; ok && i < len(rec) {
				return strings.TrimSpace(rec[i])
			}
			return ""
		}
		if get("BSA Member ID") == "" && get("Advancement") == "" {
			continue // blank line
		}
		completed, err := parseDate(get("Date Completed"))
		if err != nil {
			return nil, fmt.Errorf("blackpug: line %d: bad Date Completed %q (want MM-DD-YYYY or M/D/YYYY)", line, get("Date Completed"))
		}
		rows = append(rows, Row{
			Line:        line,
			Unit:        get("Unit"),
			MemberID:    get("BSA Member ID"),
			FirstName:   get("First Name"),
			LastName:    get("Last Name"),
			Type:        get("Advancement Type"),
			Advancement: get("Advancement"),
			Version:     get("Version"),
			Completed:   completed,
			Approved:    flag(get("Approved")),
			Awarded:     flag(get("Awarded")),
		})
	}
	return rows, nil
}

// parseDate accepts MM-DD-YYYY as Black Pug writes it, and the unpadded or
// slash-separated forms a spreadsheet re-save produces (7-9-2026, 7/9/2026).
// Go's "1"/"2" layout elements accept padded and unpadded values alike.
func parseDate(s string) (time.Time, error) {
	var err error
	for _, layout := range []string{"1-2-2006", "1/2/2006"} {
		var t time.Time
		if t, err = time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, err
}

// FilterBadges keeps only the rows for the named badges (case-insensitive,
// exact name). It errors if a name matches no row, so a typo can't silently
// turn into "nothing to do".
func FilterBadges(rows []Row, names []string) ([]Row, error) {
	if len(names) == 0 {
		return rows, nil
	}
	want := map[string]bool{}
	for _, n := range names {
		want[strings.ToLower(strings.TrimSpace(n))] = false
	}
	var out []Row
	for _, r := range rows {
		badge := r.Advancement
		if r.Type != TypeMeritBadge {
			badge, _ = r.BadgeAndRequirement()
		}
		k := strings.ToLower(badge)
		if _, ok := want[k]; ok {
			want[k] = true
			out = append(out, r)
		}
	}
	var missing []string
	for _, n := range names {
		if !want[strings.ToLower(strings.TrimSpace(n))] {
			missing = append(missing, n)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("no rows in the export for badge(s): %s", strings.Join(missing, ", "))
	}
	return out, nil
}

func flag(s string) bool {
	if n, err := strconv.Atoi(s); err == nil {
		return n != 0
	}
	return strings.EqualFold(s, "true") || strings.EqualFold(s, "yes")
}
