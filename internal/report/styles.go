package report

import (
	"fmt"

	excelize "github.com/xuri/excelize/v2"
)

// styleSpec captures the combinations of cell styling the renderer needs.
// Each spec maps to a single cached excelize style id.
type styleSpec struct {
	bold      bool
	percent   bool
	wrap      bool
	fillGreen bool
}

// styleCache lazily creates excelize styles for unique styleSpec values and
// hands out the same style id on subsequent requests. Without this, a
// workbook with dozens of rows would spawn thousands of redundant style
// definitions, bloating the file.
type styleCache struct {
	f     *excelize.File
	byKey map[styleSpec]int
}

func newStyleCache(f *excelize.File) *styleCache {
	return &styleCache{f: f, byKey: map[styleSpec]int{}}
}

func (c *styleCache) get(spec styleSpec) (int, error) {
	if id, ok := c.byKey[spec]; ok {
		return id, nil
	}
	style := &excelize.Style{}
	if spec.bold {
		style.Font = &excelize.Font{Bold: true}
	}
	if spec.percent {
		style.NumFmt = 9 // built-in "0%"
	}
	if spec.wrap {
		style.Alignment = &excelize.Alignment{WrapText: true, Vertical: "top"}
	}
	if spec.fillGreen {
		style.Fill = excelize.Fill{
			Type:    "pattern",
			Pattern: 1,
			Color:   []string{allCompletedFillColor},
		}
	}
	id, err := c.f.NewStyle(style)
	if err != nil {
		return 0, fmt.Errorf("new style %+v: %w", spec, err)
	}
	c.byKey[spec] = id
	return id, nil
}
