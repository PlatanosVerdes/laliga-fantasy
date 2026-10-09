package render

import (
	"strings"
	"testing"
)

// The comparator marks the best of each row, the cheapest on the cost rows, and says whether the
// outsider improves on my best of his line.
func TestCompareTableMarksTheBestAndJudges(t *testing.T) {
	mine := map[string]any{"id": "1", "name": "Mio", "position": "DEL", "is_mine": true,
		"xpts": 4.0, "value": 10_000_000.0, "available": true}
	him := map[string]any{"id": "2", "name": "Otro", "position": "DEL", "xpts": 6.0,
		"value": 30_000_000.0, "available": true}
	table := CompareTable([]map[string]any{mine, him})
	var value, xpts CompareRow
	for _, row := range table.Rows {
		switch row.Label {
		case "Valor":
			value = row
		case "xPts por jornada":
			xpts = row
		}
	}
	if value.Cells[0].C != "cmp-cheap" || xpts.Cells[1].C != "cmp-best" || xpts.Cells[0].C != "" {
		t.Errorf("cheapest value and best xPts: %+v %+v", value, xpts)
	}
	verdict := asJSON(table.Verdict)
	if !strings.Contains(verdict, "mejora a tu mejor DEL") || !strings.Contains(verdict, "+2.00 xPts") ||
		!strings.Contains(verdict, "20.00M mas") {
		t.Errorf("verdict: %s", verdict)
	}
}
