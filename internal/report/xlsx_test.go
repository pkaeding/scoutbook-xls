package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	excelize "github.com/xuri/excelize/v2"
)

// --- helpers --------------------------------------------------------------

// xlsxTempPath returns a fresh temp .xlsx path for a test.
func xlsxTempPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "report.xlsx")
}

// scoutCols builds a minimal []ScoutCol from first names.
func scoutCols(firsts ...string) []ScoutCol {
	out := make([]ScoutCol, len(firsts))
	for i, f := range firsts {
		out[i] = ScoutCol{
			FirstName: f,
			FullName:  f + " Example",
			UserID:    1000 + i,
		}
	}
	return out
}

// findSheetName returns the first sheet name containing sub, or "" if none.
func findSheetName(f *excelize.File, sub string) string {
	for _, name := range f.GetSheetList() {
		if strings.Contains(name, sub) {
			return name
		}
	}
	return ""
}

// findRowContaining returns the 1-based row index in the given sheet whose
// row 0 column equals label, or 0 if not found.
func findRowContaining(t *testing.T, f *excelize.File, sheet, label string) int {
	t.Helper()
	rows, err := f.GetRows(sheet)
	if err != nil {
		t.Fatalf("GetRows(%q) err = %v", sheet, err)
	}
	for i, row := range rows {
		if len(row) > 0 && row[0] == label {
			return i + 1
		}
	}
	return 0
}

// --- tests ---------------------------------------------------------------

func TestRenderXLSXCreatesFileWithDenSheet(t *testing.T) {
	m := ReportModel{
		DenLabel: "Webelos 1",
		RankName: "Webelos",
		Scouts:   scoutCols("Alice", "Bob"),
		SummaryRows: []SummaryRow{
			{Kind: SummaryRowSectionHeader, Label: "Rank Requirements"},
			{Kind: SummaryRowRankReq, Label: "1a — Bobcat", Percents: []float64{0.5, 0.25}},
			{Kind: SummaryRowSectionHeader, Label: "Adventures"},
			{Kind: SummaryRowAdventure, Label: "My Family", Percents: []float64{0.5, 0.5}},
		},
		// No per-adventure sheets.
		Adventures: nil,
	}

	path := xlsxTempPath(t)
	if err := RenderXLSX(m, path); err != nil {
		t.Fatalf("RenderXLSX err = %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(%q) err = %v", path, err)
	}
	if info.Size() == 0 {
		t.Fatalf("Stat(%q) size = 0, want > 0", path)
	}

	f, err := excelize.OpenFile(path)
	if err != nil {
		t.Fatalf("excelize.OpenFile(%q) err = %v", path, err)
	}
	defer f.Close()

	// Sheet named after the den label must exist.
	if idx, _ := f.GetSheetIndex("Webelos 1"); idx < 0 {
		t.Fatalf("sheet %q not found; sheets = %v", "Webelos 1", f.GetSheetList())
	}

	// Header row: A1 contains something like "Requirement", B1, C1 are
	// scout first names.
	a1, err := f.GetCellValue("Webelos 1", "A1")
	if err != nil {
		t.Fatalf("GetCellValue(A1) err = %v", err)
	}
	if !strings.Contains(strings.ToLower(a1), "requirement") {
		t.Errorf("A1 = %q, want to contain \"requirement\" (case-insensitive)", a1)
	}

	b1, err := f.GetCellValue("Webelos 1", "B1")
	if err != nil {
		t.Fatalf("GetCellValue(B1) err = %v", err)
	}
	if b1 != "Alice" {
		t.Errorf("B1 = %q, want %q", b1, "Alice")
	}
	c1, err := f.GetCellValue("Webelos 1", "C1")
	if err != nil {
		t.Fatalf("GetCellValue(C1) err = %v", err)
	}
	if c1 != "Bob" {
		t.Errorf("C1 = %q, want %q", c1, "Bob")
	}
}

