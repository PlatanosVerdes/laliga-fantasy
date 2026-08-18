"""Command line interface."""
from __future__ import annotations

import argparse
import json
import sys
import webbrowser
from pathlib import Path
from typing import Any, Callable, Sequence

from . import analysis, auth, favourites, futbolfantasy as ff, http, laliga, logs, matching, policies, report
from .config import DATA_DIR, POSITION_NAMES, SETTINGS_FILE, ensure_dirs

BOLD = "\033[1m"
DIM = "\033[2m"
RED = "\033[31m"
GREEN = "\033[32m"
YELLOW = "\033[33m"
RESET = "\033[0m"


# --- output helpers ---------------------------------------------------------

def _supports_color() -> bool:
    return sys.stdout.isatty()


def paint(text: str, code: str) -> str:
    return f"{code}{text}{RESET}" if _supports_color() else text


def money(value: Any) -> str:
    if value in (None, ""):
        return "-"
    value = float(value)
    sign = "-" if value < 0 else ""
    value = abs(value)
    if value >= 1e6:
        return f"{sign}{value / 1e6:.2f}M"
    if value >= 999_500:
        return f"{sign}{value / 1e6:.2f}M"
    if value >= 1e3:
        return f"{sign}{value / 1e3:.0f}K"
    return f"{sign}{value:.0f}"


def table(headers: Sequence[str], rows: Sequence[Sequence[Any]], *, right: set[int] = frozenset()) -> str:
    if not rows:
        return paint("  (sin resultados)", DIM)
    cells = [[("-" if c is None else str(c)) for c in row] for row in rows]
    widths = [len(h) for h in headers]
    for row in cells:
        for index, cell in enumerate(row):
            widths[index] = max(widths[index], len(cell))
    def line(values: Sequence[str], bold: bool = False) -> str:
        parts = []
        for index, value in enumerate(values):
            parts.append(value.rjust(widths[index]) if index in right else value.ljust(widths[index]))
        text = "  ".join(parts).rstrip()
        return paint(text, BOLD) if bold else text
    out = [line(list(headers), bold=True), paint("  ".join("─" * w for w in widths), DIM)]
    out += [line(row) for row in cells]
    return "\n".join("  " + row for row in out)


def heading(text: str) -> None:
    print()
    print(paint(text, BOLD))


def player_rows(players: Sequence[dict], *, cost_key: str | None = None,
                extra: tuple[str, Callable[[dict], Any]] | None = None) -> tuple[list[str], list[list[Any]]]:
    headers = ["#", "Jugador", "Pos", "Equipo", "Valor"]
    if cost_key:
        headers.append("Coste")
    headers += ["xPts", "Pts/M", "Tit%", "7d%", "Rival", "Score"]
    if extra:
        headers.append(extra[0])

    rows = []
    for index, player in enumerate(players, start=1):
        rival = player.get("next_rival")
        rival_text = f"{rival[:12]}{'(C)' if player.get('next_home') else '(F)'}" if rival else "-"
        status = player.get("status")
        name = player["name"][:22]
        if not player.get("available"):
            name = paint(f"{name}!", RED)
        elif status == "doubtful":
            name = paint(f"{name}?", YELLOW)
        row = [index, name, player["position"], (player.get("team_short") or "?"),
               money(player["value"])]
        if cost_key:
            row.append(money(player.get(cost_key)))
        row += [
            f"{player['xpts']:.2f}",
            f"{player['points_value']:.2f}",
            player.get("start_probability") if player.get("start_probability") is not None else "-",
            f"{player.get('projected_pct') or 0:+.1f}",
            rival_text,
            f"{player['score']:+.2f}",
        ]
        if extra:
            value = extra[1](player)
            row.append(value if isinstance(value, str) else ("-" if value is None else str(value)))
        rows.append(row)
    right = {0, 4, 5, 6, 7, 8, 9} if cost_key else {0, 4, 5, 6, 7, 8}
    return headers, rows


# --- settings ---------------------------------------------------------------

def load_settings() -> dict[str, Any]:
    if SETTINGS_FILE.exists():
        try:
            return json.loads(SETTINGS_FILE.read_text())
        except json.JSONDecodeError:
            return {}
    return {}


def save_settings(values: dict[str, Any]) -> None:
    DATA_DIR.mkdir(parents=True, exist_ok=True)
    SETTINGS_FILE.write_text(json.dumps({**load_settings(), **values}, indent=2))


def resolve_league(args) -> tuple[str | None, str | None, str | None]:
    """Return (league_id, my_team_id, league_name), remembering the choice."""
    settings = load_settings()
    league_id = getattr(args, "league", None) or settings.get("league_id")
    team_id = settings.get("team_id")
    league_name = settings.get("league_name")

    if league_id and team_id and not getattr(args, "refresh_league", False):
        return str(league_id), str(team_id), league_name

    entries = laliga.leagues()
    if not entries:
        return None, None, None
    chosen = None
    if league_id:
        chosen = next((e for e in entries if str(e.get("id")) == str(league_id)), None)
    if chosen is None:
        if len(entries) > 1 and not league_id:
            heading("Tienes varias ligas; usa --league <id> para fijar otra")
            print(table(["id", "liga"], [[e.get("id"), e.get("name")] for e in entries]))
        chosen = entries[0]

    league_id = str(chosen.get("id"))
    league_name = chosen.get("name")
    team = chosen.get("team") or {}
    team_id = str(team.get("id") or "")
    if not team_id:
        user = laliga.me()
        user_id = str(user.get("id") or user.get("userId") or "")
        for entry in laliga.standings(league_id):
            if entry.get("userId") == user_id:
                team_id = entry["teamId"]
                break
    save_settings({"league_id": league_id, "team_id": team_id, "league_name": league_name})
    return league_id, team_id, league_name


