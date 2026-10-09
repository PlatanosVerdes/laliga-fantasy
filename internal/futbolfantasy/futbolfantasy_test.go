package futbolfantasy

import (
	"os"
	"testing"
)

// The ceiling's own page links the player, and that link is the one to follow: a slug made from
// a short name ("Roberto") points at somebody else or at nobody.
func TestDetailCarriesThePageItBelongsTo(t *testing.T) {
	page := `<div class="analytics-nombre"><a href="https://www.futbolfantasy.com/jugadores/roberto-fernandez" ` +
		`class="jugador">Roberto</a></div><script>parsePujaIdeal( 56800000 )</script>`
	detail := ParseDetail(page)
	url, _ := detail["ff_url"].(*string)
	if url == nil || *url != "https://www.futbolfantasy.com/jugadores/roberto-fernandez" {
		t.Fatalf("ff_url = %v", detail["ff_url"])
	}
	if detail["ideal_bid"] != 56800000 {
		t.Errorf("ideal_bid = %v", detail["ideal_bid"])
	}
	if none := ParseDetail(`<script>parsePujaIdeal(1)</script>`)["ff_url"].(*string); none != nil {
		t.Errorf("no link on the page, no link: %v", *none)
	}
}

func hierarchyFixture(t *testing.T) []map[string]any {
	t.Helper()
	page, err := os.ReadFile("testdata/jerarquias_sevilla.html")
	if err != nil {
		t.Fatal(err)
	}
	return ParseHierarchy(string(page), "sevilla")
}

func TestHierarchySortsEveryPlayerIntoHisCategory(t *testing.T) {
	rows := hierarchyFixture(t)
	if len(rows) != 25 {
		t.Fatalf("25 players on the page, got %d", len(rows))
	}
	byName := map[string]map[string]any{}
	counts := map[string]int{}
	for _, row := range rows {
		byName[row["ff_name"].(string)] = row
		counts[row["key"].(string)]++
	}
	for _, key := range []string{"clave", "importante", "rotacion", "revulsivo", "reserva", "descarte"} {
		if counts[key] == 0 {
			t.Errorf("no player in %s: %v", key, counts)
		}
	}
	keeper := byName["Odysseas Vlachodimos"]
	if keeper["key"] != "clave" || keeper["label"] != "Clave" || keeper["ff_id"] != "6235" ||
		keeper["url"] != "https://www.futbolfantasy.com/jugadores/odysseas-vlachodimos" {
		t.Errorf("Vlachodimos: %v", keeper)
	}
	// The whole note, not the cut one with "Leer más".
	if keeper["note"] != "Titular en las siete jornadas; Fran González y Rafa Romero no han "+
		"disputado minutos ni amenazan su puesto." {
		t.Errorf("note: %q", keeper["note"])
	}
	if byName["Gabriel Suazo"]["change"] != "up" || byName["Robbie Ure"]["change"] != "down" ||
		byName["Kike Salas"]["change"] != "" {
		t.Error("the arrows: up, down, none")
	}
	if byName["Rubén Vargas"]["status"] != "disponible" || byName["Marcão"]["status"] != "lesionado" {
		t.Error("the status badges")
	}
	if keeper["team_url"] != "https://www.futbolfantasy.com/laliga/equipos/sevilla/jerarquias" {
		t.Errorf("team page: %v", keeper["team_url"])
	}
}

func TestTeamSlugsComeFromTheMenu(t *testing.T) {
	page, _ := os.ReadFile("testdata/jerarquias_sevilla.html")
	slugs := TeamSlugs(string(page))
	if len(slugs) != 20 || slugs[0] != "alaves" {
		t.Errorf("20 clubs from the menu: %v", slugs)
	}
}