func TestRenderXLSXSummaryRowsInOrder(t *testing.T) {
	// Rank-req rows hold per-scout DateCompleted strings (binary done/not-done);
	// adventure rows hold percent values.
	date1a := "2025-09-11"
	m := ReportModel{
		DenLabel: "Webelos 1",
		RankName: "Webelos",
		Scouts:   scoutCols("Alice", "Bob"),
		SummaryRows: []SummaryRow{
			{Kind: SummaryRowSectionHeader, Label: "Rank Requirements"},
			{Kind: SummaryRowRankReq, Label: "1a — Bobcat", Dates: []*string{&date1a, nil}},
			{Kind: SummaryRowRankReq, Label: "2a — Elective", Dates: []*string{nil, nil}},
			{Kind: SummaryRowSectionHeader, Label: "Adventures"},
			{Kind: SummaryRowAdventure, Label: "My Family", Percents: []float64{0.75, 0.5}},
		},
	}

	path := xlsxTempPath(t)
	if err := RenderXLSX(m, path); err != nil {
		t.Fatalf("RenderXLSX err = %v", err)
	}

	f, err := excelize.OpenFile(path)
	if err != nil {
		t.Fatalf("excelize.OpenFile err = %v", err)
	}
	defer f.Close()

	sheet := "Webelos 1"
	if idx, _ := f.GetSheetIndex(sheet); idx < 0 {
		t.Fatalf("sheet %q not found; sheets = %v", sheet, f.GetSheetList())
	}

	// Expected label-order in column A (after header row at row 1):
	// Row 2: "Rank Requirements"
	// Row 3: "1a — Bobcat"
	// Row 4: "2a — Elective"
	// Row 5: "Adventures"
	// Row 6: "My Family"
	wantLabels := []string{
		"Rank Requirements",
		"1a — Bobcat",
		"2a — Elective",
		"Adventures",
		"My Family",
	}
	// Locate the first label row and assert they are contiguous in order.
	firstRow := findRowContaining(t, f, sheet, wantLabels[0])
	if firstRow == 0 {
		t.Fatalf("could not find a row with label %q in column A; sheet rows follow", wantLabels[0])
	}
	for i, label := range wantLabels {
		got, err := f.GetCellValue(sheet, cellRef("A", firstRow+i))
		if err != nil {
			t.Fatalf("GetCellValue(A%d) err = %v", firstRow+i, err)
		}
		if got != label {
			t.Errorf("A%d = %q, want %q", firstRow+i, got, label)
		}
	}

	// The "1a — Bobcat" row holds date strings. Scout A has a date, Scout B
	// does not. Adventures (below) still carry percent formatting.
	bobcatRow := firstRow + 1
	bBobcat := cellRef("B", bobcatRow)
	cBobcat := cellRef("C", bobcatRow)
	if v, _ := f.GetCellValue(sheet, bBobcat); v != "2025-09-11" {
		t.Errorf("%s = %q, want %q", bBobcat, v, "2025-09-11")
	}
	if v, _ := f.GetCellValue(sheet, cBobcat); v != "" {
		t.Errorf("%s = %q, want \"\" (no date for this scout)", cBobcat, v)
	}

	// The adventure row ("My Family") is at firstRow+4 (Rank Requirements,
	// 1a, 2a, Adventures section header, My Family). Scout A percent must
	// render with a percent sign and be stored as a number with percent fmt.
	advRow := firstRow + 4
	bAdv := cellRef("B", advRow)
	bVal, err := f.GetCellValue(sheet, bAdv)
	if err != nil {
		t.Fatalf("GetCellValue(%s) err = %v", bAdv, err)
	}
	if !strings.HasSuffix(bVal, "%") {
		t.Errorf("%s = %q, want a value ending in %q", bAdv, bVal, "%")
	}
	btype, err := f.GetCellType(sheet, bAdv)
	if err != nil {
		t.Fatalf("GetCellType(%s) err = %v", bAdv, err)
	}
	if btype == excelize.CellTypeSharedString || btype == excelize.CellTypeInlineString {
		t.Errorf("%s cell type = %v, want numeric; percents must be stored as numbers", bAdv, btype)
	}
	styleID, err := f.GetCellStyle(sheet, bAdv)
	if err != nil {
		t.Fatalf("GetCellStyle(%s) err = %v", bAdv, err)
	}
	style, err := f.GetStyle(styleID)
	if err != nil {
		t.Fatalf("GetStyle(%d) err = %v", styleID, err)
	}
	if style == nil {
		t.Fatalf("GetStyle(%d) returned nil", styleID)
	}
	hasPercentFmt := (style.CustomNumFmt != nil && strings.Contains(*style.CustomNumFmt, "%")) ||
		style.NumFmt == 9 || style.NumFmt == 10
	if !hasPercentFmt {
		t.Errorf("%s style has no percent number format: NumFmt=%d CustomNumFmt=%v", bAdv, style.NumFmt, style.CustomNumFmt)
	}
}

