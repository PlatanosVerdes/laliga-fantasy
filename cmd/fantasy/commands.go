package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/advice"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/api"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/cli"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/futbolfantasy"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/matching"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/model"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/policies"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/rules"
)

// world is the pair every reporting command needs: the universe and the advice over it. Built
// once here so no command has to remember the order of the steps.
type world struct {
	Universe *model.Universe
	Generic  map[string]any
	Advice   map[string]any
	Players  []map[string]any
	Cash     float64
	League   string
	TeamID   string
}

func loadWorld(limit int, enrich bool) (*world, error) {
	leagueID, teamID, err := savedLeague()
	if err != nil {
		return nil, err
	}
	client := api.New()
	universe, err := model.BuildLive(client, leagueID, teamID, loadState(), 2*time.Hour)
	if err != nil {
		return nil, err
	}

	cash := 0.0
	if cashOverride != nil {
		cash = *cashOverride
	} else if money, err := client.Money(teamID, time.Minute); err == nil {
		cash = money.TeamMoney
	}

	blob, err := json.Marshal(universe)
	if err != nil {
		return nil, err
	}
	var generic map[string]any
	if err := json.Unmarshal(blob, &generic); err != nil {
		return nil, err
	}
	buckets := advice.Recommend(generic, cash, 0, limit)
	if enrich {
		advice.EnrichBuckets(buckets, limit, futbolfantasy.DetailTTL)
	}

	league := ""
	if settings, err := os.ReadFile(config.SettingsFile); err == nil {
		var parsed struct {
			LeagueName string `json:"league_name"`
		}
		if json.Unmarshal(settings, &parsed) == nil {
			league = parsed.LeagueName
		}
	}
	return &world{Universe: universe, Generic: generic, Advice: buckets,
		Players: rowsFrom(generic["players"]), Cash: cash, League: league,
		TeamID: teamID}, nil
}

func bucket(buckets map[string]any, name string) []map[string]any {
	return rowsFrom(buckets[name])
}

// --- squad ------------------------------------------------------------------------------

func cmdSquad(args []string) error {
	flags := flag.NewFlagSet("squad", flag.ContinueOnError)
	if err := flags.Parse(args); err != nil {
		return err
	}
	state, err := loadWorld(15, true)
	if err != nil {
		return err
	}
	squad := bucket(state.Advice, "squad")
	if len(squad) == 0 {
		fmt.Println(cli.Red("Necesito sesion y liga para ver tu plantilla."))
		return nil
	}

	cli.Heading("Mi plantilla")
	headers, rows, right := cli.PlayerRows(squad, "", nil)
	fmt.Println(cli.Table(headers, rows, right))

	total, xpts := 0.0, make([]float64, 0, len(squad))
	for _, player := range squad {
		total += number(player["value"])
		xpts = append(xpts, number(player["xpts"]))
	}
	sort.Sort(sort.Reverse(sort.Float64Slice(xpts)))
	best := 0.0
	for index, value := range xpts {
		if index >= 11 {
			break
		}
		best += value
	}
	fmt.Println()
	fmt.Printf("  Valor total : %s\n", cli.Money(total))
	fmt.Printf("  xPts mejores 11: %.1f\n", best)
	return nil
}

// --- market -----------------------------------------------------------------------------

func cmdMarket(args []string) error {
	flags := flag.NewFlagSet("market", flag.ContinueOnError)
	position := flags.String("position", "", "filtrar por posicion (POR, DEF, MED, DEL)")
	maxPrice := flags.Float64("max-price", 0, "precio maximo")
	free := flags.Bool("free", false, "solo los que no tienen dueño")
	limit := flags.Int("limit", 25, "filas")
	if err := flags.Parse(args); err != nil {
		return err
	}
	state, err := loadWorld(15, false)
	if err != nil {
		return err
	}

	var players []map[string]any
	for _, player := range state.Players {
		if !truthyValue(player["available"]) {
			continue
		}
		if *position != "" && !strings.EqualFold(text(player["position"]), *position) {
			continue
		}
		if *maxPrice != 0 && number(player["value"]) > *maxPrice {
			continue
		}
		if *free && text(player["owner"]) != "" {
			continue
		}
		players = append(players, player)
	}
	sort.SliceStable(players, func(i, j int) bool {
		return number(players[i]["score"]) > number(players[j]["score"])
	})
	if len(players) > *limit {
		players = players[:*limit]
	}

	cli.Heading("Ranking por score")
	owner := &cli.Column{Header: "Dueño", Read: func(row map[string]any) string {
		if name := text(row["owner"]); name != "" {
			return name
		}
		return "libre"
	}}
	headers, rows, right := cli.PlayerRows(players, "", owner)
	fmt.Println(cli.Table(headers, rows, right))
	return nil
}

