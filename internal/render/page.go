package render

// The page is a shell: the stylesheet, the icons, the views as JSON and the module that draws
// them (assets/ui). Everything on screen is drawn there, from the views built here.

// TabView is one tab of the bar.
type TabView struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// Tabs are the page's groups, in the bar's order. Only with a session: a tab that jumps nowhere
// is worse than no tab.
var Tabs = []TabView{{"decidir", "Decidir"}, {"comprar", "Comprar"}, {"vender", "Vender"},
	{"clausulas", "Cláusulas"}, {"plantilla", "Plantilla"}, {"partidos", "Partidos"},
	{"rivales", "Rivales"}, {"liga", "Liga"}, {"ranking", "Ranking"},
	{"necroporra", "Necroporra"}, {"comparador", "Comparador"}}

// SectionView is one section of the page: its id (what an address names), the tab it belongs
// to, and the view (or the browser's own component, UI) that fills it.
type SectionView struct {
	ID    string `json:"id"`
	Tab   string `json:"tab"`
	View  string `json:"view,omitempty"`
	UI    string `json:"ui,omitempty"`
	Class string `json:"class,omitempty"`
}

func shell(id, tab, view string) SectionView {
	return SectionView{ID: id, Tab: tab, View: view, Class: "mk"}
}

// Stat is one of the four cards under the tab bar: a label, one figure and a line under it.
type Stat struct {
	Icon  string `json:"icon,omitempty"`
	Label string `json:"label"`
	Value string `json:"value"`
	Small string `json:"small,omitempty"`
	Note  string `json:"note,omitempty"`
	// ValueID marks the figure the live balance rewrites.
	ValueID string `json:"value_id,omitempty"`
	// Deadline turns the figure into a live countdown, outlined in red in its last six hours.
	Deadline string `json:"deadline,omitempty"`
	Tab      string `json:"tab,omitempty"`
}

// Foot is where the page comes from and what the server may do.
type Foot struct {
	Generated string `json:"generated"`
	League    string `json:"league,omitempty"`
	Week      int    `json:"week"`
	Mode      string `json:"mode,omitempty"`
	// Weight is how much of the current season the xPts already lean on.
	Weight string `json:"weight"`
}

type PageData struct {
	Tabs     []TabView     `json:"tabs,omitempty"`
	Stats    []Stat        `json:"stats"`
	Sections []SectionView `json:"sections"`
	Foot     Foot          `json:"foot"`
}

// Page is the document around the app. Without JavaScript it says so rather than showing an
// empty frame.
func Page(css, crestCSS, views string) string {
	// Without it a phone lays the page out at a made-up 980px and shrinks it: 13px text
	// arrives at five real pixels, and no rule inside the document can undo that.
	return `<!doctype html><meta charset="utf-8">` +
		`<meta name="viewport" content="width=device-width, initial-scale=1">` +
		`<title>Fantasy</title>` + Favicon + `<style>` + css + crestCSS + `</style>` +
		IconSprite + `<div class="wrap" id="app"></div>` +
		`<noscript><p class="noscript">Este panel se dibuja con JavaScript: actívalo para verlo.` +
		`</p></noscript>` +
		`<script type="application/json" id="views-data">` + views + `</script>` +
		`<script type="module" src="/assets/ui/main.js?v=` + Esc(Build) + `"></script>`
}
