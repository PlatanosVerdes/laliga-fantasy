"""Team crests, downloaded once and embedded as data URIs.

The report is meant to open with no network of its own, so the badges travel
inside the HTML. Twenty crests come to roughly 50 KB, which is cheaper than the
page's own tables.
"""
from __future__ import annotations

import base64
import json
import urllib.error
import urllib.request
from typing import Any, Iterable

from .config import CACHE_DIR, FF_HEADERS, ensure_dirs
from .logs import log

CREST_CACHE = CACHE_DIR / "crests.json"
MAX_BYTES = 40_000


def _load_cache() -> dict[str, str]:
    if not CREST_CACHE.exists():
        return {}
    try:
        return json.loads(CREST_CACHE.read_text())
    except json.JSONDecodeError:
        return {}


def _save_cache(cache: dict[str, str]) -> None:
    ensure_dirs()
    CREST_CACHE.write_text(json.dumps(cache))


def data_uris(teams: Iterable[dict[str, Any]]) -> dict[str, str]:
    """team_id -> `data:image/png;base64,...`, fetching only what is missing."""
    cache = _load_cache()
    fetched = 0
    for team in teams:
        team_id = str(team.get("id"))
        url = team.get("badgeColor") or team.get("badgeWhite")
        if not team_id or not url or team_id in cache:
            continue
        request = urllib.request.Request(url, headers={"User-Agent": FF_HEADERS["User-Agent"]})
        try:
            with urllib.request.urlopen(request, timeout=15) as response:
                raw = response.read(MAX_BYTES + 1)
        except (urllib.error.URLError, OSError, TimeoutError) as exc:
            log.debug("crest unavailable", extra={"team_id": team_id,
                                                 "error_type": type(exc).__name__})
            continue
        if len(raw) > MAX_BYTES:
            log.debug("crest too big", extra={"team_id": team_id, "bytes": len(raw)})
            continue
        cache[team_id] = "data:image/png;base64," + base64.b64encode(raw).decode()
        fetched += 1
    if fetched:
        _save_cache(cache)
        log.info("crests fetched", extra={"fetched": fetched, "total": len(cache)})
    return cache