// --- advise -----------------------------------------------------------------------------

func cmdAdvise(args []string) error {
	flags := flag.NewFlagSet("advise", flag.ContinueOnError)
	limit := flags.Int("limit", 10, "filas por bloque")
	if err := flags.Parse(args); err != nil {
		return err
	}
	state, err := loadWorld(*limit, true)
	if err != nil {
		return err
	}

	fmt.Printf("\n  Saldo: %s · poder de compra: %s\n", cli.Money(state.Cash),
		cli.Money(state.Advice["spending_power"]))

	blocks := []struct {
		Title string
		Name  string
		Cost  string
	}{
		{"Pujar ahora · mercado libre", "bids_now", "entry_cost"},
		{"En venta por rivales", "asks", "entry_cost"},
		{"Cláusulas pagables", "raids", "entry_cost"},
		{"Candidatos a vender", "sells", ""},
	}
	for _, block := range blocks {
		rows := bucket(state.Advice, block.Name)
		cli.Heading(block.Title)
		if block.Name == "sells" {
			reasons := &cli.Column{Header: "Motivos", Read: func(row map[string]any) string {
				items, _ := row["reasons"].([]any)
				out := make([]string, 0, len(items))
				for _, item := range items {
					out = append(out, fmt.Sprint(item))
				}
				if len(out) == 0 {
					return "-"
				}
				return strings.Join(out, "; ")
			}}
			headers, table, right := cli.PlayerRows(rows, "", reasons)
			fmt.Println(cli.Table(headers, table, right))
			continue
		}
		headers, table, right := cli.PlayerRows(rows, block.Cost, nil)
		fmt.Println(cli.Table(headers, table, right))
	}

	offers := bucket(state.Advice, "offers")
	if len(offers) > 0 {
		cli.Heading("Ofertas que has recibido")
		rows := make([][]string, 0, len(offers))
		for _, offer := range offers {
			verdict := "-"
			if truthyValue(offer["worth_taking"]) {
				verdict = cli.Green("merece la pena")
			}
			rows = append(rows, []string{
				text(offer["name"]), cli.Money(offer["value"]), cli.Money(offer["ask"]),
				cli.Money(offer["offer_amount"]),
				fmt.Sprintf("%.2fx", number(offer["vs_value"])), verdict,
			})
		}
		fmt.Println(cli.Table([]string{"jugador", "valor", "pides", "te ofrecen", "sobre valor",
			""}, rows, map[int]bool{1: true, 2: true, 3: true, 4: true}))
	}
	return nil
}

// --- standings and rivals ---------------------------------------------------------------

func cmdStandings(args []string) error {
	state, err := loadWorld(15, false)
	if err != nil {
		return err
	}
	rivals := bucket(state.Advice, "rivals")
	cli.Heading("Poder de compra de la liga")
	rows := make([][]string, 0, len(rivals))
	for _, team := range rivals {
		name := text(team["manager"])
		if name == "" {
			name = text(team["name"])
		}
		if truthyValue(team["is_me"]) {
			name = cli.Cyan(name + " (tú)")
		}
		rows = append(rows, []string{
			fmt.Sprintf("%d", int(number(team["cash_position"]))), name,
			fmt.Sprintf("%d", int(number(team["points"]))),
			cli.Money(team["squad_value"]), cli.Money(team["net_flow"]),
			cli.Money(team["estimated_cash"]), text(team["power"]),
		})
	}
	fmt.Println(cli.Table([]string{"#", "manager", "puntos", "plantilla", "neto", "caja",
		"poder"}, rows, map[int]bool{0: true, 2: true, 3: true, 4: true, 5: true}))

	model := mapFrom(state.Advice["cash_model"])
	fmt.Println()
	if truthyValue(model["anchored"]) {
		fmt.Printf("  %s\n", cli.Dim(fmt.Sprintf(
			"Caja reconstruida del historial sobre una base medida en tu propio saldo (%s). "+
				"El error que queda es la diferencia de recompensas diarias, como maximo %s.",
			cli.Money(model["base"]), cli.Money(model["uncertainty"]))))
	} else {
		fmt.Printf("  %s\n", cli.Dim("Sin saldo propio que leer: base asumida para todos."))
	}
	return nil
}

