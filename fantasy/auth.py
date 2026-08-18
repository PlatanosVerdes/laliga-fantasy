"""Token storage and renewal against LaLiga's Azure AD B2C tenant.

Social logins (Google/Apple/Facebook) cannot be automated headlessly, so the
supported path is: grab the tokens once from the browser session, store them
here, and keep the session alive with the refresh token.
"""
from __future__ import annotations

import base64
import hashlib
import json
import os
import re
import secrets
import time
import urllib.parse
from typing import Any

from . import http
from .logs import log
from .config import (
    B2C_NATIVE_CLIENT_ID,
    B2C_ROPC_POLICY,
    B2C_SIGNIN_POLICY,
    B2C_TOKEN_URL,
    B2C_WEB_CLIENT_ID,
    CONFIG_DIR,
    TOKEN_FILE,
)

NATIVE_REDIRECT_URI = "authredirect://com.lfp.laligafantasy"
AUTHORIZE_URL = B2C_TOKEN_URL.replace("/token", "/authorize")

BROWSER_SNIPPET = r"""
/* 1. Abre https://fantasy.laliga.com/ y entra con tu cuenta (Google/Apple/...).
   2. Abre la consola del navegador (Cmd+Opt+J) y pega TODO esto.
   3. Se copia un JSON al portapapeles: pasalo a `python3 fantasy.py auth paste`. */
(() => {
  const out = {};
  const scan = (store, where) => {
    for (let i = 0; i < store.length; i++) {
      const k = store.key(i);
      let v = store.getItem(k);
      if (!v) continue;
      const seen = new Set();
      const decode = (part) => {
        let b64 = part.replace(/-/g, '+').replace(/_/g, '/');
        while (b64.length % 4) b64 += '=';
        return JSON.parse(decodeURIComponent(escape(atob(b64))));
      };
      const walk = (node) => {
        if (!node || seen.has(node)) return;
        if (typeof node === 'string') {
          if (/^ey[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\./.test(node)) {
            try {
              const p = decode(node.split('.')[1]);
              if (p.exp) out.candidates = [...(out.candidates || []),
                { token: node, exp: p.exp, aud: p.aud, iss: p.iss, key: k, where }];
            } catch (e) {}
          } else if (node.length > 40 && /^[A-Za-z0-9_.\-]+$/.test(node) && k.toLowerCase().includes('refresh')) {
            out.refresh_token = node;
          }
          return;
        }
        if (typeof node !== 'object') return;
        seen.add(node);
        for (const [key, val] of Object.entries(node)) {
          if (/refresh_?token/i.test(key) && typeof val === 'string' && val.length > 40) out.refresh_token = val;
          if (/client_?id/i.test(key) && typeof val === 'string' && val.length === 36) out.client_id = val;
          walk(val);
        }
      };
      try { walk(JSON.parse(v)); } catch (e) { walk(v); }
    }
  };
  scan(localStorage, 'local');
  scan(sessionStorage, 'session');
  const cands = (out.candidates || []).filter(c => /laliga/i.test(c.iss || ''));
  const best = (cands.length ? cands : out.candidates || []).sort((a, b) => b.exp - a.exp)[0];
  if (!best) { console.error('No se encontro ningun JWT. Asegurate de haber iniciado sesion.'); return; }
  const payload = {
    access_token: best.token,
    refresh_token: out.refresh_token || null,
    client_id: out.client_id || best.aud || null,
    expires_on: best.exp,
  };
  const text = JSON.stringify(payload);
  if (typeof copy === 'function') copy(text);
  else if (navigator.clipboard) navigator.clipboard.writeText(text);
  console.log(text);
  console.log('%cCopiado al portapapeles. Caduca: ' + new Date(best.exp * 1000).toLocaleString(),
              'font-weight:bold');
  if (!payload.refresh_token) console.warn('Sin refresh_token: la sesion durara solo lo que diga exp.');
})();
"""


def _decode_jwt(token: str) -> dict[str, Any]:
    try:
        payload = token.split(".")[1]
        payload += "=" * (-len(payload) % 4)
        return json.loads(base64.urlsafe_b64decode(payload))
    except Exception:
        return {}


def load_tokens() -> dict[str, Any] | None:
    """The stored session, seeding it from the environment on first run.

    The session cannot live in an environment variable, because the refresh token
    rotates: the API can hand back a new one on every renewal and it has to be
    written somewhere. That is why gh, aws, docker and kubectl all keep rotating
    credentials in a file. But a container still needs a way to be handed the
    first one without an interactive login, so FANTASY_TOKENS (the whole JSON) or
    FANTASY_REFRESH_TOKEN seeds the file once and rotation takes over from there.
    """
    if not TOKEN_FILE.exists():
        seeded = _seed_from_env()
        if seeded:
            return seeded
        return None
    try:
        return json.loads(TOKEN_FILE.read_text())
    except json.JSONDecodeError:
        return None


