package render

import (
	"strings"
	"testing"
)

func TestPageDeclaresTheViewport(t *testing.T) {
	got := Page("", "", "", "", "", "", "", "")
	if !strings.Contains(got, `name="viewport"`) || !strings.Contains(got, "width=device-width") {
		t.Errorf("la pagina tiene que declarar el viewport: %.200s", got)
	}
}

// Marking only one end hides half a column and leaves the table misaligned.
func TestWideOnlyColumnsAreMarkedOnBothEnds(t *testing.T) {
	columns := []Column{
		{"Jugador", func(row map[string]any) any { return row["name"] }, "text"},
		{"Pts/M", func(row map[string]any) any { return row["points_value"] }, "mag"},
	}
	got := Table(columns, []map[string]any{{"name": "Mbappe", "points_value": 0.3}}, "")
	if strings.Count(got, "wide-only") != 2 {
		t.Errorf("la columna ancha tiene que marcarse en cabecera y celda: %s", got)
	}
	if strings.Contains(got, `<th data-kind="text" class="wide-only"`) {
		t.Errorf("el jugador no se esconde en ninguna pantalla: %s", got)
	}
}

func TestWideOnlyColumnsStayInTheMarkup(t *testing.T) {
	columns := []Column{
		{"Jugador", func(row map[string]any) any { return row["name"] }, "text"},
		{"Valor 7d", func(row map[string]any) any { return row["projected_pct"] }, "pct"},
		{"Score", func(row map[string]any) any { return row["score"] }, "num"},
	}
	got := Table(columns, []map[string]any{{"name": "Pedri", "projected_pct": 1.0, "score": 7.0}}, "")
	if strings.Count(got, "<th ") != 3 || strings.Count(got, "<td ") != 3 {
		t.Errorf("las tres columnas tienen que seguir en el HTML: %s", got)
	}
}