// --- activity ---------------------------------------------------------------------------

func cmdActivity(args []string) error {
	flags := flag.NewFlagSet("activity", flag.ContinueOnError)
	limit := flags.Int("limit", 25, "eventos")
	if err := flags.Parse(args); err != nil {
		return err
	}
	state, err := loadWorld(15, false)
	if err != nil {
		return err
	}

	cli.Heading("Movimientos de la liga")
	rows := [][]string{}
	for _, event := range state.Universe.Activity {
		if strings.Contains(event.Kind, "alinea") {
			continue
		}
		if len(rows) >= *limit {
			break
		}
		player, buyer, seller := "-", "-", "-"
		if event.Player != nil {
			player = *event.Player
		}
		if event.Buyer != nil {
			buyer = *event.Buyer
		}
		if event.Seller != nil {
			seller = *event.Seller
		}
		amount := "-"
		if event.Amount != nil {
			amount = cli.Money(*event.Amount)
		}
		// The value on the trade date is what says whether it was a steal.
		premium := "-"
		if event.Premium != nil {
			premium = fmt.Sprintf("%.2fx", *event.Premium)
		}
		// The T is a machine's separator: a person reads a space.
		rows = append(rows, []string{strings.Replace(event.Date, "T", " ", 1), event.Kind,
			player, seller, buyer, amount, premium})
	}
	fmt.Println(cli.Table([]string{"fecha", "tipo", "jugador", "vende", "compra", "importe",
		"sobre valor"}, rows, map[int]bool{5: true, 6: true}))
	return nil
}

// --- player -----------------------------------------------------------------------------

func cmdPlayer(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("uso: player <nombre>")
	}
	query := matching.Normalize(strings.Join(args, " "))
	state, err := loadWorld(15, true)
	if err != nil {
		return err
	}

	var found map[string]any
	for _, player := range state.Players {
		if strings.Contains(matching.Normalize(text(player["name"])), query) ||
			strings.Contains(matching.Normalize(text(player["full_name"])), query) {
			found = player
			break
		}
	}
	if found == nil {
		fmt.Println(cli.Red(fmt.Sprintf("no encuentro a nadie que se llame como '%s'",
			strings.Join(args, " "))))
		return nil
	}

	cli.Heading(fmt.Sprintf("%s · %s · %s", text(found["name"]), text(found["position"]),
		text(found["team"])))
	owner := text(found["owner"])
	if owner == "" {
		owner = "libre"
	}
	lines := [][2]string{
		{"Valor de mercado", cli.Money(found["value"])},
		{"Estado", text(found["status"])},
		{"Puntos 25/26", fmt.Sprintf("%.0f", number(found["last_season_points"]))},
		{"Puntos temporada", fmt.Sprintf("%.0f (media %.2f)", number(found["season_points"]),
			number(found["season_avg"]))},
		{"xPts / jornada", fmt.Sprintf("%.2f", number(found["xpts"]))},
		{"Puntos por millon", fmt.Sprintf("%.3f", number(found["points_value"]))},
		{"Score / ranking", fmt.Sprintf("%+.2f · #%d global · #%d en %s",
			number(found["score"]), int(number(found["rank"])),
			int(number(found["position_rank"])), text(found["position"]))},
		{"Dueño", owner},
	}
	if probability, ok := asFloatValue(found["start_probability"]); ok {
		lines = append(lines, [2]string{"Probabilidad titular", fmt.Sprintf("%.0f%%", probability)})
	}
	if clause := number(found["clause"]); clause != 0 {
		state := "abierta"
		if truthyValue(found["clause_locked"]) {
			state = "bloqueada"
		}
		lines = append(lines, [2]string{"Clausula",
			fmt.Sprintf("%s (%s)", cli.Money(clause), state)})
	}
	if absence := mapFrom(found["absence"]); absence != nil {
		lines = append(lines, [2]string{"Baja",
			fmt.Sprintf("%s · %s", text(absence["kind"]), text(absence["reason"]))})
	}
	for _, line := range lines {
		fmt.Printf("  %-22s %s\n", line[0], line[1])
	}
	return nil
}

// --- favourites, always, raid -----------------------------------------------------------