# --- commands ---------------------------------------------------------------

def cmd_auth(args) -> int:
    action = args.action
    if action == "snippet":
        print(auth.BROWSER_SNIPPET)
        print(paint("Cuando lo tengas en el portapapeles:  pbpaste | python3 fantasy.py auth paste", DIM))
        return 0
    if action == "paste":
        text = sys.stdin.read()
        tokens = auth.parse_pasted(text)
        auth.save_tokens(tokens)
        print(paint("Sesion guardada.", GREEN),
              f"{tokens.get('email') or '?'} · caduca en {auth.seconds_left(tokens) // 60} min")
        if not tokens.get("refresh_token"):
            print(paint("Sin refresh_token: tendras que repetir el pegado cuando caduque.", YELLOW))
        return 0
    if action == "browser":
        flow = auth.start_browser_login()
        auth.save_pending(flow)
        print()
        print(paint("1.", BOLD), "Abre esta URL e inicia sesion con la misma cuenta que usas "
                                 "en la app:")
        print()
        print(f"   {flow['url']}")
        print()
        print(paint("   Si Google pide permisos extra (YouTube, genero, fecha de nacimiento,", DIM))
        print(paint("   direccion postal), dejalos TODOS desmarcados: solo hace falta la "
                    "identidad.", DIM))
        print()
        print(paint("2.", BOLD), "Al terminar, el navegador intentara abrir "
                                 f"{auth.NATIVE_REDIRECT_URI}")
        print("   Como nada maneja ese esquema, veras un error o se quedara cargando en blanco.")
        print("   Eso significa que ha funcionado. Necesitamos la URL de ese salto:")
        print()
        print(paint("   a)", BOLD), "mirala en la barra de direcciones, o")
        print(paint("   b)", BOLD), "abre DevTools (Cmd+Opt+I) > Network con 'Preserve log' ANTES "
                                    "de terminar,")
        print("      y busca la respuesta 302 cuyo Location empieza por authredirect://")
        print()
        print(paint("3.", BOLD), "Pasala al segundo comando (acepta la URL entera o solo el code):")
        print()
        print(paint("   python3 fantasy.py auth code '<URL o code>'", BOLD))
        print()
        print(paint(f"   Tienes {auth.PENDING_TTL // 60} minutos: el verificador PKCE queda "
                    f"guardado en {auth.PENDING_FILE.name}.", DIM))
        if not args.no_open:
            webbrowser.open(flow["url"])
        return 0
    if action == "code":
        if not args.value:
            print(paint("Falta la URL o el code: python3 fantasy.py auth code '<URL>'", RED))
            return 1
        flow = auth.load_pending()
        code = auth.extract_code(args.value, flow.get("state"))
        tokens = auth.exchange_code(code, flow["verifier"])
        auth.clear_pending()
        print(paint("Sesion guardada.", GREEN),
              f"{tokens.get('email') or '?'} · caduca en {auth.seconds_left(tokens) // 60} min"
              f" · refresh: {'si' if tokens.get('refresh_token') else 'no'}")
        return 0
    if action == "login":
        import getpass
        email = args.email or input("Email: ")
        password = args.password or getpass.getpass("Password: ")
        tokens = auth.login_password(email, password)
        print(paint("Login correcto.", GREEN), tokens.get("email"))
        return 0
    if action == "refresh":
        tokens = auth.load_tokens()
        if not tokens:
            print(paint("No hay sesion guardada.", RED))
            return 1
        tokens = auth.refresh(tokens)
        print(paint("Token renovado.", GREEN), f"caduca en {auth.seconds_left(tokens) // 60} min")
        return 0
    if action == "logout":
        auth.clear_tokens()
        print("Sesion borrada.")
        return 0

    tokens = auth.load_tokens()
    if not tokens:
        print("Sin sesion. Ejecuta: python3 fantasy.py auth snippet")
        return 1
    left = auth.seconds_left(tokens)
    state = paint("valida", GREEN) if left > 0 else paint("caducada", RED)
    print(f"Cuenta      : {tokens.get('email') or '?'}")
    print(f"Proveedor   : {tokens.get('idp') or 'laliga'}")
    print(f"Token       : {state} ({left // 60} min)")
    print(f"Refresh     : {'si' if tokens.get('refresh_token') else 'no'}")
    print(f"client_id   : {tokens.get('client_id')}")
    return 0


def cmd_leagues(args) -> int:
    entries = laliga.leagues()
    rows = [[e.get("id"), e.get("name"), (e.get("team") or {}).get("name"),
             e.get("teamsNumber") or e.get("numTeams") or "-"] for e in entries]
    heading("Tus ligas")
    print(table(["id", "nombre", "mi equipo", "equipos"], rows))
    return 0


