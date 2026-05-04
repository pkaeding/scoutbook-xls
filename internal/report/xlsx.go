package report

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	excelize "github.com/xuri/excelize/v2"
)

// Column-width + row-height constants used by the renderer. These are in
// Excel's native units: column widths are "characters of the default font"
// and row heights are points.
const (
	// summaryColAMin is the floor for the summary sheet's column A width.
	summaryColAMin = 24
	// summaryColAMax is the cap so an unusually long label doesn't push
	// scout columns entirely offscreen.
	summaryColAMax = 80
	// summaryColAPadding is added to the longest label's character count to
	// leave visual breathing room.
	summaryColAPadding = 4

	// scoutColWidth is the width of each scout column on every sheet.
	scoutColWidth = 12

	// adventureColAWidth is the fixed width for column A on per-adventure
	// sheets. Requirement text wraps inside this width.
	adventureColAWidth = 60
	// adventureRowLineHeight is the per-wrapped-line height (points) for
	// adventure requirement rows.
	adventureRowLineHeight = 15
	// adventureMinRowHeight is the minimum row height on adventure sheets
	// so single-line rows still feel roomy.
	adventureMinRowHeight = 18
)

// normalizeSpaces collapses runs of Unicode whitespace (including U+00A0
// non-breaking space, which the Scouting API emits between first and last
// names) into single ASCII spaces, and trims ends. Used on every string
// value written to a cell so names and labels render cleanly.
func normalizeSpaces(s string) string {
	if s == "" {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			if b.Len() > 0 {
				prevSpace = true
			}
			continue
		}
		if prevSpace {
			b.WriteByte(' ')
			prevSpace = false
		}
		b.WriteRune(r)
	}
	return b.String()
}

// RenderXLSX writes a ReportModel to an XLSX workbook at path.
//
// Layout:
//   - One summary sheet named m.DenLabel containing a header row
//     ("Requirement" + scout first names) followed by one row per
//     SummaryRow. Percents are stored as raw floats with a "0%" number
//     format; section headers only populate column A.
//   - One per-adventure sheet for each AdventureSheet in m.Adventures,
//     named after the adventure (truncated to 31 chars, with a
//     " (2)" / " (3)" suffix on collision). Each sheet has the same
//     header row, one row per requirement with DateCompleted strings
//     written into scout columns, and a final "% Complete" row with
//     OverallPcts formatted as percentages.
// allCompletedFillColor is the light green background applied to any row
// where every scout has completed the requirement or adventure.
const allCompletedFillColor = "D9EAD3"