func cmdFav(args []string) error {
	favourites, err := loadFavourites()
	if err != nil {
		return err
	}
	if len(args) == 0 || args[0] == "list" {
		cli.Heading("Favoritos")
		ids := make([]string, 0, len(favourites))
		for id := range favourites {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		rows := make([][]string, 0, len(ids))
		for _, id := range ids {
			rows = append(rows, []string{id, text(favourites[id]["name"]),
				text(favourites[id]["note"])})
		}
		fmt.Println(cli.Table([]string{"id", "jugador", "nota"}, rows, nil))
		return nil
	}

	action, name := args[0], strings.Join(args[1:], " ")
	if name == "" {
		return fmt.Errorf("uso: fav <add|rm> <nombre>")
	}
	state, err := loadWorld(15, false)
	if err != nil {
		return err
	}
	query := matching.Normalize(name)
	var found map[string]any
	for _, player := range state.Players {
		if strings.Contains(matching.Normalize(text(player["name"])), query) {
			found = player
			break
		}
	}
	if found == nil {
		fmt.Println(cli.Red(fmt.Sprintf("no encuentro a '%s'", name)))
		return nil
	}

	id := text(found["id"])
	switch action {
	case "add":
		favourites[id] = map[string]any{"id": id, "name": found["name"], "note": nil}
		fmt.Println(cli.Green(fmt.Sprintf("%s marcado como favorito", text(found["name"]))))
	case "rm":
		delete(favourites, id)
		fmt.Printf("Quitado: %s\n", text(found["name"]))
	default:
		return fmt.Errorf("no se que es '%s': usa add o rm", action)
	}
	return saveFavourites(favourites)
}

func cmdAlways(args []string) error {
	flags := flag.NewFlagSet("always", flag.ContinueOnError)
	minPrice := flags.Float64("min", 0, "precio minimo al listarlo")
	accept := flags.Float64("accept", 0, "aceptar ofertas desde este importe")
	auto := flags.Bool("auto", false, "vender cuando la oferta sea buena")
	if err := flags.Parse(args); err != nil {
		return err
	}
	rest := flags.Args()

	armed, err := policies.Load()
	if err != nil {
		return err
	}

	if len(rest) == 0 || rest[0] == "list" {
		cli.Heading("Siempre en mercado")
		rows := make([][]string, 0, len(armed))
		for _, id := range policies.SortedIDs(armed) {
			policy := armed[id]
			sells := "no vendo solo"
			if policy.AcceptAbove != nil && *policy.AcceptAbove != 0 {
				sells = cli.Money(*policy.AcceptAbove)
			} else if policy.AutoSell {
				sells = "cuando la oferta sea buena"
			}
			floor := "-"
			if policy.MinPrice != nil && *policy.MinPrice != 0 {
				floor = cli.Money(*policy.MinPrice)
			}
			rows = append(rows, []string{fallbackText(policy.Name, id), floor, sells})
		}
		fmt.Println(cli.Table([]string{"jugador", "precio minimo", "vendo solo si"}, rows,
			map[int]bool{1: true, 2: true}))

		state, err := loadWorld(15, true)
		if err != nil {
			return err
		}
		plan := policies.Plan(state.Players, armed)
		if len(plan) > 0 {
			cli.Heading("Que haria ahora mismo")
			planRows := make([][]string, 0, len(plan))
			for _, action := range plan {
				amount := "-"
				if value, ok := asFloatValue(action["amount"]); ok {
					amount = cli.Money(value)
				}
				planRows = append(planRows, []string{text(action["name"]),
					text(action["action"]), amount, text(action["why"])})
			}
			fmt.Println(cli.Table([]string{"jugador", "accion", "importe", "motivo"}, planRows,
				map[int]bool{2: true}))
			fmt.Println(cli.Dim("  Se ejecuta en el proximo refresco del servidor; " +
				"con --read-only no."))
		}
		return nil
	}

	action, name := rest[0], strings.Join(rest[1:], " ")
	if action == "rm" && name != "" {
		state, err := loadWorld(15, false)
		if err != nil {
			return err
		}
		id, label := findMine(state.Players, name)
		if id == "" {
			fmt.Println(cli.Red(fmt.Sprintf("'%s' no esta en tu plantilla.", name)))
			return nil
		}
		if _, existed := armed[id]; existed {
			delete(armed, id)
			fmt.Printf("Quitado: %s\n", label)
		} else {
			fmt.Printf("No estaba: %s\n", label)
		}
		return policies.Save(armed)
	}

	if action != "add" {
		name = strings.TrimSpace(strings.Join(rest, " "))
	}
	if name == "" {
		return fmt.Errorf("uso: always [add|rm] <nombre> [--min N] [--accept N] [--auto]")
	}
	state, err := loadWorld(15, true)
	if err != nil {
		return err
	}
	id, label := findMine(state.Players, name)
	if id == "" {
		fmt.Println(cli.Red(fmt.Sprintf("'%s' no esta en tu plantilla.", name)))
		return nil
	}

	policy := armed[id]
	policy.ID, policy.Name, policy.AlwaysList = id, label, true
	if *minPrice != 0 {
		policy.MinPrice = minPrice
	} else if policy.MinPrice == nil {
		var player map[string]any
		for _, row := range state.Players {
			if text(row["id"]) == id {
				player = row
			}
		}
		value := number(player["value"])
		policy.MinPrice = &value
	}
	if *accept != 0 {
		policy.AcceptAbove = accept
	}
	if *auto {
		policy.AutoSell = true
	}
	armed[id] = policy

	tail := "no lo vendo solo; con --auto o --accept <importe> lo autorizas."
	if policy.AcceptAbove != nil && *policy.AcceptAbove != 0 {
		tail = fmt.Sprintf("acepto ofertas desde %s.", cli.Money(*policy.AcceptAbove))
	} else if policy.AutoSell {
		tail = "vendo en cuanto la oferta sea buena."
	}
	fmt.Println(cli.Green(fmt.Sprintf("%s: siempre en mercado a %s, %s", label,
		cli.Money(*policy.MinPrice), tail)))
	return policies.Save(armed)
}

func cmdRaid(args []string) error {
	flags := flag.NewFlagSet("raid", flag.ContinueOnError)
	maxPay := flags.Float64("max", 0, "pago maximo")
	if err := flags.Parse(args); err != nil {
		return err
	}
	rest := flags.Args()
	armed, err := policies.Load()
	if err != nil {
		return err
	}

	if len(rest) == 0 || rest[0] == "list" {
		state, err := loadWorld(15, true)
		if err != nil {
			return err
		}
		cli.Heading("Clausulazos programados")
		plan := policies.RaidPlan(state.Players, armed, state.Cash)
		rows := make([][]string, 0, len(plan))
		for _, action := range plan {
			rows = append(rows, []string{text(action["name"]), text(action["owner"]),
				cli.Money(action["clause"]), cli.Money(action["max_pay"]),
				text(action["action"]), text(action["why"])})
		}
		fmt.Println(cli.Table([]string{"jugador", "dueño", "clausula", "mi limite", "estado",
			"motivo"}, rows, map[int]bool{2: true, 3: true}))
		return nil
	}

	action, name := rest[0], strings.Join(rest[1:], " ")
	if action != "add" && action != "rm" {
		name, action = strings.Join(rest, " "), "add"
	}
	state, err := loadWorld(15, true)
	if err != nil {
		return err
	}
	query := matching.Normalize(name)
	var found map[string]any
	for _, player := range state.Players {
		if truthyValue(player["is_mine"]) {
			continue
		}
		if strings.Contains(matching.Normalize(text(player["name"])), query) {
			found = player
			break
		}
	}
	if found == nil {
		fmt.Println(cli.Red(fmt.Sprintf("no encuentro a ningun rival llamado '%s'", name)))
		return nil
	}
	id := text(found["id"])

	if action == "rm" {
		delete(armed, id)
		fmt.Printf("Quitado el clausulazo de %s\n", text(found["name"]))
		return policies.Save(armed)
	}

	ceiling := *maxPay
	if ceiling == 0 {
		// A fifth over the clause, so a small raise by the owner does not cancel it.
		ceiling = number(found["clause"]) * 1.2
		if ceiling == 0 {
			ceiling = number(found["value"]) * 1.5
		}
	}
	policy := armed[id]
	policy.ID, policy.Name, policy.Raid = id, text(found["name"]), true
	policy.MaxPay = &ceiling
	armed[id] = policy
	fmt.Println(cli.Green(fmt.Sprintf(
		"%s: clausulazo programado, pago maximo %s. Se paga en cuanto se libere y solo si "+
			"sigue por debajo de ese importe.", text(found["name"]), cli.Money(ceiling))))
	return policies.Save(armed)
}

// --- leagues ----------------------------------------------------------------------------

// cmdRules shows and edits the house rules of the current league. They live per league id
// because the next league will have agreed something else.
func cmdRules(args []string) error {
	leagueID, _, err := savedLeague()
	if err != nil {
		return err
	}
	all, err := rules.Load()
	if err != nil {
		return err
	}
	league := all[leagueID]

	if len(args) == 0 || args[0] == "list" {
		cli.Heading("Normas de la liga " + leagueID)
		if league.HoldDays > 0 {
			fmt.Printf("  %s  no se puede vender un fichaje hasta pasados %d dias\n",
				cli.Green("se aplica"), league.HoldDays)
			if league.HoldExceptions != "" {
				fmt.Println(cli.Dim("             excepciones: " + league.HoldExceptions))
			}
		} else {
			fmt.Println(cli.Dim("  sin plazo de venta configurado"))
		}
		for _, note := range league.Notes {
			fmt.Printf("  %s     %s\n", cli.Dim("acuerdo"), note)
		}
		fmt.Println(cli.Dim("\n  fantasy rules hold <dias> [excepciones]   ·   fantasy rules note <texto>"))
		return nil
	}

	switch args[0] {
	case "hold":
		if len(args) < 2 {
			return fmt.Errorf("uso: rules hold <dias> [excepciones]")
		}
		days, err := strconv.Atoi(args[1])
		if err != nil || days < 0 {
			return fmt.Errorf("'%s' no es un numero de dias", args[1])
		}
		league.HoldDays = days
		if len(args) > 2 {
			league.HoldExceptions = strings.Join(args[2:], " ")
		}
	case "note":
		if len(args) < 2 {
			return fmt.Errorf("uso: rules note <texto>")
		}
		league.Notes = append(league.Notes, strings.Join(args[1:], " "))
	case "clear":
		league = rules.League{}
	default:
		return fmt.Errorf("no se que es '%s': usa list, hold, note o clear", args[0])
	}

	all[leagueID] = league
	if err := rules.Save(all); err != nil {
		return err
	}
	fmt.Println(cli.Green("guardado"))
	return cmdRules(nil)
}

func cmdLeagues(args []string) error {
	client := api.New()
	leagues, err := client.Leagues(time.Hour)
	if err != nil {
		return err
	}
	cli.Heading("Tus ligas")
	rows := make([][]string, 0, len(leagues))
	for _, league := range leagues {
		rows = append(rows, []string{text(league["id"]), text(league["name"]),
			fmt.Sprintf("%d", int(number(league["teamsNumber"])))})
	}
	fmt.Println(cli.Table([]string{"id", "liga", "equipos"}, rows, map[int]bool{2: true}))
	fmt.Println(cli.Dim("  Se recuerda la primera que uses: settings.json"))
	return nil
}

// --- small helpers ----------------------------------------------------------------------

func findMine(players []map[string]any, name string) (string, string) {
	query := matching.Normalize(name)
	for _, player := range players {
		if !truthyValue(player["is_mine"]) {
			continue
		}
		if strings.Contains(matching.Normalize(text(player["name"])), query) ||
			strings.Contains(matching.Normalize(text(player["full_name"])), query) {
			return text(player["id"]), text(player["name"])
		}
	}
	return "", ""
}

func loadFavourites() (map[string]map[string]any, error) {
	out := map[string]map[string]any{}
	body, err := os.ReadFile(config.FavouritesFile)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return map[string]map[string]any{}, nil
	}
	return out, nil
}

func saveFavourites(favourites map[string]map[string]any) error {
	if err := config.EnsureDirs(); err != nil {
		return err
	}
	blob, err := json.MarshalIndent(favourites, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(config.FavouritesFile, blob, 0o600)
}

func fallbackText(value, other string) string {
	if value != "" {
		return value
	}
	return other
}

func mapFrom(value any) map[string]any {
	if row, ok := value.(map[string]any); ok {
		return row
	}
	return nil
}

func text(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case *string:
		if typed == nil {
			return ""
		}
		return *typed
	case float64:
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	}
	return fmt.Sprint(value)
}

func asFloatValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case *float64:
		if typed == nil {
			return 0, false
		}
		return *typed, true
	case int:
		return float64(typed), true
	case *int:
		if typed == nil {
			return 0, false
		}
		return float64(*typed), true
	}
	return 0, false
}

func number(value any) float64 {
	amount, _ := asFloatValue(value)
	return amount
}

func truthyValue(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case *bool:
		return typed != nil && *typed
	case float64:
		return typed != 0
	}
	return false
}
