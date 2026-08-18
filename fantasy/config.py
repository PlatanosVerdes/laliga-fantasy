"""Paths, constants and endpoint templates."""
from __future__ import annotations

import os
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
DATA_DIR = Path(os.environ.get("FANTASY_DATA_DIR", ROOT / "data"))
CACHE_DIR = DATA_DIR / "cache"
TOKEN_FILE = DATA_DIR / "tokens.json"
SETTINGS_FILE = DATA_DIR / "settings.json"
REPORT_FILE = DATA_DIR / "report.html"

# --- LaLiga Fantasy API (season 26/27 host; the old api-fantasy host is frozen)
API_BASE = "https://fantasy-api.llt-services.com/api"
COMPETITION_ID = os.environ.get("FANTASY_COMPETITION_ID", "1")
CMP = f"/v1/competition/{COMPETITION_ID}"

API_HEADERS = {
    "x-app": "2",
    "x-lang": "es",
    "Referer": "https://fantasy.laliga.com/",
    "User-Agent": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) "
                  "AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0 Safari/537.36",
}

# --- LaLiga Azure AD B2C
B2C_TOKEN_URL = "https://login.laliga.es/laligadspprob2c.onmicrosoft.com/oauth2/v2.0/token"
B2C_SIGNIN_POLICY = "B2C_1A_5ULAIP_PARAMETRIZED_SIGNIN"
B2C_ROPC_POLICY = "B2C_1A_ResourceOwnerv2"
B2C_WEB_CLIENT_ID = "6457fa17-1224-416a-b21a-ee6ce76e9bc0"
B2C_NATIVE_CLIENT_ID = "af88bcff-1157-40a0-b579-030728aacf0b"

# --- futbolfantasy.com
FF_BASE = "https://www.futbolfantasy.com"
FF_MARKET_URL = f"{FF_BASE}/analytics/laliga-fantasy/mercado"
FF_DETAIL_URL = f"{FF_BASE}/analytics/laliga-fantasy/mercado/detalle/{{ff_id}}?perfil=1"
FF_PLAYER_URL = f"{FF_BASE}/jugadores/{{slug}}"
FF_INJURED_URL = f"{FF_BASE}/laliga/lesionados"
FF_SUSPENDED_URL = f"{FF_BASE}/laliga/sancionados"

FF_HEADERS = {
    "User-Agent": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) "
                  "AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0 Safari/537.36",
    "Accept": "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
    "Accept-Language": "es-ES,es;q=0.9",
}

POSITIONS = {1: "POR", 2: "DEF", 3: "MED", 4: "DEL", 5: "ENT"}
POSITION_NAMES = {1: "Portero", 2: "Defensa", 3: "Centrocampista", 4: "Delantero", 5: "Entrenador"}

# futbolfantasy shows short team names; LaLiga's teams-master uses official ones.
FF_TEAM_ALIASES = {
    "alaves": "deportivo alaves",
    "athletic": "athletic club",
    "atletico": "atletico de madrid",
    "barcelona": "fc barcelona",
    "betis": "real betis",
    "celta": "celta",
    "deportivo": "rc deportivo",
    "elche": "elche cf",
    "espanyol": "rcd espanyol",
    "getafe": "getafe cf",
    "girona": "girona fc",
    "levante": "levante ud",
    "malaga": "malaga cf",
    "mallorca": "rcd mallorca",
    "osasuna": "ca osasuna",
    "oviedo": "real oviedo",
    "racing": "r racing club",
    "rayo": "rayo vallecano",
    "real madrid": "real madrid",
    "real sociedad": "real sociedad",
    "sevilla": "sevilla fc",
    "valencia": "valencia cf",
    "villarreal": "villarreal cf",
}

# Squad rules of LaLiga Fantasy: 11 starters within a legal formation.
MIN_PER_POSITION = {1: 1, 2: 3, 3: 3, 4: 1}
IDEAL_PER_POSITION = {1: 2, 2: 6, 3: 6, 4: 4}
WEEKS_IN_SEASON = 38


def ensure_dirs() -> None:
    CACHE_DIR.mkdir(parents=True, exist_ok=True)
