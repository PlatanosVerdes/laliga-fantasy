package render

import (
	"strings"
	"testing"
)

// The page has to say which build is serving it: without it the only way to tell whether a fix
// is deployed is to read the container's logs, and a stale page looks exactly like a broken fix.
func TestHeaderShowsTheBuildOnlyWhenStamped(t *testing.T) {
	Build = ""
	if got := Header("21/08/2026 16:07", "Liga", 2, nil, false, "auto", ""); strings.Contains(got, "build") {
		t.Errorf("sin sello no deberia anunciar version: %s", got)
	}

	Build = "v2026.08.21.3"
	defer func() { Build = "" }()
	got := Header("21/08/2026 16:07", "Liga", 2, nil, false, "auto", "")
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