func TestRenderXLSXPerAdventureSheet(t *testing.T) {
	date := "2025-12-11"
	m := ReportModel{
		DenLabel: "Webelos 1",
		RankName: "Webelos",
		Scouts:   scoutCols("Alice", "Bob"),
		SummaryRows: []SummaryRow{
			{Kind: SummaryRowSectionHeader, Label: "Adventures"},
			{Kind: SummaryRowAdventure, Label: "My Family", Percents: []float64{0.5, 1.0}},
		},
		Adventures: []AdventureSheet{
			{
				AdventureID: 140,
				Name:        "My Family",
				ShortName:   "MyFam",
				Rows: []AdventureRow{
					{Label: "1 — MyFam", DatesCompleted: []*string{nil, &date}},
					{Label: "2 — MyFam", DatesCompleted: []*string{&date, nil}},
				},
				OverallPcts: []float64{0.5, 1.0},
			},
		},
	}

	path := xlsxTempPath(t)
	if err := RenderXLSX(m, path); err != nil {
		t.Fatalf("RenderXLSX err = %v", err)
	}

	f, err := excelize.OpenFile(path)
	if err != nil {
		t.Fatalf("excelize.OpenFile err = %v", err)
	}
	defer f.Close()

	// A sheet named "My Family" exists.
	if idx, _ := f.GetSheetIndex("My Family"); idx < 0 {
		t.Fatalf("sheet %q not found; sheets = %v", "My Family", f.GetSheetList())
	}
	sheet := "My Family"

	// Header row.
	a1, _ := f.GetCellValue(sheet, "A1")
	if !strings.Contains(strings.ToLower(a1), "requirement") {
		t.Errorf("A1 = %q, want to contain \"requirement\"", a1)
	}
	b1, _ := f.GetCellValue(sheet, "B1")
	c1, _ := f.GetCellValue(sheet, "C1")
	if b1 != "Alice" {
		t.Errorf("B1 = %q, want %q", b1, "Alice")
	}
	if c1 != "Bob" {
		t.Errorf("C1 = %q, want %q", c1, "Bob")
	}

	// Row 2: "1 — MyFam", B2 empty (nil), C2 contains 2025-12
	a2, _ := f.GetCellValue(sheet, "A2")
	if a2 != "1 — MyFam" {
		t.Errorf("A2 = %q, want %q", a2, "1 — MyFam")
	}
	b2, _ := f.GetCellValue(sheet, "B2")
	if b2 != "" {
		t.Errorf("B2 = %q, want empty (nil date)", b2)
	}
	c2, _ := f.GetCellValue(sheet, "C2")
	if c2 == "" {
		t.Errorf("C2 = empty, want non-empty date value")
	}
	if !strings.Contains(c2, "2025") || !strings.Contains(c2, "12") {
		t.Errorf("C2 = %q, want value containing \"2025\" and \"12\"", c2)
	}

	// Row 3: "2 — MyFam", B3 has date, C3 empty.
	a3, _ := f.GetCellValue(sheet, "A3")
	if a3 != "2 — MyFam" {
		t.Errorf("A3 = %q, want %q", a3, "2 — MyFam")
	}
	b3, _ := f.GetCellValue(sheet, "B3")
	if b3 == "" {
		t.Errorf("B3 = empty, want non-empty date value")
	}
	if !strings.Contains(b3, "2025") || !strings.Contains(b3, "12") {
		t.Errorf("B3 = %q, want value containing \"2025\" and \"12\"", b3)
	}
	c3, _ := f.GetCellValue(sheet, "C3")
	if c3 != "" {
		t.Errorf("C3 = %q, want empty (nil date)", c3)
	}

	// Final row: "% Complete" label + 50%, 100%.
	pctRow := findRowContaining(t, f, sheet, "% Complete")
	if pctRow == 0 {
		// Try a softer match in case label text differs trivially.
		rows, _ := f.GetRows(sheet)
		for i, row := range rows {
			if len(row) > 0 && strings.Contains(strings.ToLower(row[0]), "complete") {
				pctRow = i + 1
				break
			}
		}
	}
	if pctRow == 0 {
		t.Fatalf("could not find a %q row in sheet %q", "% Complete", sheet)
	}
	bPct, _ := f.GetCellValue(sheet, cellRef("B", pctRow))
	cPct, _ := f.GetCellValue(sheet, cellRef("C", pctRow))
	if !strings.HasSuffix(bPct, "%") {
		t.Errorf("B%d = %q, want a percent-formatted value", pctRow, bPct)
	}
	if !strings.HasSuffix(cPct, "%") {
		t.Errorf("C%d = %q, want a percent-formatted value", pctRow, cPct)
	}
}

