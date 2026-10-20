package server

import (
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/model"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/render"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/writes"
)

// operations are the routes that act on the league or arm something that will: bids, sales,
// offers, clauses, shields, raids, stars, the lineup and the necroporra vote.
var operations = map[string]bool{
	"/api/bid/prepare": true, "/api/bid/confirm": true, "/api/always": true,
	"/api/raid": true, "/api/raid/cancel": true, "/api/shield": true, "/api/shield/cancel": true,
	"/api/dismiss": true, "/api/lineup": true, "/api/favourite": true, "/favourite": true,
	"/api/necroporra/vote": true,
}

// whileLoading refuses every operation until the first build is over. Here and not only on the
// page: a tab left open from before a restart still has its buttons, and the world behind them
// is gone until the build comes back.
func (s *Server) whileLoading(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		acting := request.Method != http.MethodGet && request.Method != http.MethodHead
		if acting && operations[request.URL.Path] && s.state.Loading() {
			slog.Warn("write refused", "status", http.StatusServiceUnavailable,
				"path", request.URL.Path, "reason", "loading")
			writer.Header().Set("Retry-After", "5")
			s.json(writer, http.StatusServiceUnavailable, map[string]any{
				"error": writes.ErrLoading.Error(), "loading": true})
			return
		}
		next.ServeHTTP(writer, request)
	})
}

// previewPage is the panel as far as a cold start has read it. It has no buttons at all: what
// it shows is read-only until the world is complete, and then the page turns into the panel.
func previewPage(preview *model.Preview) string {
	var body strings.Builder
	body.WriteString(`<header class="pv-head"><h1>Cargando la liga…</h1>
<p class="lead">Primero tu equipo, luego el mercado y los rivales. Las operaciones (pujas, ventas, ofertas,
cláusulas, blindajes, clausulazos) se activan cuando termine de cargar.</p>` + loadBar + `</header>
<div id="preview" data-stage="` + fmt.Sprint(preview.Stage) + `">`)
	body.WriteString(previewMine(preview))
	if preview.Stage >= model.PreviewMarket {
		body.WriteString(previewMarket(preview.Market))
	} else {
		body.WriteString(placeholder("Mercado", "Cargando mercado…"))
	}
	if preview.Stage >= model.PreviewRivals {
		body.WriteString(previewRivals(preview.Rivals))
	} else {
		body.WriteString(placeholder("Rivales", "Cargando rivales…"))
	}
	rest := "Cargando ofertas, movimientos, fichas de futbolfantasy y consejos…"
	if preview.Stage >= model.PreviewComplete {
		rest = "Liga leída. Dibujando el panel completo…"
	}
	body.WriteString(placeholder("Ofertas, movimientos y consejos", rest))
	body.WriteString(`</div><script>followPreview();</script>`)
	return strings.Replace(authPage("Cargando", body.String()), `<main class="auth">`,
		`<main class="auth pv">`, 1)
}

func placeholder(title, message string) string {
	return `<section class="pv-sec"><h2>` + title + `</h2><p class="pv-wait">` + message + `</p></section>`
}

func previewMine(preview *model.Preview) string {
	mine := preview.Mine
	var out strings.Builder
	out.WriteString(`<section class="pv-sec"><h2>Mi equipo</h2><div class="pv-chips">`)
	chip := func(label, value string) {
		out.WriteString(`<span class="pv-chip"><i>` + label + `</i><b>` + value + `</b></span>`)
	}
	chip("Saldo", render.Money(preview.Cash))
	chip("Valor", render.Money(&mine.Value))
	chip("Puntos", fmt.Sprintf("%.0f", mine.Points))
	if mine.Position != nil {
		chip("Posición", fmt.Sprintf("%dº", *mine.Position))
	}
	out.WriteString(`</div>`)
	players := append([]model.PreviewPlayer(nil), mine.Players...)
	sort.SliceStable(players, func(one, two int) bool {
		if players[one].Starter != players[two].Starter {
			return players[one].Starter
		}
		return players[one].PositionID < players[two].PositionID
	})
	out.WriteString(`<table class="pv-table"><thead><tr><th>Jugador</th><th>Pos</th><th>Equipo</th>
<th class="num">Valor</th><th class="num">Puntos</th><th>Estado</th><th class="num">Cláusula</th></tr></thead><tbody>`)
	for _, player := range players {
		name := html.EscapeString(player.Name)
		if player.Starter {
			name += ` <span class="pv-tag">titular</span>`
		}
		out.WriteString(`<tr><td>` + name + `</td><td>` + previewPosition(player.PositionID) +
			`</td><td>` + html.EscapeString(player.Team) + `</td><td class="num">` +
			render.Money(&player.Value) + `</td><td class="num">` + fmt.Sprintf("%.0f", player.Points) +
			`</td><td>` + previewStatus(player.Status) + `</td><td class="num">` +
			render.Money(player.Clause) + `</td></tr>`)
	}
	out.WriteString(`</tbody></table></section>`)
	return out.String()
}