func RenderXLSX(m ReportModel, path string) error {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	styles := newStyleCache(f)
	boldID, err := styles.get(styleSpec{bold: true})
	if err != nil {
		return err
	}

	// Track sheet name usage so truncated/collision handling produces
	// unique names across the whole workbook.
	usedNames := map[string]bool{}

	// Build the summary sheet.
	summarySheet := uniqueSheetName(normalizeSpaces(m.DenLabel), usedNames)

	// Rename the default sheet to the den label (or our uniquified form).
	defaultSheet := f.GetSheetName(0)
	if defaultSheet != summarySheet {
		if err := f.SetSheetName(defaultSheet, summarySheet); err != nil {
			return fmt.Errorf("set summary sheet name: %w", err)
		}
	}

	if err := writeHeaderRow(f, summarySheet, m.Scouts, boldID); err != nil {
		return err
	}

	// Column A on the summary: size to fit the longest label (no wrap).
	summaryColA := summaryColAWidthFor(m.SummaryRows)
	if err := f.SetColWidth(summarySheet, "A", "A", summaryColA); err != nil {
		return fmt.Errorf("set summary col A width: %w", err)
	}
	// Scout columns: fixed width.
	if len(m.Scouts) > 0 {
		firstScoutCol := colLetter(1)
		lastScoutCol := colLetter(len(m.Scouts))
		if err := f.SetColWidth(summarySheet, firstScoutCol, lastScoutCol, scoutColWidth); err != nil {
			return fmt.Errorf("set summary scout col widths: %w", err)
		}
	}

	for i, row := range m.SummaryRows {
		rowIdx := i + 2 // row 1 is the header
		aCell := cellRefXY(0, rowIdx)
		if err := f.SetCellValue(summarySheet, aCell, normalizeSpaces(row.Label)); err != nil {
			return fmt.Errorf("set summary label %s: %w", aCell, err)
		}

		// Label-cell style: bold for section headers; section headers don't
		// get the green fill even if downstream data is all complete.
		switch row.Kind {
		case SummaryRowSectionHeader:
			if err := f.SetCellStyle(summarySheet, aCell, aCell, boldID); err != nil {
				return fmt.Errorf("style summary header %s: %w", aCell, err)
			}
			continue
		}

		// Label cell on a data row: plain text, optionally filled green.
		labelStyle, err := styles.get(styleSpec{fillGreen: row.AllCompleted})
		if err != nil {
			return err
		}
		if err := f.SetCellStyle(summarySheet, aCell, aCell, labelStyle); err != nil {
			return fmt.Errorf("style summary label %s: %w", aCell, err)
		}

		// Data cells: percentage for adventures, date strings for rank reqs.
		switch row.Kind {
		case SummaryRowRankReq:
			dataStyle, err := styles.get(styleSpec{fillGreen: row.AllCompleted})
			if err != nil {
				return err
			}
			for j, datePtr := range row.Dates {
				cell := cellRefXY(j+1, rowIdx)
				if datePtr != nil {
					if err := f.SetCellStr(summarySheet, cell, normalizeSpaces(*datePtr)); err != nil {
						return fmt.Errorf("set summary date %s: %w", cell, err)
					}
				}
				if err := f.SetCellStyle(summarySheet, cell, cell, dataStyle); err != nil {
					return fmt.Errorf("style summary date %s: %w", cell, err)
				}
			}
		case SummaryRowAdventure:
			dataStyle, err := styles.get(styleSpec{percent: true, fillGreen: row.AllCompleted})
			if err != nil {
				return err
			}
			for j, pct := range row.Percents {
				cell := cellRefXY(j+1, rowIdx)
				if err := f.SetCellValue(summarySheet, cell, pct); err != nil {
					return fmt.Errorf("set summary pct %s: %w", cell, err)
				}
				if err := f.SetCellStyle(summarySheet, cell, cell, dataStyle); err != nil {
					return fmt.Errorf("style summary pct %s: %w", cell, err)
				}
			}
		}
	}

	// Per-adventure sheets.
	for _, adv := range m.Adventures {
		raw := adv.Name
		if raw == "" {
			raw = adv.ShortName
		}
		sheet := uniqueSheetName(normalizeSpaces(raw), usedNames)
		if _, err := f.NewSheet(sheet); err != nil {
			return fmt.Errorf("new sheet %q: %w", sheet, err)
		}

		if err := writeHeaderRow(f, sheet, m.Scouts, boldID); err != nil {
			return err
		}

		// Column A wide + wrap-text so requirement text flows onto multiple
		// visible lines. Scout columns get the standard fixed width.
		if err := f.SetColWidth(sheet, "A", "A", adventureColAWidth); err != nil {
			return fmt.Errorf("set adventure col A width: %w", err)
		}
		if len(m.Scouts) > 0 {
			firstScoutCol := colLetter(1)
			lastScoutCol := colLetter(len(m.Scouts))
			if err := f.SetColWidth(sheet, firstScoutCol, lastScoutCol, scoutColWidth); err != nil {
				return fmt.Errorf("set adventure scout col widths: %w", err)
			}
		}

		for i, row := range adv.Rows {
			rowIdx := i + 2
			aCell := cellRefXY(0, rowIdx)
			label := normalizeSpaces(row.Label)
			if err := f.SetCellValue(sheet, aCell, label); err != nil {
				return fmt.Errorf("set adventure label %s: %w", aCell, err)
			}
			// Wrap-text (+ optional green fill) on the label cell.
			labelStyle, err := styles.get(styleSpec{wrap: true, fillGreen: row.AllCompleted})
			if err != nil {
				return err
			}
			if err := f.SetCellStyle(sheet, aCell, aCell, labelStyle); err != nil {
				return fmt.Errorf("style adventure label %s: %w", aCell, err)
			}
			// Grow the row so the wrapped text is all visible.
			height := estimateRowHeight(label, adventureColAWidth)
			if err := f.SetRowHeight(sheet, rowIdx, height); err != nil {
				return fmt.Errorf("set adventure row height %d: %w", rowIdx, err)
			}
			// Data cells: dates, green-filled when every scout is done.
			dateStyle, err := styles.get(styleSpec{fillGreen: row.AllCompleted})
			if err != nil {
				return err
			}
			for j, datePtr := range row.DatesCompleted {
				cell := cellRefXY(j+1, rowIdx)
				if datePtr != nil {
					if err := f.SetCellStr(sheet, cell, normalizeSpaces(*datePtr)); err != nil {
						return fmt.Errorf("set date %s: %w", cell, err)
					}
				}
				if err := f.SetCellStyle(sheet, cell, cell, dateStyle); err != nil {
					return fmt.Errorf("style date %s: %w", cell, err)
				}
			}
		}

		// "% Complete" row. Green fill when every scout is at 100%.
		pctRowIdx := len(adv.Rows) + 2
		labelCell := cellRefXY(0, pctRowIdx)
		if err := f.SetCellValue(sheet, labelCell, "% Complete"); err != nil {
			return fmt.Errorf("set %%complete label: %w", err)
		}
		allDoneOverall := len(adv.OverallPcts) > 0
		for _, p := range adv.OverallPcts {
			if p < 1.0 {
				allDoneOverall = false
				break
			}
		}
		labelStyle, err := styles.get(styleSpec{bold: true, fillGreen: allDoneOverall})
		if err != nil {
			return err
		}
		if err := f.SetCellStyle(sheet, labelCell, labelCell, labelStyle); err != nil {
			return fmt.Errorf("style %%complete label: %w", err)
		}
		pctStyle, err := styles.get(styleSpec{bold: true, percent: true, fillGreen: allDoneOverall})
		if err != nil {
			return err
		}
		for j, pct := range adv.OverallPcts {
			cell := cellRefXY(j+1, pctRowIdx)
			if err := f.SetCellValue(sheet, cell, pct); err != nil {
				return fmt.Errorf("set overall pct %s: %w", cell, err)
			}
			if err := f.SetCellStyle(sheet, cell, cell, pctStyle); err != nil {
				return fmt.Errorf("style overall pct %s: %w", cell, err)
			}
		}
	}

	// Make sure the summary sheet is the active/first sheet.
	if idx, err := f.GetSheetIndex(summarySheet); err == nil && idx >= 0 {
		f.SetActiveSheet(idx)
	}

	if err := f.SaveAs(path); err != nil {
		return fmt.Errorf("save xlsx: %w", err)
	}
	return nil
}

