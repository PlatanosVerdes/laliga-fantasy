"""Standing instructions per player: keep him listed, and take a good enough offer.

Listings expire and get pulled; if you want somebody permanently on the market you
have to relist him by hand every day. This is that chore, automated — but only for
players you name explicitly and never below a floor you set. They run whenever the
server does — firing unattended is the whole point, since a clause opens when it
opens — and `--no-auto` suspends them, `--read-only` stops everything.

Nothing here decides on its own what a good price is, and nothing sells a player
unless you named the number: `min_price` is only the price he is listed at, and
without an explicit `accept_above` no offer is ever accepted automatically, however
good it looks. Market value is not that number — offers land at or above it all the
time, so defaulting to it would mean handing the player over on the first bid.
"""
from __future__ import annotations

import json
from typing import Any

from .config import CONFIG_DIR, MIN_PER_POSITION, POLICY_FILE
from .logs import log


def _money(amount: float) -> str:
    """Thousands with dots, and nothing else touched.

    The reasons used to be built with f"...{amount:,}".replace(",", ".") per fragment, which
    also turned the *prose* commas into full stops: "ya listado a 12.000.000. mejor oferta
    9.799.596" read as two sentences and was one. Found by comparing against the Go port.
    """
    return f"{int(amount):,}".replace(",", ".")


def load() -> dict[str, dict[str, Any]]:
    if not POLICY_FILE.exists():
        return {}
    try:
        data = json.loads(POLICY_FILE.read_text())
    except json.JSONDecodeError:
        return {}
    return {str(k): v for k, v in data.items()} if isinstance(data, dict) else {}


def save(policies: dict[str, dict[str, Any]]) -> None:
    CONFIG_DIR.mkdir(parents=True, exist_ok=True)
    POLICY_FILE.write_text(json.dumps(policies, indent=2, ensure_ascii=False))


def set_policy(player_id: str, *, name: str | None = None, always_listed: bool | None = None,
               min_price: int | None = None, accept_above: int | None = None,
               auto_sell: bool | None = None,
               raid: bool | None = None, max_pay: int | None = None,
               unset: tuple[str, ...] = ()) -> dict[str, Any]:
    """`unset` removes keys outright, which is how a threshold is taken back off.

    Passing None only means "leave it alone", so clearing needs its own way in:
    otherwise disarming an automatic sale would be impossible from a form where
    emptying the field is exactly how a person says "no".
    """
    policies = load()
    entry = {**policies.get(str(player_id), {}), "id": str(player_id)}
    for key in unset:
        entry.pop(key, None)
    if name:
        entry["name"] = name
    if always_listed is not None:
        entry["always_listed"] = always_listed
    if min_price is not None:
        entry["min_price"] = int(min_price)
    if accept_above is not None:
        entry["accept_above"] = int(accept_above)
    if auto_sell is not None:
        entry["auto_sell"] = auto_sell
    if raid is not None:
        entry["raid"] = raid
    if max_pay is not None:
        entry["max_pay"] = int(max_pay)
    policies[str(player_id)] = entry
    save(policies)
    return entry


def remove(player_id: str) -> bool:
    policies = load()
    if str(player_id) not in policies:
        return False
    policies.pop(str(player_id))
    save(policies)
    return True


def raid_plan(players: list[dict[str, Any]], *, cash: float) -> list[dict[str, Any]]:
    """Scheduled clause raids: pay the moment the lock lifts, if it is still worth it.

    The whole point is to fire the instant the clause opens, but a clause is not a
    fixed price: the owner can raise it, and he can shield the player outright. So
    the instruction carries `max_pay` — above that we stand down rather than
    overpay for something we wanted at yesterday's price.
    """
    policies = load()
    actions: list[dict[str, Any]] = []
    for player in players:
        policy = policies.get(str(player.get("id")))
        if not policy or not policy.get("raid"):
            continue

        clause = float(player.get("clause") or 0)
        ceiling = int(policy.get("max_pay") or 0)
        row = {"player_id": str(player["id"]), "name": player["name"],
               "clause": clause, "max_pay": ceiling, "owner": player.get("owner"),
               "owner_team_id": player.get("owner_team_id")}

        if player.get("is_mine"):
            actions.append({**row, "action": "ninguna", "why": "ya es tuyo"})
        elif not player.get("owner"):
            actions.append({**row, "action": "ninguna", "why": "ya no lo tiene nadie"})
        elif player.get("shielded"):
            actions.append({**row, "action": "bloqueada",
                            "why": f"{player['owner']} lo ha blindado"})
        elif player.get("clause_locked"):
            hours = player.get("clause_hours_left")
            actions.append({**row, "action": "esperando",
                            "why": ("clausula bloqueada"
                                    + (f", se abre en {hours:.0f}h" if hours else ""))})
        elif ceiling and clause > ceiling:
            actions.append({**row, "action": "cancelada",
                            "why": (f"la clausula subio a {_money(clause)}, "
                                    f"tu limite es {_money(ceiling)}")})
        elif clause > cash:
            actions.append({**row, "action": "sin_saldo",
                            "why": (f"cuesta {_money(clause)} "
                                    f"y tienes {_money(cash)}")})
        else:
            actions.append({**row, "action": "pagar_clausula", "amount": int(clause),
                            "why": (f"abierta a {_money(clause)}, "
                                    f"por debajo de tu limite de {_money(ceiling)}")})
    return actions


