"""Dependency-free HTTP client with on-disk TTL cache and small retry loop."""
from __future__ import annotations

import gzip
import hashlib
import json
import time
import urllib.error
import urllib.parse
import urllib.request
from typing import Any, Mapping

from .config import CACHE_DIR, ensure_dirs
from .logs import log


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


def _read_cache(url: str, tag: str, ttl: float):
    if ttl <= 0:
        return None
    path = _cache_path(url, tag)
    if not path.exists() or time.time() - path.stat().st_mtime > ttl:
        return None
    return path.read_text(encoding="utf-8", errors="replace")


def _write_cache(url: str, tag: str, body: str) -> None:
    ensure_dirs()
    _cache_path(url, tag).write_text(body, encoding="utf-8")


def fetch(
    url: str,
    *,
    headers: Mapping[str, str] | None = None,
    method: str = "GET",
    data: bytes | None = None,
    ttl: float = 0,
    tag: str = "raw",
    timeout: float = 30,
    retries: int = 3,
) -> str:
    cached = _read_cache(url, tag, ttl)
    if cached is not None:
        log.debug("http cache hit", extra={"url": url, "tag": tag, "bytes": len(cached)})
        return cached

    last_error: Exception | None = None
    for attempt in range(retries):
        req = urllib.request.Request(url, method=method, data=data)
        for key, value in (headers or {}).items():
            req.add_header(key, value)
        req.add_header("Accept-Encoding", "gzip")
        started = time.perf_counter()
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
            if ttl > 0:
                _write_cache(url, tag, body)
            return body
        except urllib.error.HTTPError as exc:
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
            last_error = HttpError(exc.code, url, body)
        except (urllib.error.URLError, TimeoutError, OSError) as exc:
            log.warning("http failure", extra={"url": url, "tag": tag,
                                               "error_type": type(exc).__name__,
                                               "attempt": attempt + 1})
            last_error = exc
        time.sleep(0.6 * (attempt + 1))

    raise last_error if last_error else RuntimeError(f"unreachable: {url}")


def get_json(url: str, **kwargs) -> Any:
    return json.loads(fetch(url, **kwargs))


def post_form(url: str, form: Mapping[str, str], *, headers: Mapping[str, str] | None = None) -> Any:
    body = urllib.parse.urlencode(form).encode()
    merged = {"Content-Type": "application/x-www-form-urlencoded", **(headers or {})}
    return json.loads(fetch(url, headers=merged, method="POST", data=body, retries=2))
