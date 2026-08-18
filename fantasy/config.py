"""Paths, constants and endpoint templates."""
from __future__ import annotations

import os
import shutil
from pathlib import Path

APP_NAME = "laliga-fantasy"
ROOT = Path(__file__).resolve().parent.parent


def _xdg(variable: str, default: str) -> Path:
    """XDG base directory, which is where a CLI's files are supposed to live."""
    base = os.environ.get(variable)
    return Path(base).expanduser() if base else Path.home() / default


EXPLICIT_DIR = bool(os.environ.get("FANTASY_DATA_DIR"))


def _resolve_dirs() -> tuple[Path, Path, Path]:
    """(config, state, cache), honouring one override and the XDG spec.

    FANTASY_DATA_DIR collapses all three into one directory, which is what a
    container wants: a single volume to mount. Without it, files land where the
    rest of the system keeps them, and a legacy ./data next to the code still wins
    if it exists so an existing install keeps working.
    """
    override = os.environ.get("FANTASY_DATA_DIR")
    if override:
        one = Path(override).expanduser()
        return one, one, one / "cache"

    legacy = ROOT / "data"
    if (legacy / "tokens.json").exists() or (legacy / "settings.json").exists():
        return legacy, legacy, legacy / "cache"

    return (_xdg("XDG_CONFIG_HOME", ".config") / APP_NAME,
            _xdg("XDG_STATE_HOME", ".local/state") / APP_NAME,
            _xdg("XDG_CACHE_HOME", ".cache") / APP_NAME)


CONFIG_DIR, STATE_DIR, CACHE_DIR = _resolve_dirs()

# Credentials and preferences: small, edited by us, worth backing up.
TOKEN_FILE = CONFIG_DIR / "tokens.json"
SETTINGS_FILE = CONFIG_DIR / "settings.json"
FAVOURITES_FILE = CONFIG_DIR / "favourites.json"
POLICY_FILE = CONFIG_DIR / "policies.json"
# Output and logs: regenerable, can grow.
REPORT_FILE = STATE_DIR / "report.html"
LOG_FILE = STATE_DIR / "fantasy.log"

# Kept as an alias because plenty of code and the Dockerfile still say "data dir".
DATA_DIR = CONFIG_DIR

MIGRATED = []


def migrate_legacy() -> list[str]:
    """Move files from ./data into the resolved directories, once.

    Silent when there is nothing to move. It exists so upgrading does not cost the
    user a fresh login.

    Never runs under FANTASY_DATA_DIR. An explicit override means "use exactly this
    directory", and a container mounts its own volume: vacuuming the host's ./data
    into it would empty a live session somewhere else.
    """
    legacy = ROOT / "data"
    if EXPLICIT_DIR or not legacy.is_dir() or legacy == CONFIG_DIR:
        return []
    moved = []
    targets = {
        "tokens.json": TOKEN_FILE, "settings.json": SETTINGS_FILE,
        "favourites.json": FAVOURITES_FILE, "policies.json": POLICY_FILE,
        "report.html": REPORT_FILE, "fantasy.log": LOG_FILE,
    }
    for name, target in targets.items():
        source = legacy / name
        if source.exists() and not target.exists():
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.move(str(source), str(target))
            if name == "tokens.json":
                os.chmod(target, 0o600)
            moved.append(name)
    MIGRATED.extend(moved)
    return moved


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
    for directory in (CONFIG_DIR, STATE_DIR, CACHE_DIR):
        directory.mkdir(parents=True, exist_ok=True)