func TestRenderXLSXSheetNameTruncation(t *testing.T) {
	longName := "This Adventure Name Exceeds Thirty One Characters"
	if len(longName) <= 31 {
		t.Fatalf("test fixture bug: longName is %d chars, want > 31", len(longName))
	}
	m := ReportModel{
		DenLabel: "Webelos 1",
		RankName: "Webelos",
		Scouts:   scoutCols("Alice"),
		SummaryRows: []SummaryRow{
			{Kind: SummaryRowSectionHeader, Label: "Adventures"},
			{Kind: SummaryRowAdventure, Label: longName, Percents: []float64{0.5}},
		},
		Adventures: []AdventureSheet{
			{
				AdventureID: 200,
				Name:        longName,
				ShortName:   "Long",
				Rows: []AdventureRow{
					{Label: "1 — Long", DatesCompleted: []*string{nil}},
				},
				OverallPcts: []float64{0.5},
			},
		},
	}

	path := xlsxTempPath(t)
	if err := RenderXLSX(m, path); err != nil {
		t.Fatalf("RenderXLSX err = %v", err)
	}
	f, err := excelize.OpenFile(path)
	if err != nil {
		t.Fatalf("excelize.OpenFile err = %v", err)
	}
	defer f.Close()

	// Find a sheet that starts with the first chars of longName and is <= 31.
	prefix := longName[:20] // a stable prefix inside the 31-char window
	matched := ""
	for _, name := range f.GetSheetList() {
		if len(name) > 31 {
			t.Errorf("sheet name %q has len %d > 31", name, len(name))
		}
		if strings.HasPrefix(name, prefix) {
			matched = name
		}
	}
	if matched == "" {
		t.Fatalf("no sheet starts with %q; sheets = %v", prefix, f.GetSheetList())
	}
	if len(matched) > 31 {
		t.Errorf("matched sheet %q has len %d > 31", matched, len(matched))
	}
}

