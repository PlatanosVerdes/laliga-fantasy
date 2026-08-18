"""Dependency-free HTTP client with on-disk TTL cache and small retry loop."""
from __future__ import annotations

import gzip
import hashlib
import json
import os
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from typing import Any, Mapping

from .config import CACHE_DIR, ensure_dirs
from .logs import log


class RateLimited(Exception):
    """429. Carries Retry-After when the server bothered to send one."""

    def __init__(self, status: int, url: str, body: str = "", retry_after: str | None = None):
        super().__init__(f"HTTP {status} on {url} (rate limited)")
        self.status = status
        self.url = url
        self.body = body
        try:
            self.retry_after = float(retry_after) if retry_after else None
        except ValueError:
            self.retry_after = None


class FrozenMiss(Exception):
    """Asked for something the frozen cache does not hold."""

    def __init__(self, url: str, tag: str):
        super().__init__(f"FANTASY_FREEZE: no hay nada en cache para {tag} {url}")
        self.url = url
        self.tag = tag


class HttpError(Exception):
    def __init__(self, status: int, url: str, body: str = ""):
        snippet = " ".join(body.split())[:200] if body.isprintable() or body == "" else ""
        super().__init__(f"HTTP {status} on {url}{': ' + snippet if snippet else ''}")
        self.status = status
        self.url = url
        self.body = body


def _cache_path(url: str, tag: str) -> "Any":
    digest = hashlib.sha1(f"{tag}|{url}".encode()).hexdigest()[:20]
    return CACHE_DIR / f"{tag}_{digest}.cache"


# The differential harness runs both implementations against a frozen cache: TTLs are
# ignored and the network is refused, so a missing entry fails loudly instead of being
# fetched — which would make the two runs read different bytes and compare nothing.
FROZEN = os.environ.get("FANTASY_FREEZE", "").lower() in ("1", "true", "yes")

if FROZEN:
    # Loud on purpose. Frozen mode is for the differential harness: it refuses the network
    # and, because of that, does not renew the session either. Set by accident in a real
    # run it would serve stale data and let the token die quietly, so it says so.
    print("FANTASY_FREEZE activo: no se toca la red, no se renueva la sesion, "
          "todo sale de la cache", file=sys.stderr)


def _read_cache(url: str, tag: str, ttl: float):
    if ttl <= 0 and not FROZEN:
        return None
    path = _cache_path(url, tag)
    if not path.exists():
        return None
    if not FROZEN and time.time() - path.stat().st_mtime > ttl:
        return None
    return path.read_text(encoding="utf-8", errors="replace")


def _write_cache(url: str, tag: str, body: str) -> None:
    ensure_dirs()
    _cache_path(url, tag).write_text(body, encoding="utf-8")


# Requests actually put on the wire, so the refresh policy can be measured instead
# of argued about. Reported by /healthz.
STATS = {"requests": 0, "cache_hits": 0, "errors": 0}


def cached(url: str, tag: str, ttl: float) -> str | None:
    """Cache-only read, so callers can throttle just the real requests."""
    return _read_cache(url, tag, ttl)


def invalidate(*tags: str) -> int:
    """Drop cached responses by tag, so the next read goes to the wire.

    Used when something we just learned makes a long TTL wrong: a transfer means
    every squad is stale, however recently it was read.
    """
    dropped = 0
    for tag in tags:
        for path in CACHE_DIR.glob(f"{tag}_*.cache"):
            try:
                path.unlink()
                dropped += 1
            except OSError:
                pass
    if dropped:
        log.debug("cache invalidated", extra={"tags": list(tags), "files": dropped})
    return dropped


def fetch(
    url: str,
    *,
    headers: Mapping[str, str] | None = None,
    method: str = "GET",
    data: bytes | None = None,
    ttl: float = 0,
    store: bool = False,
    tag: str = "raw",
    timeout: float = 30,
    retries: int = 3,
) -> str:
    """`store` writes the response to cache without reading from it.

    That is what a probe wants: always ask, but leave the answer where the rest of
    the cycle will find it, so checking whether anything moved does not cost the
    same request twice.
    """
    cached = _read_cache(url, tag, ttl)
    if cached is not None:
        STATS["cache_hits"] += 1
        log.debug("http cache hit", extra={"url": url, "tag": tag, "bytes": len(cached)})
        return cached

    if FROZEN:
        raise FrozenMiss(url, tag)

    last_error: Exception | None = None
    for attempt in range(retries):
        req = urllib.request.Request(url, method=method, data=data)
        for key, value in (headers or {}).items():
            req.add_header(key, value)
        req.add_header("Accept-Encoding", "gzip")
        started = time.perf_counter()
        STATS["requests"] += 1
        try:
            with urllib.request.urlopen(req, timeout=timeout) as resp:
                raw = resp.read()
                if resp.headers.get("Content-Encoding") == "gzip":
                    raw = gzip.decompress(raw)
                body = raw.decode("utf-8", errors="replace")
                status = resp.status
            log.debug("http ok", extra={"url": url, "tag": tag, "status": status,
                                        "bytes": len(body),
                                        "ms": round((time.perf_counter() - started) * 1000)})
            if ttl > 0 or store:
                _write_cache(url, tag, body)
            return body
        except urllib.error.HTTPError as exc:
            STATS["errors"] += 1
            raw = exc.read()
            if exc.headers.get("Content-Encoding") == "gzip":
                try:
                    raw = gzip.decompress(raw)
                except (OSError, EOFError):
                    raw = b""
            body = raw.decode("utf-8", errors="replace")
            log.warning("http error", extra={"url": url, "tag": tag, "status": exc.code,
                                             "attempt": attempt + 1})
            if exc.code in (401, 403, 404):
                raise HttpError(exc.code, url, body) from exc
            if exc.code == 429:
                # Being rate limited is an answer, not a glitch: retrying makes it
                # worse. Surface it so the caller can stop for this cycle.
                raise RateLimited(exc.code, url, body,
                                  retry_after=exc.headers.get("Retry-After")) from exc
            last_error = HttpError(exc.code, url, body)
        except (urllib.error.URLError, TimeoutError, OSError) as exc:
            STATS["errors"] += 1
            log.warning("http failure", extra={"url": url, "tag": tag,
                                               "error_type": type(exc).__name__,
                                               "attempt": attempt + 1})
            last_error = exc
        time.sleep(0.6 * (attempt + 1))

    raise last_error if last_error else RuntimeError(f"unreachable: {url}")


def fetch_bytes(url: str, *, headers: Mapping[str, str] | None = None,
                timeout: float = 15, limit: int | None = None) -> bytes:
    """Binary fetch for images, through here rather than around it.

    The crests used to call urllib directly, which meant two things nobody wanted: they
    were invisible to the request counter, and FANTASY_FREEZE did not stop them — so a
    "frozen" run still went to the network and the page it produced was not reproducible.
    """
    if FROZEN:
        raise FrozenMiss(url, "binary")
    STATS["requests"] += 1
    request = urllib.request.Request(url, headers=dict(headers or {}))
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            return response.read(limit + 1 if limit else None)
    except (urllib.error.URLError, OSError, TimeoutError):
        STATS["errors"] += 1
        raise


def get_json(url: str, **kwargs) -> Any:
    return json.loads(fetch(url, **kwargs))


def post_form(url: str, form: Mapping[str, str], *, headers: Mapping[str, str] | None = None) -> Any:
    body = urllib.parse.urlencode(form).encode()
    merged = {"Content-Type": "application/x-www-form-urlencoded", **(headers or {})}
    return json.loads(fetch(url, headers=merged, method="POST", data=body, retries=2))