def cmd_standings(args) -> int:
    league_id, my_team_id, name = resolve_league(args)
    if not league_id:
        print(paint("No se ha encontrado ninguna liga.", RED))
        return 1
    heading(f"Clasificacion · {name}")
    rows = []
    for position, entry in enumerate(laliga.standings(league_id), start=1):
        mark = " <-- tu" if entry["teamId"] == my_team_id else ""
        rows.append([position, (entry.get("manager") or "-"), entry.get("teamName"),
                     entry.get("points"), money(entry.get("teamValue")), mark])
    print(table(["#", "manager", "equipo", "pts", "patrimonio", ""], rows, right={0, 3, 4}))
    print(paint("  patrimonio = caja + valor de plantilla. `advise` desglosa la caja estimada "
                "de cada rival.", DIM))
    return 0


def cmd_activity(args) -> int:
    league_id, my_team_id, name = resolve_league(args)
    if not league_id:
        print(paint("No se ha encontrado ninguna liga.", RED))
        return 1
    heading(f"Movimientos · {name}")
    seen = 0
    for index in range(args.pages):
        try:
            events = laliga.activity(league_id, index)
        except http.HttpError as exc:
            if exc.status == 404:
                break
            raise
        if not events:
            break
        rows = []
        for event in events:
            rows.append([
                str(event.get("date") or event.get("createdAt") or "")[:16].replace("T", " "),
                (event.get("type") or event.get("operation") or "?")[:18],
                _activity_who(event)[:20],
                _activity_what(event)[:24],
                money(event.get("amount") or event.get("money") or event.get("price")),
            ])
        print(table(["fecha", "tipo", "quien", "que", "importe"], rows, right={4}))
        seen += len(events)
    if not seen:
        print(paint("  Sin movimientos todavia (o la respuesta tiene otra forma: "
                    "prueba `probe activity`).", YELLOW))
    return 0


def _activity_who(event: dict) -> str:
    for key in ("manager", "userName", "user", "teamName", "buyer"):
        value = event.get(key)
        if isinstance(value, str):
            return value
        if isinstance(value, dict):
            return str(value.get("managerName") or value.get("name") or "?")
    return "?"


def _activity_what(event: dict) -> str:
    player = event.get("player") or event.get("playerMaster") or {}
    if isinstance(player, dict):
        return str(player.get("nickname") or player.get("name") or "-")
    return str(player or "-")


def _budget(args, team_id: str | None) -> tuple[int, int]:
    if args.budget is not None:
        return args.budget, args.debt or 0
    if not team_id:
        return 0, 0
    try:
        payload = laliga.team_money(team_id)
    except Exception as exc:
        print(paint(f"No se ha podido leer el saldo ({exc}); usa --budget.", YELLOW))
        return 0, 0
    cash = analysis.money_from_payload(payload)
    if cash is None:
        print(paint("Respuesta de saldo no reconocida; ejecuta `probe money` y usa --budget.", YELLOW))
        print(paint(json.dumps(payload, ensure_ascii=False)[:400], DIM))
        return 0, 0
    return cash, (args.debt if args.debt is not None else (analysis.max_debt_from_payload(payload) or 0))


def _load(args, *, need_league: bool = True) -> tuple[dict, dict | None, dict]:
    league_id = my_team_id = league_name = None
    if need_league and not args.no_auth:
        try:
            league_id, my_team_id, league_name = resolve_league(args)
        except Exception as exc:
            print(paint(f"Sin datos de liga ({exc}). Sigo solo con datos publicos.", YELLOW))

    universe = analysis.build_universe(league_id=league_id, my_team_id=my_team_id,
                                      ff_ttl=0 if args.fresh else 2 * 3600)
    if args.deep:
        shortlist = sorted(universe["players"], key=lambda p: p["value"], reverse=True)
        analysis.deep_enrich([p for p in shortlist if p["prior_based"]],
                             limit=max(args.limit, 20))
        analysis.apply_scores(universe["players"])
    advice = None
    if league_id and universe["ownership_loaded"]:
        cash, debt = _budget(args, my_team_id)
        advice = analysis.recommend(universe, budget=cash, max_debt=debt, limit=args.limit)
    context = {"league_id": league_id, "team_id": my_team_id, "league_name": league_name}
    return universe, advice, context


