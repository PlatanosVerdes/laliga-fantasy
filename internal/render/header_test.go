package render

import (
	"strings"
	"testing"
)

// The page has to say which build is serving it: without it the only way to tell whether a fix
// is deployed is to read the container's logs, and a stale page looks exactly like a broken fix.
func TestHeaderShowsTheBuildOnlyWhenStamped(t *testing.T) {
	Build = ""
	if got := Header("21/08/2026 16:07", "Liga", 2, nil, false, "auto"); strings.Contains(got, "build") {
		t.Errorf("sin sello no deberia anunciar version: %s", got)
	}

	Build = "v2026.08.21.3"
	defer func() { Build = "" }()
	got := Header("21/08/2026 16:07", "Liga", 2, nil, false, "auto")
	if !strings.Contains(got, `class="build"`) || !strings.Contains(got, "v2026.08.21.3") {
		t.Errorf("la cabecera deberia llevar la version: %s", got)
	}
}