def _seed_from_env() -> dict[str, Any] | None:
    blob = os.environ.get("FANTASY_TOKENS")
    if blob:
        try:
            tokens = normalize(json.loads(blob))
        except (json.JSONDecodeError, TypeError):
            log.error("FANTASY_TOKENS no es un JSON valido")
            return None
        save_tokens(tokens)
        log.info("session seeded from FANTASY_TOKENS")
        return tokens

    refresh_token = os.environ.get("FANTASY_REFRESH_TOKEN")
    if not refresh_token:
        return None
    # Only a refresh token: exchange it right away for a usable access token.
    try:
        tokens = refresh({"refresh_token": refresh_token})
    except RuntimeError as exc:
        log.error("no se ha podido usar FANTASY_REFRESH_TOKEN",
                  extra={"reason": str(exc)[:200]})
        return None
    log.info("session seeded from FANTASY_REFRESH_TOKEN")
    return tokens


def save_tokens(tokens: dict[str, Any]) -> None:
    CONFIG_DIR.mkdir(parents=True, exist_ok=True)
    TOKEN_FILE.write_text(json.dumps(tokens, indent=2))
    os.chmod(TOKEN_FILE, 0o600)


def clear_tokens() -> None:
    TOKEN_FILE.unlink(missing_ok=True)


def normalize(raw: dict[str, Any]) -> dict[str, Any]:
    token = raw.get("access_token") or raw.get("id_token") or ""
    claims = _decode_jwt(token)
    expires_on = raw.get("expires_on")
    if not expires_on:
        expires_on = claims.get("exp") or int(time.time()) + int(raw.get("expires_in") or 86400)
    return {
        "access_token": token,
        "id_token": raw.get("id_token") or token,
        "refresh_token": raw.get("refresh_token"),
        "client_id": raw.get("client_id") or claims.get("aud") or B2C_WEB_CLIENT_ID,
        "expires_on": int(expires_on),
        "email": claims.get("email") or claims.get("unique_name"),
        "name": claims.get("name") or claims.get("given_name"),
        "idp": claims.get("idp"),
    }


def parse_pasted(text: str) -> dict[str, Any]:
    """Accept the JSON blob from BROWSER_SNIPPET, or a bare JWT."""
    text = text.strip()
    if not text:
        raise ValueError("no se recibio nada por stdin")
    if text.startswith("{"):
        return normalize(json.loads(text))
    match = re.search(r"ey[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+", text)
    if not match:
        raise ValueError("no se ha encontrado un JWT ni un JSON valido en la entrada")
    return normalize({"access_token": match.group(0)})


def seconds_left(tokens: dict[str, Any]) -> int:
    return int(tokens.get("expires_on", 0) - time.time())


