package render

import (
	"strings"
	"testing"
)

// The live refresh swaps sections by id with a non-greedy match, so every view has to be one
// section with nothing nested, and each one names its tab.
func TestViewsAreFlatSectionsOfTheirTab(t *testing.T) {
	views := decidingDocument().Views()
	if len(views) < 2*len(ViewTabs) {
		t.Fatalf("a view and a 'Ver detalle' per tab, got %d", len(views))
	}
	for index, html := range views[:2*len(ViewTabs)] {
		tab := ViewTabs[index/2]
		if strings.Count(html, "<section") != 1 || strings.Count(html, "</section>") != 1 {
			t.Errorf("%s: one section, nothing nested: %.120s", tab, html)
		}
		if !strings.Contains(html, `data-tab="`+tab+`"`) {
			t.Errorf("%s: the view has to name its tab: %.120s", tab, html)
		}
	}
}

// A confirmed absence rings red with its badge, a doubt yellow.
func TestFacesWearTheStatusRing(t *testing.T) {
	out := face(map[string]any{"id": "1", "name": "Pedri", "status": "injured",
		"absence": map[string]any{"kind": "lesionado", "reason": "Rotura"}}, "sm")
	if !strings.Contains(out, "ring-out") || !strings.Contains(out, "hb-cross") ||
		!strings.Contains(out, `title="Rotura"`) {
		t.Errorf("lesionado: %s", out)
	}
	doubt := face(map[string]any{"id": "2", "name": "Gavi", "status": "doubtful"}, "sm")
	if !strings.Contains(doubt, "ring-doubt") || strings.Contains(doubt, "hb-") {
		t.Errorf("duda: %s", doubt)
	}
}
