"""Standing instructions per player: keep him listed, and take a good enough offer.

Listings expire and get pulled; if you want somebody permanently on the market you
have to relist him by hand every day. This is that chore, automated — but only for
players you name explicitly and never below a floor you set — and only when the
server was started with `--auto`. Without it the plan is computed and shown but
nothing executes, because a robot spending your money while you are not watching is
a different decision from clicking a button and confirming it twice.

Nothing here decides on its own what a good price is: `min_price` and
`accept_above` come from you. The default for both is the player's market value,
which is the one number that is not an opinion.
"""
from __future__ import annotations

import json
from typing import Any

from .config import DATA_DIR
from .logs import log

POLICY_FILE = DATA_DIR / "policies.json"


def load() -> dict[str, dict[str, Any]]:
    if not POLICY_FILE.exists():
        return {}
    try:
        data = json.loads(POLICY_FILE.read_text())
    except json.JSONDecodeError:
        return {}
    return {str(k): v for k, v in data.items()} if isinstance(data, dict) else {}


def save(policies: dict[str, dict[str, Any]]) -> None:
    DATA_DIR.mkdir(parents=True, exist_ok=True)
    POLICY_FILE.write_text(json.dumps(policies, indent=2, ensure_ascii=False))


def set_policy(player_id: str, *, name: str | None = None, always_listed: bool | None = None,
               min_price: int | None = None, accept_above: int | None = None,
               raid: bool | None = None, max_pay: int | None = None) -> dict[str, Any]:
    policies = load()
    entry = {**policies.get(str(player_id), {}), "id": str(player_id)}
    if name:
        entry["name"] = name
    if always_listed is not None:
        entry["always_listed"] = always_listed
    if min_price is not None:
        entry["min_price"] = int(min_price)
    if accept_above is not None:
        entry["accept_above"] = int(accept_above)
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
               "clause": clause, "max_pay": ceiling, "owner": player.get("owner")}

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
                            "why": f"la clausula subio a {int(clause):,}".replace(",", ".")
                                   + f", tu limite es {ceiling:,}".replace(",", ".")})
        elif clause > cash:
            actions.append({**row, "action": "sin_saldo",
                            "why": f"cuesta {int(clause):,}".replace(",", ".")
                                   + f" y tienes {int(cash):,}".replace(",", ".")})
        else:
            actions.append({**row, "action": "pagar_clausula", "amount": int(clause),
                            "why": f"abierta a {int(clause):,}".replace(",", ".")
                                   + f", por debajo de tu limite de {ceiling:,}"
                                     .replace(",", ".")})
    return actions


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
        threshold = int(policy.get("accept_above") or value)
        listing = player.get("market") or {}
        offers = player.get("offers") or []
        best = offers[0] if offers else None

        if best and float(best.get("money") or 0) >= threshold:
            actions.append({
                "player_id": str(player["id"]), "name": player["name"],
                "action": "aceptar_oferta", "amount": int(best["money"]),
                "offer_id": str(best.get("id")), "market_id": listing.get("market_id"),
                "why": f"ofrecen {int(best['money']):,}".replace(",", ".")
                       + f", tu limite es {threshold:,}".replace(",", ".")})
        elif not listing:
            price = max(floor, int(value))
            actions.append({
                "player_id": str(player["id"]), "name": player["name"],
                "action": "poner_en_venta", "amount": price,
                "why": f"no esta en el mercado; lo listo a {price:,}".replace(",", ".")})
        else:
            actions.append({"player_id": str(player["id"]), "name": player["name"],
                            "action": "ninguna",
                            "why": f"ya listado a {int(listing.get('min_bid') or 0):,}"
                                   .replace(",", ".")
                                   + (f", mejor oferta {int(best['money']):,}".replace(",", ".")
                                      if best else ", sin ofertas")})
    return actions


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
        if action["action"] in ("bloqueada", "esperando", "cancelada", "sin_saldo"):
            continue
        operation = {"aceptar_oferta": "accept_offer",
                     "poner_en_venta": "sell_to_market",
                     "pagar_clausula": "pay_clause"}.get(action["action"])
        if not operation:
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