# What counts as a good offer, when you would rather not pick a number.
#
# Three references, and the bar is the highest of them, because each one catches a
# different way of being underpaid:
#   * your asking price — you already decided what he is worth to you;
#   * 1.02x his market value — the app's own "buen precio" band, and above the ceiling
#     of the daily automatic offers, which top out around 1.05x but usually sit below par;
#   * futbolfantasy's maximum profitable bid — if somebody pays more than the player can
#     return at that price, selling is the winning side of the trade.
# Whichever is highest is the one quoted in the reason, so the number is never a mystery.
GOOD_OVER_VALUE = 1.02


def good_offer_floor(player: dict[str, Any], policy: dict[str, Any]) -> tuple[int, str]:
    """(bar, which reference set it)."""
    value = float(player.get("value") or 0)
    listing = player.get("market") or {}
    candidates = [
        (int(listing.get("min_bid") or 0), "lo que pides"),
        (int(value * GOOD_OVER_VALUE) if value else 0, "un 2% sobre su valor"),
        (int(player.get("ideal_bid") or 0), "la puja maxima rentable de futbolfantasy"),
        (int(policy.get("min_price") or 0), "tu precio minimo"),
    ]
    bar, source = max(candidates, key=lambda item: item[0])
    return bar, source


def squad_room(players: list[dict[str, Any]], position_id: Any) -> int:
    """How many of that position could be sold before the squad stops being legal.

    A price can be excellent and the sale still be a mistake: eleven legal starters
    need a goalkeeper, three defenders and three midfielders, and an automatic sale
    that leaves you unable to field a team is not a good deal at any price.
    """
    mine = [p for p in players if p.get("is_mine")]
    have = sum(1 for p in mine if p.get("position_id") == position_id)
    return have - MIN_PER_POSITION.get(position_id, 0)


def plan(players: list[dict[str, Any]]) -> list[dict[str, Any]]:
    """What the standing instructions would do right now, without doing it.

    Returned whether or not writes are enabled, so the page can always show what
    *would* happen — the plan is the useful part even when nothing executes.
    """
    policies = load()
    actions: list[dict[str, Any]] = []
    for player in players:
        policy = policies.get(str(player.get("id")))
        if not policy or not policy.get("always_listed"):
            continue
        if not player.get("is_mine"):
            actions.append({"player_id": str(player["id"]), "name": player["name"],
                            "action": "ninguna", "why": "ya no es tuyo"})
            continue

        value = float(player.get("value") or 0)
        floor = int(policy.get("min_price") or value)
        listing = player.get("market") or {}
        offers = player.get("offers") or []
        best = offers[0] if offers else None
        asking = int(listing.get("min_bid") or floor or value)

        # Two ways to authorise a sale, and no third. An explicit `accept_above` is a
        # number you chose; `auto_sell` is the check, and it means "any offer that is
        # good", with `good_offer_floor` deciding what good means. Without either,
        # nothing is ever sold.
        threshold = policy.get("accept_above")
        threshold = int(threshold) if threshold else None
        source = "tu limite"
        if threshold is None and policy.get("auto_sell"):
            threshold, source = good_offer_floor(player, policy)

        room = squad_room(players, player.get("position_id"))
        if (threshold is not None and best and float(best.get("money") or 0) >= threshold
                and room <= 0):
            # Good price, bad idea: this is the last one at his position.
            actions.append({
                "player_id": str(player["id"]), "name": player["name"],
                "action": "avisar", "amount": int(best["money"]),
                "offer_id": str(best.get("id")), "market_id": listing.get("market_id"),
                "why": (f"ofrecen {_money(best['money'])} "
                        f"(por encima de {_money(threshold)}), pero es tu ultimo "
                        f"{player.get('position') or 'jugador'}"
                        " y te quedarias sin alineacion legal")})
            continue

        if threshold is not None and best and float(best.get("money") or 0) >= threshold:
            actions.append({
                "player_id": str(player["id"]), "name": player["name"],
                "action": "aceptar_oferta", "amount": int(best["money"]),
                "offer_id": str(best.get("id")), "market_id": listing.get("market_id"),
                "why": (f"ofrecen {_money(best['money'])}, "
                        f"{source} es {_money(threshold)}")})
        elif not listing:
            price = max(floor, int(value))
            actions.append({
                "player_id": str(player["id"]), "name": player["name"],
                "action": "poner_en_venta", "amount": price,
                "why": f"no esta en el mercado; lo listo a {_money(price)}"})
        else:
            listed_at = int(listing.get("min_bid") or 0)
            why = (f"ya listado a {_money(listed_at)}"
                   + (f", mejor oferta {_money(best['money'])}" if best else ", sin ofertas"))
            # An offer that already covers the asking price, on a player nobody
            # authorised selling: worth saying out loud rather than leaving in a table
            # nobody reads. It is a notice, not an action — enforce skips it.
            if (best and threshold is None and listed_at
                    and float(best.get("money") or 0) >= listed_at):
                actions.append({
                    "player_id": str(player["id"]), "name": player["name"],
                    "action": "avisar", "amount": int(best["money"]),
                    "offer_id": str(best.get("id")), "market_id": listing.get("market_id"),
                    "why": (f"ofrecen {_money(best['money'])}, "
                            f"lo que pides ({_money(listed_at)}); "
                            "no vendo solo, decides tu")})
                continue
            if threshold is None:
                why += "; no vendo solo"
            actions.append({"player_id": str(player["id"]), "name": player["name"],
                            "action": "ninguna", "why": why})
    return actions