def cmd_advise(args) -> int:
    universe, advice, context = _load(args)
    week = universe["week"]
    print()
    print(paint(f"Jornada {week.get('weekNumber')}", BOLD),
          paint(f"({'en juego' if week.get('isLive') else 'cerrada'}, cierra {week.get('closingWeekDate','?')[:16]})", DIM))
    print(paint(f"{len(universe['players'])} jugadores · {universe['matched_count']} cruzados con "
                f"futbolfantasy · peso temporada actual {universe['current_weight']:.0%}", DIM))

    if not advice:
        print(paint("\nSin datos de liga: muestro solo el ranking global.", YELLOW))
        top = sorted((p for p in universe["players"] if p["available"]),
                     key=lambda p: p["score"], reverse=True)[:args.limit]
        heading("Mejores jugadores de LaLiga por score")
        print(table(*player_rows(top)))
        return 0

    heading(f"Saldo {money(advice['budget'])} · poder de compra {money(advice['spending_power'])}")
    shape_bits = []
    for position_id, data in advice["shape"].items():
        flag = paint("falta", RED) if data["gap"] else (paint("sobra", YELLOW) if data["surplus"] else "ok")
        shape_bits.append(f"{POSITION_NAMES[position_id][:3]} {data['owned']}/{data['ideal']} {flag}")
    print("  " + " · ".join(shape_bits))

    analysis.enrich_buckets(advice, limit=args.limit)

    heading("PUJAR AHORA · mercado libre de hoy")
    print(table(*player_rows(advice["bids_now"], cost_key="entry_cost",
                             extra=("PujaMax", lambda p: money(p.get("ideal_bid")) if p.get("ideal_bid") else "-"))))
    if advice["bids_now"]:
        expiry = (advice["bids_now"][0].get("market") or {}).get("expires")
        print(paint(f"  El mercado cierra {str(expiry)[:16].replace('T', ' ')}.", DIM))

    heading("EN VENTA por rivales")
    print(table(*player_rows(advice["asks"], cost_key="entry_cost",
                             extra=("Pide", lambda p: (f"{p.get('ask_ratio', 0):.2f}x valor"
                                                       + (" CARO" if p.get("overpriced") else ""))))))

    if advice["my_listings"]:
        heading("MIS VENTAS EN CURSO")
        rows=[[p["name"][:22], money(p["value"]), money(p["entry_cost"]),
               f"{p.get('ask_ratio', 0):.2f}x",
               "PIDES MENOS DE SU VALOR" if p.get("underpriced") else ""]
              for p in advice["my_listings"]]
        print(table(["jugador", "valor", "pides", "ratio", ""], rows, right={1,2,3}))

    heading("CLAUSULAS pagables")
    if not advice["raids"] and advice.get("clauses_locked"):
        print(paint(f"  Ninguna: las {advice['clauses_locked']} clausulas de la liga estan "
                    f"bloqueadas hasta {str(advice.get('clauses_unlock_from'))[:10]}.", DIM))
    else:
        print(table(*player_rows(advice["raids"], cost_key="entry_cost",
                                 extra=("Dueño", lambda p: (p.get("owner") or "-")[:14]))))

    heading("VENDER · candidatos de tu plantilla")
    print(table(*player_rows(advice["sells"],
                             extra=("Motivos", lambda p: "; ".join(p.get("reasons") or []) or "-"))))

    if advice["exposure"]:
        heading("SUBIR CLAUSULA · riesgo de que te los quiten")
        rows = [[p["name"][:22], p["position"], money(p["value"]), money(p.get("clause")),
                 f"{p.get('clause_margin', 0):.2f}x", p.get("threats", 0),
                 (p.get("top_threat") or "-")[:16], f"{p['score']:+.2f}"]
                for p in advice["exposure"]]
        print(table(["jugador", "pos", "valor", "clausula", "margen", "pueden", "el mas rico",
                     "score"], rows, right={2, 3, 4, 5, 7}))

    if advice.get("rivals"):
        model = advice.get("cash_model") or {}
        heading("RIVALES · caja reconstruida del historial de fichajes")
        rows = [[t["manager"] or t["name"], t["players"], money(t["squad_value"]),
                 money(t["estimated_cash"]), f"{t.get('net_flow', 0) / 1e6:+.2f}M",
                 t["points"]] for t in advice["rivals"]]
        print(table(["manager", "jug", "plantilla", "caja est.", "neto fichajes", "pts"], rows,
                    right={1, 2, 3, 4, 5}))
        if model.get("anchored"):
            print(f"  {paint('Modelo de caja:', DIM)} "
                  + paint(f"anclado en tu saldo real ({money(advice['budget'])})", GREEN)
                  + paint(f" · base {money(model['base'])} por manager · "
                          f"margen ±{money(model['uncertainty'])} por las recompensas diarias "
                          f"que no registra el historial", DIM))
        else:
            print(f"  {paint('Modelo de caja:', DIM)} "
                  + paint(f"sin anclar: asumo {money(model.get('base'))} de inicio para todos",
                          YELLOW))
    print()
    return 0


def cmd_squad(args) -> int:
    universe, advice, _ = _load(args)
    if not advice:
        print(paint("Necesito sesion y liga para ver tu plantilla.", RED))
        return 1
    heading("Mi plantilla")
    squad = advice["squad"]
    print(table(*player_rows(squad)))
    print()
    print(f"  Valor total : {money(sum(p['value'] for p in squad))}")
    print(f"  xPts mejores 11: {sum(sorted((p['xpts'] for p in squad), reverse=True)[:11]):.1f}")
    return 0


def cmd_market(args) -> int:
    universe, advice, _ = _load(args, need_league=not args.global_only)
    players = [p for p in universe["players"] if p["available"]]
    if args.position:
        wanted = args.position.upper()
        players = [p for p in players if p["position"] == wanted]
    if args.max_price:
        players = [p for p in players if p["value"] <= args.max_price]
    if args.free and universe["ownership_loaded"]:
        players = [p for p in players if not p["owner"]]

    key = {"score": "score", "value": "points_value", "trend": "projected_pct",
           "xpts": "xpts", "price": "value"}[args.sort]
    players.sort(key=lambda p: p.get(key) or 0, reverse=True)
    heading(f"Ranking por {args.sort}")
    print(table(*player_rows(players[:args.limit],
                             extra=("Dueño", lambda p: (p.get("owner") or "libre")[:14])
                             if universe["ownership_loaded"] else None)))
    return 0


