"""Operations that change something in the game.

Every write goes through `prepare()` then `confirm()`: prepare validates the amount
against your cash and futbolfantasy's ceiling and hands back a single-use token;
confirm spends that token and calls the API. A double click, a retry or a replayed
request therefore cannot bid twice.

Writes are refused unless the caller enabled them explicitly (`serve --allow-writes`),
so the copy running on the homeserver cannot spend money by accident.
"""
from __future__ import annotations

import json
import secrets
import threading
import time
from typing import Any

from . import laliga
from .config import API_BASE, API_HEADERS, CMP
from . import auth, http
from .logs import log

PREPARE_TTL = 120
_pending: dict[str, dict[str, Any]] = {}
_lock = threading.Lock()


def _money(amount: float) -> str:
    return f"{amount:,.0f}".replace(",", ".")


class WriteError(Exception):
    pass


class WritesDisabled(WriteError):
    pass


def _request(method: str, path: str, body: dict | None = None) -> Any:
    headers = dict(API_HEADERS)
    headers["Authorization"] = f"Bearer {auth.bearer()}"
    data = None
    if body is not None:
        headers["Content-Type"] = "application/json"
        data = json.dumps(body).encode()
    sep = "&" if "?" in path else "?"
    url = f"{API_BASE}{path}{sep}x-lang=es"
    raw = http.fetch(url, headers=headers, method=method, data=data, retries=1, ttl=0)
    return json.loads(raw) if raw.strip() else None


# --- the operations ---------------------------------------------------------

def _bid(league_id: str, market_id: str, amount: int) -> Any:
    return _request("POST", f"{CMP}/league/{league_id}/market/{market_id}/bid",
                    {"money": amount})


def _modify_bid(league_id: str, market_id: str, bid_id: str, amount: int) -> Any:
    return _request("PUT", f"{CMP}/league/{league_id}/market/{market_id}/bid/{bid_id}",
                    {"money": amount})


def _cancel_bid(league_id: str, market_id: str, bid_id: str) -> Any:
    return _request("DELETE",
                    f"{CMP}/league/{league_id}/market/{market_id}/bid/{bid_id}/cancel")


def _raise_clause(league_id: str, player_id: str, amount: int) -> Any:
    return _request("PUT", f"{CMP}/league/{league_id}/buyout/player",
                    {"playerId": player_id, "factor": 1, "valueToIncrease": amount})


def _accept_offer(league_id: str, market_id: str, offer_id: str, amount: int) -> Any:
    # The API demands the amount in the body as a confirmation of what is being
    # accepted; without it, it answers 400.
    return _request("POST",
                    f"{CMP}/league/{league_id}/market/{market_id}/offer/{offer_id}/accept",
                    {"offerMoney": amount})


def _decline_offer(league_id: str, market_id: str, offer_id: str) -> Any:
    return _request("POST",
                    f"{CMP}/league/{league_id}/market/{market_id}/offer/{offer_id}/reject")


def _sell_to_market(league_id: str, player_id: str, amount: int) -> Any:
    return _request("POST", f"{CMP}/league/{league_id}/market/sell",
                    {"playerId": player_id, "salePrice": amount})


def _direct_offer(league_id: str, player_id: str, amount: int) -> Any:
    return _request("POST", f"{CMP}/league/{league_id}/market/direct-offer",
                    {"playerId": player_id, "money": amount})


def _pay_clause(league_id: str, player_id: str, amount: int) -> Any:
    return _request("POST", f"{CMP}/league/{league_id}/buyout/{player_id}/pay",
                    {"buyoutClauseToPay": amount})


def _withdraw(league_id: str, market_id: str) -> Any:
    return _request("DELETE", f"{CMP}/league/{league_id}/market/{market_id}/delete")


OPERATIONS = {
    "direct_offer": {"run": _direct_offer, "label": "oferta directa"},
    "pay_clause": {"run": _pay_clause, "label": "pagar clausula"},
    "accept_offer": {"run": _accept_offer, "label": "aceptar oferta"},
    "decline_offer": {"run": _decline_offer, "label": "rechazar oferta"},
    "sell_to_market": {"run": _sell_to_market, "label": "poner en venta"},
    "withdraw": {"run": _withdraw, "label": "retirar del mercado"},
    "bid": {"run": _bid, "label": "puja"},
    "modify_bid": {"run": _modify_bid, "label": "modificar puja"},
    "cancel_bid": {"run": _cancel_bid, "label": "cancelar puja"},
    "raise_clause": {"run": _raise_clause, "label": "subir clausula"},
}


# --- two-step guard ---------------------------------------------------------

def _purge() -> None:
    now = time.time()
    for token in [t for t, p in _pending.items() if now - p["created"] > PREPARE_TTL]:
        _pending.pop(token, None)