func previewMarket(listed []model.PreviewListing) string {
	if len(listed) == 0 {
		return placeholder("Mercado", "No hay nadie en el mercado.")
	}
	var out strings.Builder
	out.WriteString(`<section class="pv-sec"><h2>Mercado</h2><table class="pv-table"><thead><tr>
<th>Jugador</th><th>Pos</th><th>Equipo</th><th class="num">Valor</th><th class="num">Precio</th>
<th>Vende</th><th class="num">Pujas</th><th>Cierra</th></tr></thead><tbody>`)
	for _, row := range listed {
		seller := "LaLiga"
		if row.Listing.IsMine {
			seller = "tú"
		} else if row.Listing.Seller != nil {
			seller = *row.Listing.Seller
		}
		bids := render.Missing
		if row.Listing.Bids != nil {
			bids = fmt.Sprint(*row.Listing.Bids)
		}
		mine := ""
		if row.Listing.MyBid != nil {
			mine = ` <span class="pv-tag">tu puja ` + render.Money(row.Listing.MyBid) + `</span>`
		}
		out.WriteString(`<tr><td>` + html.EscapeString(row.Player.Name) + mine + `</td><td>` +
			previewPosition(row.Player.PositionID) + `</td><td>` + html.EscapeString(row.Player.Team) +
			`</td><td class="num">` + render.Money(&row.Player.Value) + `</td><td class="num">` +
			render.Money(&row.Listing.MinBid) + `</td><td>` + html.EscapeString(seller) +
			`</td><td class="num">` + bids + `</td><td>` + previewWhen(row.Listing.Expires) + `</td></tr>`)
	}
	out.WriteString(`</tbody></table></section>`)
	return out.String()
}

func previewRivals(rivals []model.PreviewTeam) string {
	var out strings.Builder
	out.WriteString(`<section class="pv-sec"><h2>Rivales</h2><table class="pv-table"><thead><tr>
<th class="num">#</th><th>Mánager</th><th class="num">Puntos</th><th class="num">Valor</th>
<th class="num">Jugadores</th><th>Los más caros</th></tr></thead><tbody>`)
	for _, team := range rivals {
		place := render.Missing
		if team.Position != nil {
			place = fmt.Sprint(*team.Position)
		}
		top := make([]string, 0, 3)
		for _, player := range team.Players[:min(3, len(team.Players))] {
			top = append(top, html.EscapeString(player.Name)+" "+render.Money(&player.Value))
		}
		out.WriteString(`<tr><td class="num">` + place + `</td><td>` + html.EscapeString(team.Name) +
			`</td><td class="num">` + fmt.Sprintf("%.0f", team.Points) + `</td><td class="num">` +
			render.Money(&team.Value) + `</td><td class="num">` + fmt.Sprint(len(team.Players)) +
			`</td><td>` + strings.Join(top, " · ") + `</td></tr>`)
	}
	out.WriteString(`</tbody></table></section>`)
	return out.String()
}

var previewPositions = map[int]string{1: "POR", 2: "DEF", 3: "MED", 4: "DEL"}

func previewPosition(id int) string {
	if name, ok := previewPositions[id]; ok {
		return name
	}
	return render.Missing
}

var previewStatuses = map[string]string{
	"injured": "lesionado", "doubtful": "duda", "sanctioned": "sancionado",
	"suspended": "sancionado", "out_of_league": "fuera de la liga",
}

func previewStatus(status string) string {
	if label, ok := previewStatuses[status]; ok {
		return `<span class="pv-bad">` + label + `</span>`
	}
	return ""
}

func previewWhen(stamp *string) string {
	if stamp == nil {
		return render.Missing
	}
	when, err := time.Parse(time.RFC3339, *stamp)
	if err != nil {
		return render.Missing
	}
	days := []string{"dom", "lun", "mar", "mié", "jue", "vie", "sáb"}
	when = when.In(time.Local)
	return fmt.Sprintf("%s %02d:%02d", days[when.Weekday()], when.Hour(), when.Minute())
}