def verify_clause(league_id: str, action: dict[str, Any]) -> tuple[bool, str]:
    """Re-read the clause from the owner's squad, right before paying it.

    The plan is built from data that can be half an hour old, and this is the one
    operation where that gap is expensive: the owner can raise the clause or shield
    the player at any moment, and paying is irreversible. One request, at the only
    instant it matters.
    """
    from . import laliga

    team_id = action.get("owner_team_id")
    if not team_id:
        return False, "no se sabe de quien es ahora mismo"
    try:
        squad = laliga.team_squad(league_id, str(team_id), ttl=0)
    except Exception as exc:
        return False, f"no se ha podido comprobar la clausula ({type(exc).__name__})"

    for slot in laliga.squad_players(squad):
        if str(((slot.get("playerMaster") or {}).get("id")) or "") != action["player_id"]:
            continue
        if slot.get("isShielded"):
            return False, "esta blindado"
        unlock = slot.get("buyoutClauseLockedEndTime")
        if unlock:
            from datetime import datetime, timezone
            try:
                when = datetime.fromisoformat(str(unlock).replace("Z", "+00:00"))
                if when.tzinfo is None:
                    when = when.replace(tzinfo=timezone.utc)
                if when > datetime.now(timezone.utc):
                    return False, "la clausula sigue bloqueada"
            except ValueError:
                pass
        now_clause = float(slot.get("buyoutClause") or 0)
        ceiling = int(action.get("max_pay") or 0)
        if ceiling and now_clause > ceiling:
            return False, (f"la clausula esta en {_money(now_clause)}, "
                           f"por encima de tu limite de {_money(ceiling)}")
        action["clause"] = now_clause
        action["amount"] = int(now_clause)
        return True, f"clausula confirmada en {_money(now_clause)}"
    return False, "ya no esta en esa plantilla"


def enforce(players: list[dict[str, Any]], *, league_id: str, my_team_id: str,
            allow_writes: bool, cash: float = 0) -> list[dict[str, Any]]:
    """Carry out both plans. A no-op in read-only mode."""
    from . import writes

    actions = plan(players) + raid_plan(players, cash=cash)
    if not allow_writes:
        return actions
    for action in actions:
        if action["action"] == "ninguna":
            continue
        if action["action"] in ("bloqueada", "esperando", "cancelada", "sin_saldo",
                                "avisar"):
            continue
        operation = {"aceptar_oferta": "accept_offer",
                     "poner_en_venta": "sell_to_market",
                     "pagar_clausula": "pay_clause"}.get(action["action"])
        if not operation:
            continue
        if operation == "pay_clause":
            ok, why = verify_clause(league_id, action)
            log.info("clause verified", extra={"player": action["name"], "ok": ok,
                                              "reason": why})
            if not ok:
                action["action"] = "cancelada"
                action["why"] = why
                continue
        try:
            summary = writes.prepare(
                operation, league_id=league_id, my_team_id=my_team_id,
                amount=action.get("amount"), market_id=action.get("market_id"),
                offer_id=action.get("offer_id"), player_id=action["player_id"],
                player={"name": action["name"]}, allow_writes=True)
            result = writes.confirm(summary["token"], allow_writes=True)
        except Exception as exc:
            action["result"] = f"fallo: {exc}"
            log.error("policy action failed", extra={"player": action["name"],
                                                    "action": action["action"],
                                                    "reason": str(exc)[:200]})
            continue
        action["result"] = "hecho"
        log.info("policy action done", extra={"player": action["name"],
                                             "action": action["action"],
                                             "amount": action.get("amount"),
                                             "ok": bool(result)})
    return actions