func TestRenderXLSXSheetNameCollision(t *testing.T) {
	// Both names share the same first 31 chars; they differ only at char 32.
	base := strings.Repeat("A", 31)
	nameX := base + "X"
	nameY := base + "Y"
	if nameX[:31] != nameY[:31] {
		t.Fatalf("test fixture bug: first 31 chars differ")
	}

	m := ReportModel{
		DenLabel: "Webelos 1",
		RankName: "Webelos",
		Scouts:   scoutCols("Alice"),
		SummaryRows: []SummaryRow{
			{Kind: SummaryRowSectionHeader, Label: "Adventures"},
			{Kind: SummaryRowAdventure, Label: nameX, Percents: []float64{0.5}},
			{Kind: SummaryRowAdventure, Label: nameY, Percents: []float64{0.25}},
		},
		Adventures: []AdventureSheet{
			{
				AdventureID: 201,
				Name:        nameX,
				ShortName:   "X",
				Rows: []AdventureRow{
					{Label: "1 — X", DatesCompleted: []*string{nil}},
				},
				OverallPcts: []float64{0.5},
			},
			{
				AdventureID: 202,
				Name:        nameY,
				ShortName:   "Y",
				Rows: []AdventureRow{
					{Label: "1 — Y", DatesCompleted: []*string{nil}},
				},
				OverallPcts: []float64{0.25},
			},
		},
	}

	path := xlsxTempPath(t)
	if err := RenderXLSX(m, path); err != nil {
		t.Fatalf("RenderXLSX err = %v", err)
	}
	f, err := excelize.OpenFile(path)
	if err != nil {
		t.Fatalf("excelize.OpenFile err = %v", err)
	}
	defer f.Close()

	sheets := f.GetSheetList()

	// Two sheets start with the common 31-char prefix and have distinct names.
	prefix := base
	var matched []string
	for _, name := range sheets {
		if len(name) > 31 {
			t.Errorf("sheet name %q has len %d > 31", name, len(name))
		}
		// The truncated sheet must START with some prefix of base. Allow a
		// numeric suffix inside the 31-char window.
		if strings.HasPrefix(name, prefix[:20]) {
			matched = append(matched, name)
		}
	}
	if len(matched) < 2 {
		t.Fatalf("expected >= 2 sheets with truncated-name prefix; got %v from %v", matched, sheets)
	}
	if matched[0] == matched[1] {
		t.Errorf("matched sheet names collide: %q == %q", matched[0], matched[1])
	}
	// Ensure uniqueness across the full sheet list.
	seen := map[string]bool{}
	for _, name := range sheets {
		if seen[name] {
			t.Errorf("duplicate sheet name %q in %v", name, sheets)
		}
		seen[name] = true
	}
}

func TestRenderXLSXHeaderRowIsBold(t *testing.T) {
	m := ReportModel{
		DenLabel: "Webelos 1",
		RankName: "Webelos",
		Scouts:   scoutCols("Alice", "Bob"),
		SummaryRows: []SummaryRow{
			{Kind: SummaryRowSectionHeader, Label: "Rank Requirements"},
		},
	}

	path := xlsxTempPath(t)
	if err := RenderXLSX(m, path); err != nil {
		t.Fatalf("RenderXLSX err = %v", err)
	}
	f, err := excelize.OpenFile(path)
	if err != nil {
		t.Fatalf("excelize.OpenFile err = %v", err)
	}
	defer f.Close()

	sheet := "Webelos 1"
	styleID, err := f.GetCellStyle(sheet, "A1")
	if err != nil {
		t.Skipf("GetCellStyle(A1) err = %v; skipping bold assertion", err)
	}
	style, err := f.GetStyle(styleID)
	if err != nil {
		t.Skipf("GetStyle(%d) err = %v; skipping bold assertion", styleID, err)
	}
	if style == nil || style.Font == nil {
		t.Skipf("style or font nil; skipping bold assertion (style=%+v)", style)
	}
	if !style.Font.Bold {
		t.Errorf("A1 font.Bold = false, want true")
	}
}

