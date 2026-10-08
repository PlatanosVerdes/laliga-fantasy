package render

import (
	"strings"
	"testing"
)

// The page has to say which build is serving it, where it is seen: without it the only way to tell whether a fix
// is deployed is to read the container's logs, and a stale page looks exactly like a broken fix.
func TestHeaderShowsTheBuildOnlyWhenStamped(t *testing.T) {
	Build = ""
	if got := Header(nil, true, "", nil); strings.Contains(got, `class="build"`) {
		t.Errorf("sin sello no deberia anunciar version: %s", got)
	}

	Build = "v2026.08.21.3"
	defer func() { Build = "" }()
	got := Header(nil, true, "", nil)
	if !strings.Contains(got, `class="build"`) || !strings.Contains(got, "v2026.08.21.3") {
		t.Errorf("la barra de arriba deberia llevar la version: %s", got)
	}
}

// The balance is the number every button on this page is judged against, so it rides in the tab
// bar, which is the only strip that stays on screen while a table is scrolled. In the header
// alone it was gone after one scroll.
func TestTabBarCarriesTheBalance(t *testing.T) {
	got := TabsWith("18.21M")
	if !strings.Contains(got, `id="tab-cash"`) || !strings.Contains(got, "18.21M") {
		t.Errorf("la barra tiene que llevar el saldo: %s", got)
	}
	if strings.Index(got, "18.21M") < strings.Index(got, "Ranking") {
		t.Error("el saldo va al final de la barra, no en medio de las pestañas")
	}
	// No session, no balance: an empty chip would read as being broke.
	if TabsWith("") != Tabs {
		t.Error("sin saldo la barra se queda como estaba")
	}
}

// The page does arithmetic with the balance while somebody types an amount, so the chip carries
// the exact figure and not only the rounded label it shows.
func TestTabBarStampsTheExactBalance(t *testing.T) {
	amount := 18_205_453.0
	got := TabsWithAmount("18.21M", &amount)
	if !strings.Contains(got, `data-cash="18205453"`) {
		t.Errorf("la barra tiene que llevar la cifra exacta: %s", got)
	}
	// Without a figure there is nothing to stamp, and a zero would read as being broke.
	if strings.Contains(TabsWithAmount("18.21M", nil), "data-cash") {
		t.Error("sin cifra no se estampa nada")
	}
}

// The tab bar comes first with the balance and the search on its right, then the four cards
// and nothing else: where the page comes from and the other figures go to the foot.
func TestHeaderIsTabsThenFourCards(t *testing.T) {
	stats := []string{StatCard(Stat{Label: "Caja", Value: "76,5M", ValueID: "kpi-cash"}),
		StatCard(Stat{Label: "Jornada 8", Value: "1d 2h", Deadline: "2026-10-09T21:00:00+02:00"})}
	got := Header(stats, true, "76,5M", nil)
	tabs, cash := strings.Index(got, `id="tabs"`), strings.Index(got, `id="tab-cash"`)
	find, strip := strings.Index(got, `id="find"`), strings.Index(got, `class="strip"`)
	if tabs < 0 || cash < tabs || find < cash || strip < find {
		t.Errorf("orden: pestañas, saldo, buscador y tarjetas: %s", got)
	}
	if !strings.Contains(got, `id="live-dot"`) || !strings.Contains(got, `data-deadline=`) {
		t.Errorf("el punto en vivo y la cuenta atrás siguen siendo enganches del script: %s", got)
	}
	if strings.Contains(got, "más datos") || strings.Contains(got, "LaLiga Fantasy") {
		t.Error("entre las tarjetas y el contenido no va nada")
	}
	foot := PageFoot("08/10/2026 20:52", "Liga", 8,
		[]string{Widget(KPI{Label: "Valor de plantilla", Value: "205.39M"})}, "auto")
	if !strings.Contains(foot, "más datos") || !strings.Contains(foot, `id="live-stamp"`) {
		t.Errorf("el pie lleva la procedencia y el resto de cifras: %s", foot)
	}
	if !strings.Contains(Tabs, `data-tab="comparador"`) {
		t.Error("el comparador es una pestaña")
	}
}