def cmd_player(args) -> int:
    universe, advice, _ = _load(args)
    query = matching.normalize(args.name)
    hits = [p for p in universe["players"]
            if query in matching.normalize(p["name"]) or query in matching.normalize(p.get("full_name"))]
    if not hits:
        print(paint(f"No he encontrado a nadie parecido a '{args.name}'.", RED))
        return 1
    hits.sort(key=lambda p: p["score"], reverse=True)
    player = hits[0]
    if len(hits) > 1:
        print(paint("Coincidencias: " + ", ".join(p["name"] for p in hits[:8]), DIM))

    heading(f"{player['name']} · {POSITION_NAMES[player['position_id']]} · {player['team']}")
    lines = [
        ("Valor de mercado", money(player["value"])),
        ("Estado", player["status"]),
        ("Puntos 25/26", f"{player['last_season_points']:.0f}"),
        ("Puntos temporada", f"{player['season_points']:.0f} (media {player['season_avg']:.2f})"),
        ("xPts / jornada", f"{player['xpts']:.2f}"),
        ("Puntos por millon", f"{player['points_value']:.3f}"),
        ("Probabilidad titular", f"{player['start_probability']}%" if player["start_probability"] is not None else "-"),
        ("Proximo rival", f"{player['next_rival']} ({'casa' if player['next_home'] else 'fuera'})"
                          if player["next_rival"] else "-"),
        ("Tendencia valor", f"{player.get('trend_label') or '-'} · "
                            f"1d {player.get('pct_1d') or 0:+.2f}% · 7d {player.get('pct_7d') or 0:+.2f}% · "
                            f"proyeccion 7d {player['projected_pct']:+.2f}%"),
        ("Score / ranking", f"{player['score']:+.2f} · #{player['rank']} global · "
                            f"#{player['position_rank']} en {player['position']}"),
        ("Dueño", player.get("owner") or ("libre" if universe["ownership_loaded"] else "?")),
        ("Clausula", money(player.get("clause")) + (" (bloqueada)" if player.get("clause_locked") else "")),
    ]
    for label, value in lines:
        print(f"  {label:<22}{value}")

    if player.get("ff_id"):
        detail = ff.player_detail(player["ff_id"])
        ideal = detail.get("ideal_bid") or 0
        print(f"  {'Puja max. rentable':<22}"
              + (money(ideal) if ideal else paint("sin rentabilidad a este precio", YELLOW)))
        print(f"  {'Valor max/min':<22}{money(detail.get('max_value'))} ({detail.get('max_date')}) / "
              f"{money(detail.get('min_value'))} ({detail.get('min_date')})")
        if detail.get("injury_marks"):
            print(f"  {'Marcas de lesion':<22}{', '.join(detail['injury_marks'][:8])}")

    if args.history:
        slug = args.slug or matching.slugify_ff(player.get("ff_name") or player["name"])
        try:
            page = ff.player_page(slug)
        except Exception as exc:
            print(paint(f"  No he podido leer /jugadores/{slug}: {exc}", YELLOW))
            return 0
        heading(f"Historico futbolfantasy ({page.get('name')})")
        rows = [[m["week"], " vs ".join(m["teams"]) if m["teams"] else "-", m["result"] or "-",
                 m["role"], m["points"] if m["points"] is not None else "-",
                 m["sofascore"] or "-", "lesion" if m["injured"] else ""]
                for m in page["matches"][:args.history]]
        print(table(["J", "partido", "res", "rol", "pts", "sofa", ""], rows, right={0, 4, 5}))
        print(f"\n  Partidos jugados {page['games_played']} · total {page['total_points']:.0f} pts"
              f" · media {page['avg_points'] or 0:.2f}")
    return 0


def cmd_report(args) -> int:
    universe, advice, context = _load(args)
    if advice:
        analysis.enrich_buckets(advice, limit=args.limit)
    path = Path(args.output) if args.output else None
    written = report.write(universe, advice, context=context, path=path,
                           activity=universe.get("activity"))
    print(paint(f"Informe escrito en {written}", GREEN))
    if args.json:
        json_path = written.with_suffix(".json")
        report.dump_json(universe, advice, json_path, universe.get("activity"))
        print(f"Datos crudos en {json_path}")
    if args.open:
        webbrowser.open(written.as_uri())
    return 0


def cmd_fav(args) -> int:
    if args.action == "list" or not args.name:
        entries = favourites.load()
        if not entries:
            print("Sin favoritos. Añade con: python3 fantasy.py fav add <nombre>")
            return 0
        heading("Favoritos")
        print(table(["id", "jugador", "nota"],
                    [[e.get("id"), e.get("name") or "?", e.get("note") or ""]
                     for e in entries.values()]))
        return 0

    universe = analysis.build_universe(league_id=None)
    query = matching.normalize(args.name)
    hits = [p for p in universe["players"]
            if query in matching.normalize(p["name"])
            or query in matching.normalize(p.get("full_name"))]
    if not hits:
        print(paint(f"No he encontrado a nadie parecido a '{args.name}'.", RED))
        return 1
    hits.sort(key=lambda p: p["score"], reverse=True)
    player = hits[0]
    if len(hits) > 1:
        print(paint("Coincidencias: " + ", ".join(p["name"] for p in hits[:8]), DIM))
    if args.action == "add":
        favourites.add(player["id"], player["name"])
        print(paint(f"★ {player['name']} añadido a favoritos.", GREEN))
    else:
        removed = favourites.remove(player["id"])
        print(f"{'Quitado' if removed else 'No estaba en'} favoritos: {player['name']}")
    return 0


