package render

import (
	"strings"
	"testing"
)

// The page has to say which build is serving it: without it the only way to tell whether a fix
// is deployed is to read the container's logs, and a stale page looks exactly like a broken fix.
func TestHeaderShowsTheBuildOnlyWhenStamped(t *testing.T) {
	Build = ""
	if got := Header("21/08/2026 16:07", "Liga", 2, nil, nil, false, "auto", "", nil); strings.Contains(got, "build") {
		t.Errorf("sin sello no deberia anunciar version: %s", got)
	}

	Build = "v2026.08.21.3"
	defer func() { Build = "" }()
	got := Header("21/08/2026 16:07", "Liga", 2, nil, nil, false, "auto", "", nil)
	if !strings.Contains(got, `class="build"`) || !strings.Contains(got, "v2026.08.21.3") {
		t.Errorf("la cabecera deberia llevar la version: %s", got)
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

// The tab bar comes first with the search beside it, then the four cards; the other figures
// wait folded under "más datos" instead of filling the screen above every tab.
func TestHeaderOpensWithTabsThenFourCards(t *testing.T) {
	stats := []string{StatCard(Stat{Label: "Caja", Value: "76.45M", ValueID: "kpi-cash"}),
		StatCard(Stat{Label: "Jornada 8", Value: "1d 2h", Deadline: "2026-10-09T21:00:00+02:00"})}
	more := []string{Widget(KPI{Label: "Valor de plantilla", Value: "205.39M"})}
	got := Header("08/10/2026 20:52", "Liga", 8, stats, more, true, "auto", "76.45M", nil)
	tabs, find := strings.Index(got, `id="tabs"`), strings.Index(got, `id="find"`)
	strip, folded := strings.Index(got, `class="strip"`), strings.Index(got, "más datos")
	if tabs < 0 || find < tabs || strip < find || folded < strip {
		t.Errorf("orden: pestañas, buscador, tarjetas y luego más datos: %s", got)
	}
	if !strings.Contains(got, `id="kpi-cash"`) || !strings.Contains(got, `data-deadline=`) {
		t.Errorf("la caja y la cuenta atrás siguen siendo enganches del script: %s", got)
	}
	if strings.Index(got, "Valor de plantilla") < folded {
		t.Error("el resto de cifras va plegado")
	}
	if !strings.Contains(got, `data-tab="comparador"`) {
		t.Error("el comparador es una pestaña")
	}
}
