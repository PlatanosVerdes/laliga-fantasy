package render

import (
	"strings"
	"testing"
)

// A rival section that renders to nothing looks exactly like a rival with no players, which is
// why the missing table spec went out unnoticed. These assert the shape, not the wording.
func universeWithRivals() Document {
	players := []any{
		map[string]any{"id": "1", "name": "Mi medio", "position": "MED", "position_id": 3.0,
			"is_mine": true, "owner_team_id": "100", "xpts": 3.0, "value": 10_000_000.0},
		map[string]any{"id": "2", "name": "Su medio bueno", "position": "MED", "position_id": 3.0,
			"owner_team_id": "200", "owner": "cristian", "xpts": 4.5, "value": 20_000_000.0,
			"clause": 30_000_000.0, "clause_locked": false},
		map[string]any{"id": "3", "name": "Su medio malo", "position": "MED", "position_id": 3.0,
			"owner_team_id": "200", "owner": "cristian", "xpts": 1.0, "value": 5_000_000.0,
			"clause": 8_000_000.0, "clause_locked": true,
			"clause_locked_until": "2026-08-25T00:00:00Z"},
		map[string]any{"id": "4", "name": "Su portero", "position": "POR", "position_id": 1.0,
			"owner_team_id": "300", "owner": "tete", "xpts": 2.0, "value": 7_000_000.0,
			"market": map[string]any{"min_bid": 6_500_000.0}},
		map[string]any{"id": "5", "name": "Libre", "position": "DEL", "position_id": 4.0,
			"xpts": 9.0, "value": 1_000_000.0},
	}
	return Document{Universe: map[string]any{
		"my_team_id": "100",
		"players":    players,
		"league_teams": map[string]any{
			"100": map[string]any{"team_id": "100", "manager": "yo", "position": 1.0,
				"points": 20.0, "estimated_cash": 5_000_000.0},
			"200": map[string]any{"team_id": "200", "manager": "cristian", "position": 3.0,
				"points": 15.0, "estimated_cash": 2_000_000.0},
			"300": map[string]any{"team_id": "300", "manager": "tete", "position": 2.0,
				"points": 18.0, "estimated_cash": 40_000_000.0},
		},
	}}
}

func TestRivalSectionsOnePerRival(t *testing.T) {
	document := universeWithRivals()
	sections := document.rivalSections(rows(document.Universe["players"]))
	// El selector primero, y una seccion por rival detras.
	if len(sections) != 3 {
		t.Fatalf("selector y dos rivales, salieron %d secciones", len(sections))
	}
	if !strings.Contains(sections[0], `id="rivalpick"`) ||
		!strings.Contains(sections[0], `<select id="rival-pick">`) {
		t.Fatalf("la primera tiene que ser el desplegable: %.140s", sections[0])
	}
	// Ordered by league position, so second in the table comes first.
	if !strings.Contains(sections[1], `id="rival-300"`) {
		t.Errorf("el segundo de la liga deberia ir primero: %.120s", sections[1])
	}
	// Y el desplegable los ofrece en el mismo orden.
	if strings.Index(sections[0], "rival-300") > strings.Index(sections[0], "rival-200") {
		t.Error("las opciones tienen que ir en orden de clasificacion")
	}
	for _, section := range sections[1:] {
		if !strings.Contains(section, `data-tab="rivales"`) {
			t.Error("una seccion que no dice su pestaña queda invisible")
		}
		if !strings.Contains(section, "<table") {
			t.Errorf("seccion sin tabla, que es todo su contenido: %.160s", section)
		}
	}
}

func TestRivalSectionsExcludeMineAndFreeAgents(t *testing.T) {
	document := universeWithRivals()
	joined := strings.Join(document.rivalSections(rows(document.Universe["players"])), "")
	// Por id de fila, no por nombre: el mio aparece de todas formas como referencia en la
	// columna "Frente a lo tuyo", y ahi es correcto que salga.
	for _, absent := range []string{`data-cmp="1"`, `data-cmp="5"`, `id="rival-100"`} {
		if strings.Contains(joined, absent) {
			t.Errorf("%s no es de un rival y no deberia tener fila", absent)
		}
	}
	for _, present := range []string{`data-cmp="2"`, `data-cmp="3"`, `data-cmp="4"`} {
		if !strings.Contains(joined, present) {
			t.Errorf("falta la fila %s", present)
		}
	}
}