// writeHeaderRow writes the "Requirement" label in A1 plus one scout first
// name per column across row 1, all styled bold.
func writeHeaderRow(f *excelize.File, sheet string, scouts []ScoutCol, boldID int) error {
	a1 := "A1"
	if err := f.SetCellValue(sheet, a1, "Requirement"); err != nil {
		return fmt.Errorf("set %s header on %q: %w", a1, sheet, err)
	}
	if err := f.SetCellStyle(sheet, a1, a1, boldID); err != nil {
		return fmt.Errorf("style %s on %q: %w", a1, sheet, err)
	}
	for i, s := range scouts {
		cell := cellRefXY(i+1, 1)
		if err := f.SetCellValue(sheet, cell, normalizeSpaces(s.FirstName)); err != nil {
			return fmt.Errorf("set header %s on %q: %w", cell, sheet, err)
		}
		if err := f.SetCellStyle(sheet, cell, cell, boldID); err != nil {
			return fmt.Errorf("style header %s on %q: %w", cell, sheet, err)
		}
	}
	return nil
}

// colLetter returns the Excel column letters for a 0-indexed column number.
// Handles arbitrary column widths (A..Z, AA..ZZ, AAA...).
func colLetter(n int) string {
	if n < 0 {
		return ""
	}
	var out []byte
	n++ // shift to 1-indexed for the base-26 conversion
	for n > 0 {
		n--
		out = append([]byte{byte('A' + n%26)}, out...)
		n /= 26
	}
	return string(out)
}

// cellRefXY builds an A1-style reference from a 0-indexed column and a
// 1-indexed row.
func cellRefXY(col0 int, row1 int) string {
	return fmt.Sprintf("%s%d", colLetter(col0), row1)
}

// uniqueSheetName truncates a candidate sheet name to Excel's 31-char limit
// and resolves collisions with already-used names by appending " (2)",
// " (3)", and so on inside the 31-char window. The returned name is marked
// as used in the provided map.
func uniqueSheetName(candidate string, used map[string]bool) string {
	base := truncate31(candidate)
	if !used[base] {
		used[base] = true
		return base
	}
	for i := 2; ; i++ {
		suffix := fmt.Sprintf(" (%d)", i)
		// Leave enough room for suffix inside the 31-char window.
		max := 31 - len(suffix)
		if max < 0 {
			max = 0
		}
		trunc := candidate
		if len(trunc) > max {
			trunc = trunc[:max]
		}
		name := trunc + suffix
		if len(name) > 31 {
			name = name[:31]
		}
		if !used[name] {
			used[name] = true
			return name
		}
	}
}

// truncate31 truncates s to at most 31 bytes (Excel's sheet-name limit).
func truncate31(s string) string {
	if len(s) <= 31 {
		return s
	}
	return s[:31]
}

// summaryColAWidthFor returns the width (in Excel character units) to use
// for column A of the summary sheet, sized to fit the longest label plus
// padding, clamped to [summaryColAMin, summaryColAMax].
func summaryColAWidthFor(rows []SummaryRow) float64 {
	longest := len("Requirement")
	for _, r := range rows {
		n := utf8.RuneCountInString(normalizeSpaces(r.Label))
		if n > longest {
			longest = n
		}
	}
	w := float64(longest + summaryColAPadding)
	if w < summaryColAMin {
		w = summaryColAMin
	}
	if w > summaryColAMax {
		w = summaryColAMax
	}
	return w
}

// estimateRowHeight approximates a row height (points) for a wrap-text cell
// containing label, given the column's character width. The math is a rough
// heuristic — Excel's exact rendering depends on font metrics — but it's
// good enough to make sure nothing's clipped.
func estimateRowHeight(label string, colWidthChars float64) float64 {
	if colWidthChars < 1 {
		colWidthChars = 1
	}
	// Count wrap lines. A hard newline bumps the line count; otherwise the
	// label wraps at roughly colWidthChars rune boundaries. This
	// overestimates slightly for short last-lines, which is fine.
	lines := 1
	runes := 0
	for _, r := range label {
		if r == '\n' {
			lines++
			runes = 0
			continue
		}
		runes++
		if float64(runes) >= colWidthChars {
			lines++
			runes = 0
		}
	}
	h := float64(lines) * adventureRowLineHeight
	if h < adventureMinRowHeight {
		h = adventureMinRowHeight
	}
	return h
}