def cmd_raid(args) -> int:
    """Programar un clausulazo para cuando se libere la clausula."""
    universe, advice, _ = _load(args)
    cash = (advice or {}).get("budget") or 0
    if args.action == "list" or not args.name:
        scheduled = {k: v for k, v in policies.load().items() if v.get("raid")}
        if not scheduled:
            print("Sin clausulazos programados. Añade con:\n"
                  "  python3 fantasy.py raid add <nombre> --max 25000000")
            return 0
        heading("Clausulazos programados")
        print(table(["jugador", "pago maximo"],
                    [[e.get("name") or e["id"], money(e.get("max_pay"))]
                     for e in scheduled.values()], right={1}))
        plan = policies.raid_plan(universe["players"], cash=cash)
        heading("Estado ahora mismo")
        print(table(["jugador", "dueño", "clausula", "mi limite", "estado", "motivo"],
                    [[a["name"], (a.get("owner") or "-")[:14], money(a.get("clause")),
                      money(a.get("max_pay")), a["action"], a["why"]] for a in plan],
                    right={2, 3}))
        return 0

    query = matching.normalize(args.name)
    hits = [p for p in universe["players"] if not p["is_mine"]
            and (query in matching.normalize(p["name"])
                 or query in matching.normalize(p.get("full_name")))]
    if not hits:
        print(paint(f"'{args.name}' no aparece como jugador de otro manager.", RED))
        return 1
    hits.sort(key=lambda p: p["score"], reverse=True)
    player = hits[0]
    if args.action == "rm":
        policies.set_policy(player["id"], raid=False)
        print(f"Clausulazo cancelado: {player['name']}")
        return 0

    clause = player.get("clause") or 0
    ceiling = args.max or int(clause * 1.2) or int(player["value"] * 1.5)
    policies.set_policy(player["id"], name=player["name"], raid=True, max_pay=ceiling)
    print(paint(f"Clausulazo programado: {player['name']} ({player.get('owner')}).", GREEN))
    print(f"  Clausula ahora : {money(clause)}")
    print(f"  Pago maximo    : {money(ceiling)}")
    if clause and ceiling < clause:
        print(paint("  Tu limite esta por debajo de la clausula actual: no se ejecutara "
                    "salvo que baje.", YELLOW))
    if player.get("clause_locked"):
        print(paint(f"  Se disparara en cuanto se libere "
                    f"({str(player.get('clause_locked_until'))[:16]}).", DIM))
    return 0


def cmd_always(args) -> int:
    """`always` = standing instructions: keep listed, accept over a threshold."""
    if args.action == "list" or not args.name:
        entries = policies.load()
        if not entries:
            print("Sin instrucciones. Añade con:\n"
                  "  python3 fantasy.py always add <nombre> --min 12000000 --accept 13000000")
            return 0
        heading("Siempre en mercado")
        print(table(["jugador", "precio minimo", "acepto por encima de"],
                    [[e.get("name") or e["id"], money(e.get("min_price")),
                      money(e.get("accept_above"))] for e in entries.values()],
                    right={1, 2}))
        universe, advice, _ = _load(args)
        plan = policies.plan(universe["players"])
        if plan:
            heading("Que haria ahora mismo")
            print(table(["jugador", "accion", "importe", "motivo"],
                        [[a["name"], a["action"], money(a.get("amount")), a["why"]]
                         for a in plan], right={2}))
            print(paint("  Se ejecuta en el proximo refresco del servidor; con "
                        "--read-only no.", DIM))
        return 0

    universe, advice, _ = _load(args)
    query = matching.normalize(args.name)
    hits = [p for p in universe["players"] if p["is_mine"]
            and (query in matching.normalize(p["name"])
                 or query in matching.normalize(p.get("full_name")))]
    if not hits:
        print(paint(f"'{args.name}' no esta en tu plantilla.", RED))
        return 1
    player = hits[0]
    if args.action == "rm":
        print(("Quitado" if policies.remove(player["id"]) else "No estaba") +
              f": {player['name']}")
        return 0
    entry = policies.set_policy(player["id"], name=player["name"],
                                min_price=args.min or int(player["value"]),
                                accept_above=args.accept or int(player["value"]))
    print(paint(f"{player['name']}: siempre en mercado a {money(entry['min_price'])}, "
                f"acepto ofertas desde {money(entry['accept_above'])}.", GREEN))
    return 0