func TestRivalSectionsSayWhoBeatsMine(t *testing.T) {
	document := universeWithRivals()
	sections := document.rivalSections(rows(document.Universe["players"]))
	var cristian string
	for _, section := range sections {
		if strings.Contains(section, `id="rival-200"`) {
			cristian = section
		}
	}
	if cristian == "" {
		t.Fatal("sin seccion de cristian")
	}
	// 4.5 contra mi 3.0 es un jugador mejor, y 1.0 no: uno solo.
	if !strings.Contains(cristian, "<strong>Uno</strong> de los suyos mejora") {
		t.Errorf("la nota tiene que contar los que te mejoran: %s", note(cristian))
	}
	if !strings.Contains(cristian, "+1.50") {
		t.Error("falta la diferencia de xPts contra el tuyo de esa posicion")
	}
	// Su medio malo tiene la clausula bloqueada y el bueno no.
	if !strings.Contains(cristian, "pagable ya") {
		t.Error("una clausula libre tiene que decir que es pagable")
	}
	if !strings.Contains(cristian, "1 con la clausula pagable ya") {
		t.Errorf("la nota tiene que contar las clausulas pagables: %s", note(cristian))
	}
}

func TestRivalSectionsWithoutLeagueTeams(t *testing.T) {
	document := Document{Universe: map[string]any{"players": []any{}}}
	if sections := document.rivalSections(nil); sections != nil {
		t.Errorf("sin liga no hay rivales: %v", sections)
	}
}

// The forecast section: three cards off the podium flag, the whole league underneath, and the
// tab it belongs to said out loud. A section that does not name its tab is a section nobody ever
// sees, which is the failure this file exists for.
func TestOutlookSectionCardsAndTable(t *testing.T) {
	document := Document{Advice: map[string]any{"outlook": []any{
		map[string]any{"manager": "cristian", "team_id": "200", "week": 3.0, "rank": 1.0,
			"worst": true, "xpts": 18.5, "ceiling": 24.0, "lost": 5.5, "position": 4.0,
			"outs": 2.0, "holes": 1.0, "doubts": 0.0, "air": 0.0, "away": 6.0,
			"fixture_pct": -3.2, "hard": []any{"BAR"},
			"reasons": []any{"2 bajas (Uno, Dos) · 5.5 xPts fuera"}},
		map[string]any{"manager": "tete", "team_id": "300", "week": 3.0, "rank": 2.0,
			"worst": true, "xpts": 22.0, "ceiling": 22.0, "lost": 0.0, "position": 2.0,
			"holes": 0.0, "reasons": []any{}},
		map[string]any{"manager": "yo", "team_id": "100", "week": 3.0, "rank": 3.0,
			"worst": true, "xpts": 25.0, "ceiling": 25.0, "lost": 0.0, "is_me": true,
			"holes": 0.0, "reasons": []any{}},
		map[string]any{"manager": "otro", "team_id": "400", "week": 3.0, "rank": 4.0,
			"worst": false, "xpts": 30.0, "ceiling": 30.0, "lost": 0.0, "holes": 0.0,
			"reasons": []any{}},
	}}}

	section := document.outlookSection()
	if !strings.Contains(section, `id="pinta"`) ||
		!strings.Contains(section, `data-tab="rivales"`) {
		t.Fatalf("la seccion tiene que decir su id y su pestaña: %.160s", section)
	}
	if !strings.Contains(section, "Quien pinta peor la J3") {
		t.Error("el titulo tiene que nombrar la jornada")
	}
	// Three cards, not four: who goes on the podium is the forecast's own judgement.
	if got := strings.Count(section, `class="pinta-card"`); got != 3 {
		t.Errorf("%d tarjetas, want 3", got)
	}
	if !strings.Contains(section, "el que peor pinta") {
		t.Error("falta la etiqueta del primero")
	}
	if !strings.Contains(section, "2 bajas (Uno, Dos) · 5.5 xPts fuera") {
		t.Error("los motivos de la tarjeta no salieron")
	}
	// A card with nothing broken still says something rather than sitting empty.
	if !strings.Contains(section, "nada roto") {
		t.Error("una tarjeta sin motivos tiene que decirlo")
	}
	if !strings.Contains(section, `−5.5`) {
		t.Error("falta lo que se deja respecto a su once sano")
	}
	if !strings.Contains(section, `class="flag-mine"`) {
		t.Error("si el tuyo esta entre los peores hay que decirlo")
	}
	// The four rows of the league are the table, podium or not.
	if got := strings.Count(section, "<tr"); got != 5 {
		t.Errorf("%d filas (cabecera incluida), want 5", got)
	}
	if !strings.Contains(section, "1 en blanco") {
		t.Error("un hueco en el once tiene que verse en la tabla")
	}
	if !strings.Contains(section, "once completo") {
		t.Error("cero huecos no es un numero que falte, es una buena noticia")
	}
	if !strings.Contains(section, "-3.20%") {
		t.Error("falta lo que hace el calendario")
	}
}

