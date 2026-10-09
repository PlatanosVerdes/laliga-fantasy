package render

import (
	"strings"
	"testing"
)

// The page is a shell: the views as JSON, the module that draws them versioned by the build so
// a deploy is picked up, and a line for whoever has JavaScript off.
func TestPageIsAShellAroundTheViews(t *testing.T) {
	Build = "v2026.08.21.3"
	defer func() { Build = "" }()
	got := Page("body{}", "", `{"a":1}`)
	for _, want := range []string{`name="viewport"`, `<div class="wrap" id="app"></div>`,
		`<noscript>`, `<script type="application/json" id="views-data">{"a":1}</script>`,
		`src="/assets/ui/main.js?v=v2026.08.21.3"`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in %.300s", want, got)
		}
	}
}

// The page's own data: the tabs with the comparator, the four cards with the balance first and
// a live countdown, and the sections each naming its tab and what fills it; the build and the
// mode ride in meta, where the live dot and the dialogs read them.
func TestPageDataDescribesTheShell(t *testing.T) {
	Build = "v1"
	defer func() { Build = "" }()
	document := decidingDocument()
	document.Mode = "auto"
	_, views := document.Render()
	page := views["page"].(PageData)
	if len(page.Tabs) != 10 || page.Tabs[9].ID != "comparador" {
		t.Errorf("tabs: %+v", page.Tabs)
	}
	if len(page.Stats) == 0 || page.Stats[0].ValueID != "kpi-cash" {
		t.Errorf("the balance first: %+v", page.Stats)
	}
	for _, section := range page.Sections {
		if section.ID == "" || section.Tab == "" || (section.View == "" && section.UI == "") {
			t.Errorf("a section says where it goes and what fills it: %+v", section)
		}
		if section.View != "" && views[section.View] == nil {
			t.Errorf("section %s names a view that is not there", section.ID)
		}
	}
	meta := views["meta"].(map[string]any)
	if meta["build"] != "v1" || meta["mode"] != "auto" {
		t.Errorf("meta: %v", meta)
	}
	if strings.Contains(asJSON(views), "más datos") {
		t.Error("the foot carries only where the page comes from")
	}
}
