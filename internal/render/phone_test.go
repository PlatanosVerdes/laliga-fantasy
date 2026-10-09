package render

import (
	"strings"
	"testing"
)

func TestPageDeclaresTheViewport(t *testing.T) {
	got := Page("", "", "{}")
	if !strings.Contains(got, `name="viewport"`) || !strings.Contains(got, "width=device-width") {
		t.Errorf("la pagina tiene que declarar el viewport: %.200s", got)
	}
}