func TestOutlookSectionWithoutAForecast(t *testing.T) {
	if got := (Document{}).outlookSection(); got != "" {
		t.Errorf("sin datos no hay seccion, salio %.80s", got)
	}
}

func note(section string) string {
	start := strings.Index(section, `<p class="note">`)
	if start < 0 {
		return "(sin nota)"
	}
	end := strings.Index(section[start:], "</p>")
	return section[start : start+end]
}

// Los clausulazos que no se pudieron hacer van en su propia subseccion: mezclados con los que
// siguen en pie, la seccion se leia como si todos estuvieran a punto de pagarse.
func TestRaidsSectionSeparatesTheOnesThatCouldNotRun(t *testing.T) {
	document := Document{Raids: []map[string]any{
		{"player_id": "1", "name": "El que espera", "owner": "cristian",
			"clause": 20_000_000.0, "max_pay": 25_000_000.0, "action": "esperando",
			"why": "clausula bloqueada, se abre en 12h"},
		{"player_id": "2", "name": "El que subio", "owner": "tete",
			"clause": 30_129_276.0, "max_pay": 17_155_131.0, "action": "cancelada",
			"why": "la clausula subio a 30.129.276, tu limite es 17.155.131"},
	}}

	section := document.raidsSection()
	cut := strings.Index(section, "No se pudieron hacer")
	if cut < 0 {
		t.Fatalf("falta la subseccion de los que no se pudieron hacer: %.200s", section)
	}
	if !strings.Contains(section[:cut], "El que espera") {
		t.Error("el que sigue en pie va en la tabla de arriba")
	}
	if !strings.Contains(section[cut:], "El que subio") ||
		strings.Contains(section[:cut], "El que subio") {
		t.Error("el cancelado va debajo, no arriba")
	}
	if !strings.Contains(section, "1 en pie · 1 sin hacer") {
		t.Errorf("el contador tiene que separar los dos: %.200s", section)
	}
}

// Y con todos caidos la tabla de arriba lo dice en vez de quedarse en "Sin datos".
func TestRaidsSectionSaysNoneStandingWhenAllFell(t *testing.T) {
	document := Document{Raids: []map[string]any{
		{"player_id": "2", "name": "El blindado", "owner": "tete", "clause": 30_000_000.0,
			"max_pay": 40_000_000.0, "action": "bloqueada", "why": "tete lo ha blindado"},
	}}

	section := document.raidsSection()
	if !strings.Contains(section, "Ninguno en pie ahora mismo") {
		t.Errorf("sin clausulazos en pie hay que decirlo: %.200s", section)
	}
	if !strings.Contains(section, "0 en pie · 1 sin hacer") {
		t.Errorf("el contador no cuadra: %.200s", section)
	}
}