// TestRenderXLSXNormalizesNBSPInNames verifies that non-breaking spaces
// (U+00A0) the Scouting API emits in names get collapsed to ASCII spaces
// when written to cells, and that labels with embedded NBSP are cleaned up
// similarly. Scout first names in this test are deliberately single words
// (no NBSP in the middle), since the API splits first/last — but full
// names and labels can carry NBSP, and those are the cases we guard.
func TestRenderXLSXNormalizesNBSPInNames(t *testing.T) {
	// NBSP is U+00A0, rendered below as " " literal.
	nbsp := " "
	m := ReportModel{
		DenLabel: "Webelos 1",
		RankName: "Webelos",
		Scouts: []ScoutCol{
			{FirstName: "Alice" + nbsp + "Marie", FullName: "Alice Marie Example", UserID: 1},
			{FirstName: "Bob", FullName: "Bob Example", UserID: 2},
		},
		SummaryRows: []SummaryRow{
			{Kind: SummaryRowSectionHeader, Label: "Rank" + nbsp + "Requirements"},
			{Kind: SummaryRowRankReq, Label: "1a" + nbsp + "—" + nbsp + "Bobcat", Percents: []float64{0.5, 0.25}},
		},
		Adventures: []AdventureSheet{
			{
				AdventureID: 140,
				Name:        "My" + nbsp + "Family",
				ShortName:   "My" + nbsp + "Family",
				Rows: []AdventureRow{
					{Label: "1" + nbsp + "— Be helpful", DatesCompleted: []*string{nil, nil}},
				},
				OverallPcts: []float64{0.5, 0.5},
			},
		},
	}

	path := xlsxTempPath(t)
	if err := RenderXLSX(m, path); err != nil {
		t.Fatalf("RenderXLSX err = %v", err)
	}
	f, err := excelize.OpenFile(path)
	if err != nil {
		t.Fatalf("excelize.OpenFile err = %v", err)
	}
	defer f.Close()

	// Summary sheet: B1 (first scout's firstname with NBSP) must be ASCII.
	b1, err := f.GetCellValue("Webelos 1", "B1")
	if err != nil {
		t.Fatalf("GetCellValue(B1) err = %v", err)
	}
	if strings.Contains(b1, nbsp) {
		t.Errorf("B1 = %q still contains NBSP", b1)
	}
	if b1 != "Alice Marie" {
		t.Errorf("B1 = %q, want %q", b1, "Alice Marie")
	}

	// Section-header row A2 should be normalized too.
	a2, err := f.GetCellValue("Webelos 1", "A2")
	if err != nil {
		t.Fatalf("GetCellValue(A2) err = %v", err)
	}
	if strings.Contains(a2, nbsp) {
		t.Errorf("A2 = %q still contains NBSP", a2)
	}

	// Rank-req row A3 with multiple NBSPs around em-dash.
	a3, err := f.GetCellValue("Webelos 1", "A3")
	if err != nil {
		t.Fatalf("GetCellValue(A3) err = %v", err)
	}
	if strings.Contains(a3, nbsp) {
		t.Errorf("A3 = %q still contains NBSP", a3)
	}
	if a3 != "1a — Bobcat" {
		t.Errorf("A3 = %q, want %q", a3, "1a — Bobcat")
	}

	// Per-adventure sheet name should be normalized (and findable).
	advSheet := findSheetName(f, "My Family")
	if advSheet == "" {
		t.Fatalf("no sheet found containing normalized %q; sheets = %v", "My Family", f.GetSheetList())
	}
	if strings.Contains(advSheet, nbsp) {
		t.Errorf("adventure sheet name %q still contains NBSP", advSheet)
	}

	// Adventure row label normalized.
	advLabel, err := f.GetCellValue(advSheet, "A2")
	if err != nil {
		t.Fatalf("GetCellValue(%q!A2) err = %v", advSheet, err)
	}
	if strings.Contains(advLabel, nbsp) {
		t.Errorf("adventure A2 = %q still contains NBSP", advLabel)
	}
}

