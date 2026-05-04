package report

import (
	"fmt"
	"strings"
	"unicode"

	excelize "github.com/xuri/excelize/v2"
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
func RenderXLSX(m ReportModel, path string) error {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	// Styles we'll reuse.
	boldID, err := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true},
	})
	if err != nil {
		return fmt.Errorf("new bold style: %w", err)
	}
	pctFmt := 9 // built-in "0%"
	pctID, err := f.NewStyle(&excelize.Style{
		NumFmt: pctFmt,
	})
	if err != nil {
		return fmt.Errorf("new percent style: %w", err)
	}
	boldPctID, err := f.NewStyle(&excelize.Style{
		Font:   &excelize.Font{Bold: true},
		NumFmt: pctFmt,
	})
	if err != nil {
		return fmt.Errorf("new bold+percent style: %w", err)
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

	for i, row := range m.SummaryRows {
		rowIdx := i + 2 // row 1 is the header
		aCell := cellRefXY(0, rowIdx)
		if err := f.SetCellValue(summarySheet, aCell, normalizeSpaces(row.Label)); err != nil {
			return fmt.Errorf("set summary label %s: %w", aCell, err)
		}
		switch row.Kind {
		case SummaryRowSectionHeader:
			// Bold the label; leave scout columns empty.
			if err := f.SetCellStyle(summarySheet, aCell, aCell, boldID); err != nil {
				return fmt.Errorf("style summary header %s: %w", aCell, err)
			}
		case SummaryRowRankReq, SummaryRowAdventure:
			for j, pct := range row.Percents {
				cell := cellRefXY(j+1, rowIdx)
				if err := f.SetCellValue(summarySheet, cell, pct); err != nil {
					return fmt.Errorf("set summary pct %s: %w", cell, err)
				}
				if err := f.SetCellStyle(summarySheet, cell, cell, pctID); err != nil {
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

		for i, row := range adv.Rows {
			rowIdx := i + 2
			aCell := cellRefXY(0, rowIdx)
			if err := f.SetCellValue(sheet, aCell, normalizeSpaces(row.Label)); err != nil {
				return fmt.Errorf("set adventure label %s: %w", aCell, err)
			}
			for j, datePtr := range row.DatesCompleted {
				if datePtr == nil {
					continue
				}
				cell := cellRefXY(j+1, rowIdx)
				if err := f.SetCellStr(sheet, cell, normalizeSpaces(*datePtr)); err != nil {
					return fmt.Errorf("set date %s: %w", cell, err)
				}
			}
		}

		// "% Complete" row.
		pctRowIdx := len(adv.Rows) + 2
		labelCell := cellRefXY(0, pctRowIdx)
		if err := f.SetCellValue(sheet, labelCell, "% Complete"); err != nil {
			return fmt.Errorf("set %%complete label: %w", err)
		}
		if err := f.SetCellStyle(sheet, labelCell, labelCell, boldID); err != nil {
			return fmt.Errorf("style %%complete label: %w", err)
		}
		for j, pct := range adv.OverallPcts {
			cell := cellRefXY(j+1, pctRowIdx)
			if err := f.SetCellValue(sheet, cell, pct); err != nil {
				return fmt.Errorf("set overall pct %s: %w", cell, err)
			}
			if err := f.SetCellStyle(sheet, cell, cell, boldPctID); err != nil {
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
