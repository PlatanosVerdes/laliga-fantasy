"""Starred players, kept in a small JSON file next to the session."""
from __future__ import annotations

import json
from typing import Any

from .config import CONFIG_DIR, FAVOURITES_FILE


def load() -> dict[str, dict[str, Any]]:
    if not FAVOURITES_FILE.exists():
        return {}
    try:
        raw = json.loads(FAVOURITES_FILE.read_text())
    except json.JSONDecodeError:
        return {}
    # Older runs stored a bare list of ids; keep those readable.
    if isinstance(raw, list):
        return {str(pid): {"id": str(pid)} for pid in raw}
    return {str(k): v for k, v in raw.items()} if isinstance(raw, dict) else {}


def save(entries: dict[str, dict[str, Any]]) -> None:
    CONFIG_DIR.mkdir(parents=True, exist_ok=True)
    FAVOURITES_FILE.write_text(json.dumps(entries, indent=2, ensure_ascii=False))


def ids() -> set[str]:
    return set(load())


def add(player_id: str, name: str | None = None, note: str | None = None) -> bool:
    entries = load()
    key = str(player_id)
    existed = key in entries
    entries[key] = {"id": key, "name": name or entries.get(key, {}).get("name"),
                    "note": note or entries.get(key, {}).get("note")}
    save(entries)
    return not existed


def remove(player_id: str) -> bool:
    entries = load()
    if str(player_id) not in entries:
        return False
    entries.pop(str(player_id))
    save(entries)
    return True


def toggle(player_id: str, name: str | None = None) -> bool:
    """Returns True when the player ends up starred."""
    if str(player_id) in load():
        remove(player_id)
        return False
    add(player_id, name)
    return True
