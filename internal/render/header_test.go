package render

import (
	"strings"
	"testing"
)

// The page says which build is serving it, in the live dot's tooltip: without it the only way
// to tell whether a fix is deployed is to read the container's logs.
func TestHeaderShowsTheBuildOnlyWhenStamped(t *testing.T) {
	Build = ""
	if got := Header(nil, true, nil); strings.Contains(got, `data-build=`) || strings.Contains(got, "build-tag") {
		t.Errorf("sin sello no deberia anunciar version: %s", got)
	}
	Build = "v2026.08.21.3"
	defer func() { Build = "" }()
	got := Header(nil, true, nil)
	if !strings.Contains(got, `data-build="v2026.08.21.3"`) || !strings.Contains(got, "· v2026.08.21.3") {
		t.Errorf("el punto en vivo lleva la version: %s", got)
	}
	if !strings.Contains(got, `<span class="build-tag">v2026.08.21.3</span>`) {
		t.Errorf("la version se ve sin pasar el raton: %s", got)
	}
}

// The tab bar keeps its room for the tabs: no balance chip (the Caja card has it), the exact
// figure stamped on the bar for the arithmetic, and the search folded into a magnifier.
func TestTabBarLeavesRoomForTheTabs(t *testing.T) {
	amount := 18_205_453.0
	got := Header(nil, true, &amount)
	if strings.Contains(got, "tab-cash") || !strings.Contains(got, `data-cash="18205453"`) {
		t.Errorf("sin chip de saldo, con la cifra exacta: %s", got)
	}
	if !strings.Contains(got, `class="find-btn"`) || !strings.Contains(got, `<div class="find-pop" hidden>`) {
		t.Errorf("la busqueda es una lupa: %s", got)
	}
	if strings.Contains(Header(nil, true, nil), "data-cash") {
		t.Error("sin cifra no se estampa nada")
	}
}

// The tab bar comes first with the search on its right, then the four cards and nothing else:
// where the page comes from and the other figures go to the foot.
func TestHeaderIsTabsThenFourCards(t *testing.T) {
	stats := []string{StatCard(Stat{Label: "Caja", Value: "76,5M", ValueID: "kpi-cash"}),
		StatCard(Stat{Label: "Jornada 8", Value: "1d 2h", Deadline: "2026-10-09T21:00:00+02:00"})}
	got := Header(stats, true, nil)
	tabs := strings.Index(got, `id="tabs"`)
	find, strip := strings.Index(got, `id="find"`), strings.Index(got, `class="strip"`)
	if tabs < 0 || find < tabs || strip < find {
		t.Errorf("orden: pestañas, buscador y tarjetas: %s", got)
	}
	if !strings.Contains(got, `id="live-dot"`) || !strings.Contains(got, `data-deadline=`) {
		t.Errorf("el punto en vivo y la cuenta atrás siguen siendo enganches del script: %s", got)
	}
	if strings.Contains(got, "más datos") || strings.Contains(got, "LaLiga Fantasy") {
		t.Error("entre las tarjetas y el contenido no va nada")
	}
	foot := PageFoot("08/10/2026 20:52", "Liga", 8, "auto")
	if strings.Contains(foot, "más datos") || !strings.Contains(foot, `id="live-stamp"`) ||
		!strings.Contains(foot, "jornada 8") {
		t.Errorf("el pie lleva solo la procedencia: %s", foot)
	}
	if !strings.Contains(Tabs, `data-tab="comparador"`) {
		t.Error("el comparador es una pestaña")
	}
}