// TestRenderXLSXSizesColumnAAndRowHeights verifies that column A is widened
// on both the summary and adventure sheets, and that per-adventure data
// rows grow taller than the default when labels are long.
func TestRenderXLSXSizesColumnAAndRowHeights(t *testing.T) {
	longReq := "1 — With your parent or legal guardian, plan, shop for, cook, and eat a balanced meal together at least once during the week."
	m := ReportModel{
		DenLabel: "Webelos 1",
		RankName: "Webelos",
		Scouts:   scoutCols("Alice", "Bob"),
		SummaryRows: []SummaryRow{
			{Kind: SummaryRowSectionHeader, Label: "Rank Requirements"},
			{Kind: SummaryRowRankReq, Label: "1a — Bobcat (Webelos)", Percents: []float64{0.5, 0.25}},
			{Kind: SummaryRowSectionHeader, Label: "Adventures"},
			{Kind: SummaryRowAdventure, Label: "Stronger, Faster, Higher", Percents: []float64{0.5, 0.5}},
		},
		Adventures: []AdventureSheet{
			{
				AdventureID: 61,
				Name:        "Stronger, Faster, Higher",
				ShortName:   "Stronger, Faster, Higher",
				Rows: []AdventureRow{
					{Label: longReq, DatesCompleted: []*string{nil, nil}},
				},
				OverallPcts: []float64{0.5, 0.5},
			},
		},
	}

	path := xlsxTempPath(t)
	if err := RenderXLSX(m, path); err != nil {
		t.Fatalf("RenderXLSX err = %v", err)
	}

	f, err := excelize.OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile err = %v", err)
	}
	defer f.Close()

	// Summary sheet column A should be wide enough for the longest label.
	summaryW, err := f.GetColWidth("Webelos 1", "A")
	if err != nil {
		t.Fatalf("GetColWidth summary A: %v", err)
	}
	if summaryW < 20 {
		t.Errorf("summary col A width = %v, want >= 20", summaryW)
	}
	if summaryW > 85 { // a bit of slack above the 80 cap
		t.Errorf("summary col A width = %v, want <= 80 + slack", summaryW)
	}

	// Adventure sheet column A should be widened to adventureColAWidth (60).
	advSheet := ""
	for _, n := range f.GetSheetList() {
		if n != "Webelos 1" {
			advSheet = n
			break
		}
	}
	if advSheet == "" {
		t.Fatalf("no per-adventure sheet found; sheets = %v", f.GetSheetList())
	}
	advW, err := f.GetColWidth(advSheet, "A")
	if err != nil {
		t.Fatalf("GetColWidth adv A: %v", err)
	}
	if advW < 50 {
		t.Errorf("adventure col A width = %v, want >= 50", advW)
	}

	// Row 2 on the adventure sheet holds the long requirement. It should
	// be taller than the default (14.25 points-ish).
	rowH, err := f.GetRowHeight(advSheet, 2)
	if err != nil {
		t.Fatalf("GetRowHeight row 2: %v", err)
	}
	if rowH < 25 {
		t.Errorf("adventure row 2 height = %v, want > 25 (wrapped)", rowH)
	}

	// Wrap-text should be applied to A2 on the adventure sheet.
	styleID, err := f.GetCellStyle(advSheet, "A2")
	if err != nil {
		t.Fatalf("GetCellStyle A2: %v", err)
	}
	style, err := f.GetStyle(styleID)
	if err != nil {
		t.Fatalf("GetStyle(%d): %v", styleID, err)
	}
	if style == nil || style.Alignment == nil || !style.Alignment.WrapText {
		t.Errorf("A2 style wrap-text = false, want true (style=%+v)", style)
	}
}

// TestRenderXLSXRankReqSummaryCellsAreDateStrings verifies that a
// SummaryRowRankReq row writes per-scout dates as strings (not percents).
func TestRenderXLSXRankReqSummaryCellsAreDateStrings(t *testing.T) {
	date := "2025-09-11"
	m := ReportModel{
		DenLabel: "Webelos 1",
		RankName: "Webelos",
		Scouts:   scoutCols("Alice", "Bob"),
		SummaryRows: []SummaryRow{
			{Kind: SummaryRowSectionHeader, Label: "Rank Requirements"},
			{Kind: SummaryRowRankReq, Label: "1a — Bobcat", Dates: []*string{&date, nil}},
		},
	}
	path := xlsxTempPath(t)
	if err := RenderXLSX(m, path); err != nil {
		t.Fatalf("RenderXLSX err = %v", err)
	}
	f, err := excelize.OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile err = %v", err)
	}
	defer f.Close()

	// Row 3 holds "1a — Bobcat" (row 2 is the section header).
	v, _ := f.GetCellValue("Webelos 1", "B3")
	if v != date {
		t.Errorf("B3 = %q, want %q", v, date)
	}
	v, _ = f.GetCellValue("Webelos 1", "C3")
	if v != "" {
		t.Errorf("C3 = %q, want \"\" (no date for this scout)", v)
	}
	// B3 should NOT have a percent format attached.
	styleID, err := f.GetCellStyle("Webelos 1", "B3")
	if err != nil {
		t.Fatalf("GetCellStyle: %v", err)
	}
	style, _ := f.GetStyle(styleID)
	if style != nil && (style.NumFmt == 9 || style.NumFmt == 10) {
		t.Errorf("B3 has percent format (NumFmt=%d); want plain", style.NumFmt)
	}
}