def prepare(operation: str, *, league_id: str, my_team_id: str, amount: int | None = None,
            market_id: str | None = None, bid_id: str | None = None,
            offer_id: str | None = None, player_id: str | None = None,
            player: dict[str, Any] | None = None, allow_writes: bool = False) -> dict[str, Any]:
    """Validate an operation and return a summary plus a single-use token."""
    if not allow_writes:
        raise WritesDisabled("la escritura esta desactivada: arranca con --allow-writes")
    if operation not in OPERATIONS:
        raise WriteError(f"operacion desconocida: {operation}")

    player = player or {}
    cash = None
    try:
        cash = int(laliga.team_money(my_team_id, ttl=0).get("teamMoney") or 0)
    except Exception as exc:
        log.warning("cash unreadable before write", extra={"error_type": type(exc).__name__})

    warnings: list[str] = []
    if operation == "accept_offer":
        value = float(player.get("value") or 0)
        if value and amount and amount < value * 0.9:
            warnings.append(f"por debajo de su valor de mercado ({_money(value)})")
        ideal = int(player.get("ideal_bid") or 0)
        if ideal and amount and amount > ideal:
            warnings.append("te pagan mas de lo que futbolfantasy considera rentable: buena venta")
    if operation in ("direct_offer", "pay_clause"):
        if not amount or amount <= 0:
            raise WriteError("el importe tiene que ser positivo")
        if cash is not None and amount > cash:
            raise WriteError(f"no te llega: tienes {_money(cash)}")
        if operation == "pay_clause":
            clause = int(player.get("clause") or 0)
            if clause and amount < clause:
                raise WriteError(f"la clausula es {_money(clause)}")
    if operation in ("bid", "modify_bid"):
        if not amount or amount <= 0:
            raise WriteError("la puja tiene que ser un importe positivo")
        floor = int(player.get("min_bid") or 0)
        if floor and amount < floor:
            raise WriteError(f"la puja minima es {floor:,}".replace(",", "."))
        if cash is not None and amount > cash:
            raise WriteError(f"no te llega: tienes {cash:,}".replace(",", "."))
        ideal = int(player.get("ideal_bid") or 0)
        if ideal and amount > ideal:
            warnings.append(f"por encima de la puja maxima rentable de futbolfantasy "
                            f"({ideal:,})".replace(",", "."))
        elif not ideal:
            warnings.append("futbolfantasy no le ve rentabilidad a este precio")
        if cash is not None and amount > 0.5 * cash:
            warnings.append("te deja con menos de la mitad del saldo")

    _purge()
    token = secrets.token_urlsafe(18)
    summary = {
        "token": token,
        "operation": operation,
        "label": OPERATIONS[operation]["label"],
        "player_name": player.get("name"),
        "amount": amount,
        "min_bid": player.get("min_bid"),
        "ideal_bid": player.get("ideal_bid"),
        "market_value": player.get("value"),
        "cash_before": cash,
        "cash_after": (cash - amount) if (cash is not None and amount and operation in
                                         ("bid", "modify_bid", "direct_offer", "pay_clause"))
                      else ((cash + amount) if (cash is not None and amount
                                               and operation == "accept_offer") else cash),
        "warnings": warnings,
        "expires_in": PREPARE_TTL,
    }
    with _lock:
        _pending[token] = {"created": time.time(), "operation": operation,
                           "args": {"league_id": league_id, "market_id": market_id,
                                    "bid_id": bid_id, "offer_id": offer_id,
                                    "player_id": player_id, "amount": amount},
                           "summary": summary}
    log.info("write prepared", extra={"operation": operation, "amount": amount,
                                     "player": player.get("name")})
    return summary


def confirm(token: str, *, allow_writes: bool = False, dry_run: bool = False) -> dict[str, Any]:
    """Spend the token and perform the call. The token is consumed either way."""
    if not allow_writes:
        raise WritesDisabled("la escritura esta desactivada")
    _purge()
    with _lock:
        pending = _pending.pop(token, None)
    if not pending:
        raise WriteError("confirmacion caducada o ya usada: vuelve a empezar")

    operation = pending["operation"]
    args = pending["args"]
    runner = OPERATIONS[operation]["run"]
    call = {"bid": ("league_id", "market_id", "amount"),
            "modify_bid": ("league_id", "market_id", "bid_id", "amount"),
            "cancel_bid": ("league_id", "market_id", "bid_id"),
            "raise_clause": ("league_id", "player_id", "amount"),
            "accept_offer": ("league_id", "market_id", "offer_id", "amount"),
            "decline_offer": ("league_id", "market_id", "offer_id"),
            "sell_to_market": ("league_id", "player_id", "amount"),
            "direct_offer": ("league_id", "player_id", "amount"),
            "pay_clause": ("league_id", "player_id", "amount"),
            "withdraw": ("league_id", "market_id")}[operation]
    values = [args.get(name) for name in call]

    if dry_run:
        log.info("write dry-run", extra={"operation": operation, "args": values})
        return {"ok": True, "dry_run": True, "operation": operation,
                "summary": pending["summary"]}
    try:
        result = runner(*values)
    except http.HttpError as exc:
        log.error("write failed", extra={"operation": operation, "status": exc.status})
        raise WriteError(f"la API ha respondido {exc.status}: {exc.body[:200]}") from exc
    log.info("write done", extra={"operation": operation, "amount": args.get("amount")})
    return {"ok": True, "operation": operation, "summary": pending["summary"],
            "response": result}
