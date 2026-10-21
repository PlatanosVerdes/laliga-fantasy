package futbolfantasy

import (
	"os"
	"strings"
	"testing"
)

func detailFixture(t *testing.T, name string) map[string]any {
	t.Helper()
	page, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return ParseDetail(string(page))
}

func floatOf(value any) float64 {
	if found, ok := value.(*float64); ok && found != nil {
		return *found
	}
	return 0
}

// The season's high and low are what the page's script ends with, not its first or last line:
// both pages were read on 21 Oct, Sancet's high is from August and his low is today.
func TestTheSeasonRangeIsWhatThePageWorksOut(t *testing.T) {
	for _, want := range []struct {
		page            string
		high, low       float64
		highDay, lowDay string
	}{
		{"detalle_sancet_lesionado.html", 52_140_968, 23_659_485, "09 ago.", "21 oct."},
		{"detalle_koski_disponible.html", 41_329_279, 763_981, "21 oct.", "14 ago."},
	} {
		detail := detailFixture(t, want.page)
		if got := floatOf(detail["max_value"]); got != want.high {
			t.Errorf("%s: max %v, want %v", want.page, got, want.high)
		}
		if got := floatOf(detail["min_value"]); got != want.low {
			t.Errorf("%s: min %v, want %v", want.page, got, want.low)
		}
		if got := text(detail["max_date"]); got != want.highDay {
			t.Errorf("%s: max date %q, want %q", want.page, got, want.highDay)
		}
		if got := text(detail["min_date"]); got != want.lowDay {
			t.Errorf("%s: min date %q, want %q", want.page, got, want.lowDay)
		}
	}
}

// An open injury has its end put on today and drawn nowhere; a closed one is drawn.
func TestTheOpenInjuryIsReadOffTheChart(t *testing.T) {
	mark, _ := detailFixture(t, "detalle_sancet_lesionado.html")["mark"].(*Mark)
	if mark == nil || *mark != (Mark{State: "lesionado", Reason: "Lesión en el sóleo", Since: "07/10"}) {
		t.Errorf("Sancet: %+v", mark)
	}
	// Koski's page carries four injuries, every one of them over.
	mark, _ = detailFixture(t, "detalle_koski_disponible.html")["mark"].(*Mark)
	if mark == nil || *mark != (Mark{}) {
		t.Errorf("Koski: %+v", mark)
	}
}

func chartMark(condition, image, note string) string {
	return condition + "\n\t\tchart_annotations.push(\n\t\t\t{\n\t\t\t\ttype: 'line',\n\t\t\t\t" +
		"xMin: '07/10',\n\t\t\t\tcontent: " + image + "Image,\n\t\t\t\talert(\"" + note + "\");\n\t\t\t}\n\t\t);\n"
}

const (
	drawnMark = "if((days > 0 && true && 14 < days) || (days == 0 && true) || (days == -1 && false))"
	openEnd   = "if((days > 0 && false && 0 < days) || (days == 0 && false) || (days == -1 && false))"
)

// What the chart does not say in words it knows is not read as anything: no chart, no mark;
// an open period drawn with an image of its own, no mark either.
func TestAnUnknownMarkSaysNothing(t *testing.T) {
	if mark := parseMark(`<script>max_valor = 1;</script>`); mark != nil {
		t.Errorf("a page without its chart: %+v", mark)
	}
	page := "chart_annotations = [];\n" + chartMark(drawnMark, "convocado", "Inicio Lesión 07/10 : Algo") +
		chartMark(openEnd, "disponible", "Fin Lesión 21/10 : Algo")
	if mark := parseMark(page); mark != nil {
		t.Errorf("an image it does not know: %+v", mark)
	}
	page = strings.Replace(page, "convocado", "sancionado", 1)
	if mark := parseMark(page); mark == nil || mark.State != "sancionado" || mark.Reason != "Algo" {
		t.Errorf("one it does: %+v", mark)
	}
}