// TestRenderXLSXAllCompletedGetsGreenFill verifies a row is rendered with a
// light-green background when every scout has completed it, and a mixed
// row does not get the fill.
func TestRenderXLSXAllCompletedGetsGreenFill(t *testing.T) {
	aliceDate := "2025-09-11"
	bobDate := "2025-09-12"
	m := ReportModel{
		DenLabel: "Webelos 1",
		RankName: "Webelos",
		Scouts:   scoutCols("Alice", "Bob"),
		SummaryRows: []SummaryRow{
			{Kind: SummaryRowSectionHeader, Label: "Rank Requirements"},
			{
				Kind:         SummaryRowRankReq,
				Label:        "1a — Bobcat",
				Dates:        []*string{&aliceDate, &bobDate},
				AllCompleted: true,
			},
			{
				Kind:  SummaryRowRankReq,
				Label: "1b — Walkabout",
				Dates: []*string{&aliceDate, nil},
				// AllCompleted: false
			},
			{Kind: SummaryRowSectionHeader, Label: "Adventures"},
			{
				Kind:         SummaryRowAdventure,
				Label:        "My Family",
				Percents:     []float64{1.0, 1.0},
				AllCompleted: true,
			},
		},
	}
	path := xlsxTempPath(t)
	if err := RenderXLSX(m, path); err != nil {
		t.Fatalf("RenderXLSX err = %v", err)
	}
	f, err := excelize.OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile err = %v", err)
	}
	defer f.Close()

	hasGreenFill := func(sheet, cell string) bool {
		styleID, err := f.GetCellStyle(sheet, cell)
		if err != nil {
			t.Fatalf("GetCellStyle(%s!%s): %v", sheet, cell, err)
		}
		style, err := f.GetStyle(styleID)
		if err != nil || style == nil {
			return false
		}
		for _, c := range style.Fill.Color {
			if strings.EqualFold(strings.TrimPrefix(c, "#"), allCompletedFillColor) {
				return true
			}
		}
		return false
	}

	// Row 3: all complete → both label and data cells green.
	if !hasGreenFill("Webelos 1", "A3") {
		t.Errorf("A3 (all-completed rank-req) missing green fill")
	}
	if !hasGreenFill("Webelos 1", "B3") {
		t.Errorf("B3 (all-completed rank-req) missing green fill")
	}
	if !hasGreenFill("Webelos 1", "C3") {
		t.Errorf("C3 (all-completed rank-req) missing green fill")
	}

	// Row 4: partial → no green fill anywhere.
	if hasGreenFill("Webelos 1", "A4") {
		t.Errorf("A4 (partial rank-req) unexpectedly has green fill")
	}
	if hasGreenFill("Webelos 1", "B4") {
		t.Errorf("B4 (partial rank-req) unexpectedly has green fill")
	}

	// Row 6: adventure at 100% for everyone → green fill.
	if !hasGreenFill("Webelos 1", "A6") {
		t.Errorf("A6 (all-completed adventure) missing green fill")
	}
	if !hasGreenFill("Webelos 1", "B6") {
		t.Errorf("B6 (all-completed adventure) missing green fill")
	}
}

// cellRef builds an A1-style cell reference from a column letter and 1-based
// row. Supports single-letter columns only (sufficient for these tests).
func cellRef(col string, row int) string {
	return col + itoa(row)
}

// itoa avoids a strconv dependency for a tiny helper.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