def cmd_serve(args) -> int:
    from . import serve

    league_id = my_team_id = None
    if not args.no_auth:
        try:
            league_id, my_team_id, _ = resolve_league(args)
        except Exception as exc:
            print(paint(f"Sin liga ({exc}): sirvo solo datos publicos.", YELLOW))

    def builder():
        universe, advice, context = _load(args)
        if advice:
            analysis.enrich_buckets(advice, limit=args.limit)
        return universe, advice, context

    allow_writes = not args.read_only
    if not allow_writes:
        print(paint("Solo lectura: ninguna operacion movera dinero.", DIM))
    elif args.auto:
        universe = builder()[0]
        cash = 0
        pending = [a for a in policies.plan(universe["players"]) if a["action"] != "ninguna"]
        pending += [a for a in policies.raid_plan(universe["players"], cash=cash)
                    if a["action"] == "pagar_clausula"]
        print(paint("AUTOMATICO: las instrucciones programadas se ejecutaran solas.", YELLOW))
        if pending:
            print(paint(f"  {len(pending)} se ejecutara(n) en el primer refresco:", YELLOW))
            for action in pending:
                print(f"    · {action['name']}: {action['action']} — {action['why']}")
            print(paint("  Ctrl-C ahora si no es lo que quieres.", YELLOW))
        else:
            print(paint("  Ninguna en cola ahora mismo.", DIM))
    else:
        print(paint("Las instrucciones programadas se muestran pero NO se ejecutan. "
                    "Añade --auto para que actuen solas.", DIM))

    return serve.run(builder, host=args.host, port=args.port, interval=args.interval,
                     allow_writes=allow_writes, auto=args.auto, league_id=league_id,
                     my_team_id=my_team_id)


def cmd_probe(args) -> int:
    """Dump raw endpoint payloads; useful when the API shape changes again."""
    league_id, team_id, _ = (None, None, None)
    if args.what in {"leagues", "standing", "squad", "money", "market", "me", "activity"}:
        league_id, team_id, _ = resolve_league(args)

    calls = {
        "week": lambda: laliga.current_week(ttl=0),
        "players": lambda: laliga.all_players(ttl=0)[:3],
        "teams": lambda: laliga.teams_master(ttl=0)[:3],
        "me": lambda: laliga.me(ttl=0),
        "leagues": lambda: laliga.leagues(ttl=0),
        "standing": lambda: laliga.standings(league_id, ttl=0),
        "squad": lambda: laliga.team_squad(league_id, team_id, ttl=0),
        "money": lambda: laliga.team_money(team_id, ttl=0),
        "market": lambda: laliga.market(league_id, ttl=0),
        "activity": lambda: laliga.activity(league_id, 0, ttl=0),
        "ffmarket": lambda: ff.market(ttl=0)[:3],
    }
    if args.what not in calls:
        print(f"Opciones: {', '.join(calls)}")
        return 1
    payload = calls[args.what]()
    print(json.dumps(payload, indent=2, ensure_ascii=False)[:args.chars])
    return 0


def cmd_cache(args) -> int:
    ensure_dirs()
    from .config import CACHE_DIR
    files = list(CACHE_DIR.glob("*.cache"))
    if args.clear:
        for file in files:
            file.unlink()
        print(f"Borrados {len(files)} ficheros de cache.")
        return 0
    total = sum(f.stat().st_size for f in files)
    print(f"{len(files)} ficheros · {total / 1e6:.1f} MB en {CACHE_DIR}")
    return 0


# --- parser -----------------------------------------------------------------