def refresh(tokens: dict[str, Any]) -> dict[str, Any]:
    refresh_token = tokens.get("refresh_token")
    if not refresh_token:
        raise RuntimeError(
            "no hay refresh_token guardado: vuelve a pegar la sesion "
            "(`python3 fantasy.py auth snippet`)"
        )
    url = f"{B2C_TOKEN_URL}?p={B2C_SIGNIN_POLICY}"
    candidates = [tokens.get("client_id"), B2C_WEB_CLIENT_ID, B2C_NATIVE_CLIENT_ID]
    errors = []
    for client_id in dict.fromkeys(c for c in candidates if c):
        try:
            data = http.post_form(url, {
                "grant_type": "refresh_token",
                "refresh_token": refresh_token,
                "client_id": client_id,
                "scope": "openid offline_access",
            })
        except http.HttpError as exc:
            errors.append(f"{client_id}: {exc.status}")
            continue
        merged = normalize({**data, "client_id": client_id,
                            "refresh_token": data.get("refresh_token") or refresh_token})
        merged["refreshed_at"] = time.time()
        save_tokens(merged)
        log.info("token refreshed", extra={"client_id": client_id,
                                          "expires_in_min": seconds_left(merged) // 60})
        return merged
    raise RuntimeError("el refresh ha fallado con todos los client_id probados: " + ", ".join(errors))


def _b64url(raw: bytes) -> str:
    return base64.urlsafe_b64encode(raw).decode().rstrip("=")


PENDING_FILE = CONFIG_DIR / "pending_auth.json"
PENDING_TTL = 15 * 60


def save_pending(flow: dict[str, Any]) -> None:
    CONFIG_DIR.mkdir(parents=True, exist_ok=True)
    PENDING_FILE.write_text(json.dumps({**flow, "created": int(time.time())}, indent=2))
    os.chmod(PENDING_FILE, 0o600)


def load_pending() -> dict[str, Any]:
    if not PENDING_FILE.exists():
        raise RuntimeError("no hay ningun login empezado: ejecuta `auth browser` primero")
    flow = json.loads(PENDING_FILE.read_text())
    age = int(time.time()) - int(flow.get("created", 0))
    if age > PENDING_TTL:
        raise RuntimeError(f"el login empezado hace {age // 60} min ya ha caducado: "
                           "vuelve a ejecutar `auth browser`")
    return flow


def clear_pending() -> None:
    PENDING_FILE.unlink(missing_ok=True)


def start_browser_login() -> dict[str, str]:
    """Build the authorize URL for the app's own client (auth code + PKCE).

    The mobile app registers the custom scheme `authredirect://com.lfp.laligafantasy`,
    which a desktop browser cannot open — but it still puts the redirect, with the
    authorization code in the query string, in the address bar. That is enough to
    finish the exchange locally, and unlike the web client this flow accepts the
    federated logins (Google/Apple/Facebook) that the app offers.
    """
    verifier = _b64url(os.urandom(40))
    challenge = _b64url(hashlib.sha256(verifier.encode()).digest())
    state = secrets.token_urlsafe(16)
    params = urllib.parse.urlencode({
        "p": B2C_SIGNIN_POLICY,
        "client_id": B2C_NATIVE_CLIENT_ID,
        "response_type": "code",
        "redirect_uri": NATIVE_REDIRECT_URI,
        "scope": "openid offline_access",
        "code_challenge": challenge,
        "code_challenge_method": "S256",
        "state": state,
        "nonce": state,
    })
    return {"url": f"{AUTHORIZE_URL}?{params}", "verifier": verifier, "state": state}


def extract_code(text: str, expected_state: str | None = None) -> str:
    """Pull the authorization code out of a pasted redirect URL (or a bare code)."""
    text = text.strip().strip('"').strip("'")
    if not text:
        raise ValueError("no se ha pegado nada")
    if "code=" not in text and "?" not in text:
        return text

    query = urllib.parse.urlparse(text).query or text.split("?", 1)[-1]
    params = urllib.parse.parse_qs(query)
    if params.get("error"):
        raise ValueError(f"B2C ha devuelto un error: "
                         f"{params.get('error_description', params['error'])[0]}")
    code = (params.get("code") or [None])[0]
    if not code:
        raise ValueError("la URL pegada no contiene ningun `code=`")
    got_state = (params.get("state") or [None])[0]
    if expected_state and got_state and got_state != expected_state:
        raise ValueError("el `state` no coincide: reinicia el login")
    return code


def exchange_code(code: str, verifier: str) -> dict[str, Any]:
    url = f"{B2C_TOKEN_URL}?p={B2C_SIGNIN_POLICY}"
    data = http.post_form(url, {
        "grant_type": "authorization_code",
        "client_id": B2C_NATIVE_CLIENT_ID,
        "code": code,
        "redirect_uri": NATIVE_REDIRECT_URI,
        "code_verifier": verifier,
        "scope": "openid offline_access",
    })
    tokens = normalize({**data, "client_id": B2C_NATIVE_CLIENT_ID})
    save_tokens(tokens)
    return tokens


def login_password(email: str, password: str) -> dict[str, Any]:
    """ROPC flow. Only works for accounts created with email + password."""
    url = f"{B2C_TOKEN_URL}?p={B2C_ROPC_POLICY}"
    data = http.post_form(url, {
        "grant_type": "password",
        "client_id": B2C_NATIVE_CLIENT_ID,
        "scope": f"openid {B2C_NATIVE_CLIENT_ID} offline_access",
        "redirect_uri": "authredirect://com.lfp.laligafantasy",
        "username": email,
        "password": password,
        "response_type": "id_token",
    })
    tokens = normalize({**data, "client_id": B2C_NATIVE_CLIENT_ID})
    save_tokens(tokens)
    return tokens


REFRESH_MARGIN = 120
REFRESH_COOLDOWN = 600


def bearer(*, auto_refresh: bool = True) -> str:
    """Current bearer, refreshing only when the token is genuinely about to die.

    Each refresh is a hit on LaLiga's identity provider and can trigger a security
    notice, so the margin is deliberately tight and a cooldown stops a burst of
    calls from renewing more than once.
    """
    tokens = load_tokens()
    if not tokens:
        raise RuntimeError(
            "sin sesion guardada. Ejecuta `python3 fantasy.py auth browser` y luego "
            "`python3 fantasy.py auth code '<url>'`"
        )
    # Under FANTASY_FREEZE nothing may reach the network, so renewing is both impossible
    # and pointless: every answer comes from the cache. Without this, a snapshot stops
    # being replayable the moment the session inside it expires — which is hours, not days.
    if http.FROZEN:
        return tokens["access_token"]

    left = seconds_left(tokens)
    if auto_refresh and left < REFRESH_MARGIN and tokens.get("refresh_token"):
        since = time.time() - float(tokens.get("refreshed_at") or 0)
        if since >= REFRESH_COOLDOWN:
            try:
                tokens = refresh(tokens)
            except RuntimeError:
                pass
        else:
            log.debug("refresh skipped by cooldown", extra={"seconds_since": round(since)})
    if seconds_left(tokens) < 0:
        raise RuntimeError("el token ha caducado y no se ha podido refrescar; "
                           "vuelve a ejecutar `auth browser`")
    return tokens["access_token"]