def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="fantasy",
        description="Analisis de LaLiga Fantasy cruzando la API oficial con futbolfantasy.com")
    parser.add_argument("-v", "--verbose", action="store_true", help="log detallado en pantalla")
    parser.add_argument("-q", "--quiet", action="store_true", help="silenciar avisos")
    common = argparse.ArgumentParser(add_help=False)
    common.add_argument("--league", help="id de liga (se recuerda tras la primera vez)")
    common.add_argument("--refresh-league", action="store_true", help="volver a resolver liga/equipo")
    common.add_argument("--budget", type=int, help="saldo en euros (evita leerlo de la API)")
    common.add_argument("--debt", type=int, help="deuda maxima permitida")
    common.add_argument("--limit", type=int, default=15, help="filas por bloque")
    common.add_argument("--fresh", action="store_true", help="ignorar cache de futbolfantasy")
    common.add_argument("--no-auth", action="store_true", help="solo datos publicos")
    common.add_argument("--deep", action="store_true",
                        help="leer la ficha de futbolfantasy de los candidatos sin historico "
                             "(mas lento, pero corrige a Mbappe, Lamine, ascendidos...)")

    sub = parser.add_subparsers(dest="command", required=True)

    auth_parser = sub.add_parser("auth", help="gestionar la sesion")
    auth_parser.add_argument("action", nargs="?", default="status",
                             choices=["status", "browser", "code", "snippet", "paste", "login",
                                      "refresh", "logout"])
    auth_parser.add_argument("value", nargs="?",
                             help="para `auth code`: la URL de redireccion o el code")
    auth_parser.add_argument("--email")
    auth_parser.add_argument("--password")
    auth_parser.add_argument("--no-open", action="store_true",
                             help="no abrir el navegador automaticamente")
    auth_parser.set_defaults(func=cmd_auth)

    leagues_parser = sub.add_parser("leagues", parents=[common], help="listar tus ligas")
    leagues_parser.set_defaults(func=cmd_leagues)

    standings_parser = sub.add_parser("standings", parents=[common], help="clasificacion de la liga")
    standings_parser.set_defaults(func=cmd_standings)

    activity_parser = sub.add_parser("activity", parents=[common],
                                     help="historial de fichajes y ventas de la liga")
    activity_parser.add_argument("--pages", type=int, default=3, help="paginas a recorrer")
    activity_parser.set_defaults(func=cmd_activity)

    advise_parser = sub.add_parser("advise", parents=[common],
                                   help="recomendaciones de fichajes, clausulas y ventas")
    advise_parser.set_defaults(func=cmd_advise)

    squad_parser = sub.add_parser("squad", parents=[common], help="tu plantilla valorada")
    squad_parser.set_defaults(func=cmd_squad)

    market_parser = sub.add_parser("market", parents=[common], help="ranking de jugadores")
    market_parser.add_argument("--sort", default="score",
                               choices=["score", "value", "trend", "xpts", "price"])
    market_parser.add_argument("--position", help="POR/DEF/MED/DEL")
    market_parser.add_argument("--max-price", type=int)
    market_parser.add_argument("--free", action="store_true", help="solo sin dueño en tu liga")
    market_parser.add_argument("--global-only", action="store_true", help="ignorar la liga")
    market_parser.set_defaults(func=cmd_market)

    player_parser = sub.add_parser("player", parents=[common], help="ficha de un jugador")
    player_parser.add_argument("name")
    player_parser.add_argument("--history", type=int, nargs="?", const=10, default=0,
                               help="jornadas de historico de futbolfantasy")
    player_parser.add_argument("--slug", help="slug de futbolfantasy si el automatico falla")
    player_parser.set_defaults(func=cmd_player)

    report_parser = sub.add_parser("report", parents=[common], help="generar el informe HTML")
    report_parser.add_argument("--output")
    report_parser.add_argument("--open", action="store_true", help="abrir en el navegador")
    report_parser.add_argument("--json", action="store_true", help="volcar tambien el JSON")
    report_parser.set_defaults(func=cmd_report)

    fav_parser = sub.add_parser("fav", parents=[common], help="favoritos (estrella en la UI)")
    fav_parser.add_argument("action", nargs="?", default="list", choices=["list", "add", "rm"])
    fav_parser.add_argument("name", nargs="?")
    fav_parser.set_defaults(func=cmd_fav)

    raid_parser = sub.add_parser("raid", parents=[common],
                                 help="programar un clausulazo al liberarse la clausula")
    raid_parser.add_argument("action", nargs="?", default="list", choices=["list", "add", "rm"])
    raid_parser.add_argument("name", nargs="?")
    raid_parser.add_argument("--max", type=int, help="pago maximo que aceptas")
    raid_parser.set_defaults(func=cmd_raid)

    always_parser = sub.add_parser("always", parents=[common],
                                   help="siempre en mercado: relistar y aceptar la mejor oferta")
    always_parser.add_argument("action", nargs="?", default="list", choices=["list", "add", "rm"])
    always_parser.add_argument("name", nargs="?")
    always_parser.add_argument("--min", type=int, help="precio minimo al listarlo")
    always_parser.add_argument("--accept", type=int, help="aceptar ofertas desde este importe")
    always_parser.set_defaults(func=cmd_always)

    serve_parser = sub.add_parser("serve", parents=[common],
                                  help="servir el informe por HTTP y regenerarlo periodicamente")
    serve_parser.add_argument("--host", default="0.0.0.0")
    serve_parser.add_argument("--port", type=int, default=8000)
    serve_parser.add_argument("--interval", type=int, default=120,
                              help="segundos entre refrescos (default 120)")
    serve_parser.add_argument("--read-only", action="store_true",
                              help="desactivar cualquier operacion que mueva dinero")
    serve_parser.add_argument("--auto", action="store_true",
                              help="ejecutar solo las instrucciones programadas sin preguntar "
                                   "(siempre-en-mercado y clausulazos). Sin esto se muestran "
                                   "pero no se ejecutan")
    serve_parser.set_defaults(func=cmd_serve)

    probe_parser = sub.add_parser("probe", parents=[common], help="volcar respuestas crudas")
    probe_parser.add_argument("what")
    probe_parser.add_argument("--chars", type=int, default=4000)
    probe_parser.set_defaults(func=cmd_probe)

    cache_parser = sub.add_parser("cache", help="estado de la cache local")
    cache_parser.add_argument("--clear", action="store_true")
    cache_parser.set_defaults(func=cmd_cache)

    return parser


def main(argv: Sequence[str] | None = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)
    ensure_dirs()
    logs.setup(verbose=args.verbose, quiet=args.quiet, color=_supports_color())
    logs.log.info("command start", extra={"command": args.command})
    try:
        code = args.func(args)
        logs.log.info("command done", extra={"command": args.command, "exit": code})
        return code
    except (KeyboardInterrupt, EOFError):
        return 130
    except ValueError as exc:
        logs.log.error("bad input", extra={"command": args.command, "reason": str(exc)})
        print(paint(str(exc), RED))
        return 1
    except http.HttpError as exc:
        logs.log.error("command failed", extra={"command": args.command, "status": exc.status})
        if exc.status == 401:
            print(paint("La API responde 401: la sesion ha caducado. "
                        "Vuelve a pegar el token (`auth snippet`).", RED))
        else:
            print(paint(str(exc), RED))
        return 1
    except RuntimeError as exc:
        logs.log.error("command failed", extra={"command": args.command, "reason": str(exc)})
        print(paint(str(exc), RED))
        return 1
