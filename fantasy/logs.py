"""Logging: readable on the terminal, JSON lines on disk, optional remote sink.

The JSON file is meant to be shipped as-is. On the Raspberry homeserver Vector
already tails `*.log` files from repo roots into VictoriaLogs, so nothing else is
needed there; set FANTASY_LOG_URL to push straight into VictoriaLogs instead
(`http://<host>:9428/insert/jsonline?_stream_fields=service`).
"""
from __future__ import annotations

import json
import logging
import os
import socket
import sys
import threading
import time
import urllib.error
import urllib.request
from logging.handlers import RotatingFileHandler
from typing import Any

from .config import DATA_DIR

SERVICE = "laliga-fantasy"
LOG_FILE = DATA_DIR / "fantasy.log"
_RESERVED = set(logging.LogRecord("", 0, "", 0, "", (), None).__dict__) | {
    "message", "asctime", "taskName"}

log = logging.getLogger(SERVICE)


class JsonFormatter(logging.Formatter):
    def format(self, record: logging.LogRecord) -> str:
        payload: dict[str, Any] = {
            "_time": time.strftime("%Y-%m-%dT%H:%M:%S", time.localtime(record.created))
                     + f".{int(record.msecs):03d}Z",
            "service": SERVICE,
            "level": record.levelname.lower(),
            "logger": record.name,
            "message": record.getMessage(),
            "host": socket.gethostname(),
        }
        for key, value in record.__dict__.items():
            if key not in _RESERVED and not key.startswith("_"):
                payload[key] = value
        if record.exc_info:
            payload["error"] = self.formatException(record.exc_info)
        return json.dumps(payload, ensure_ascii=False, default=str)


class TerminalFormatter(logging.Formatter):
    COLORS = {"DEBUG": "\033[2m", "INFO": "\033[36m", "WARNING": "\033[33m",
              "ERROR": "\033[31m", "CRITICAL": "\033[41m"}

    def __init__(self, color: bool):
        super().__init__()
        self.color = color

    def format(self, record: logging.LogRecord) -> str:
        extras = " ".join(f"{k}={v}" for k, v in record.__dict__.items()
                          if k not in _RESERVED and not k.startswith("_"))
        text = f"{record.levelname[:4].lower():<5} {record.getMessage()}"
        if extras:
            text += f"  \033[2m{extras}\033[0m" if self.color else f"  {extras}"
        if self.color:
            return f"{self.COLORS.get(record.levelname, '')}{text}\033[0m"
        return text


class RemoteHandler(logging.Handler):
    """Fire-and-forget JSON-lines push. Never blocks or raises into the CLI."""

    def __init__(self, url: str):
        super().__init__()
        self.url = url
        self.setFormatter(JsonFormatter())

    def emit(self, record: logging.LogRecord) -> None:
        body = self.format(record).encode()
        threading.Thread(target=self._send, args=(body,), daemon=True).start()

    def _send(self, body: bytes) -> None:
        request = urllib.request.Request(
            self.url, data=body, method="POST",
            headers={"Content-Type": "application/stream+json"})
        try:
            urllib.request.urlopen(request, timeout=5).close()
        except (urllib.error.URLError, OSError, TimeoutError):
            pass


_configured = False


def setup(*, verbose: bool = False, quiet: bool = False, color: bool = True) -> logging.Logger:
    global _configured
    if _configured:
        return log
    _configured = True

    log.setLevel(logging.DEBUG)
    log.propagate = False

    # In a container the collector reads stdout, so emit JSON there instead of the
    # terminal format: Vector picks it up from the docker source with no extra config.
    as_json = os.environ.get("FANTASY_LOG_JSON", "").lower() in ("1", "true", "yes")
    console = logging.StreamHandler(sys.stdout if as_json else sys.stderr)
    if as_json:
        console.setLevel(logging.DEBUG if verbose else logging.INFO)
        console.setFormatter(JsonFormatter())
    else:
        console.setLevel(logging.ERROR if quiet else
                         (logging.DEBUG if verbose else logging.WARNING))
        console.setFormatter(TerminalFormatter(color))
    log.addHandler(console)

    try:
        DATA_DIR.mkdir(parents=True, exist_ok=True)
        file_handler = RotatingFileHandler(LOG_FILE, maxBytes=5_000_000, backupCount=3,
                                           encoding="utf-8")
        file_handler.setLevel(logging.DEBUG)
        file_handler.setFormatter(JsonFormatter())
        log.addHandler(file_handler)
    except OSError as exc:
        console.handle(log.makeRecord(SERVICE, logging.WARNING, __file__, 0,
                                      "no se ha podido abrir el fichero de log: %s",
                                      (exc,), None))

    remote = os.environ.get("FANTASY_LOG_URL")
    if remote:
        handler = RemoteHandler(remote)
        handler.setLevel(logging.INFO)
        log.addHandler(handler)

    return log


class timed:
    """Context manager that logs how long a block took."""

    def __init__(self, event: str, **fields: Any):
        self.event = event
        self.fields = fields

    def __enter__(self):
        self.start = time.perf_counter()
        return self

    def __exit__(self, exc_type, exc, tb) -> bool:
        ms = round((time.perf_counter() - self.start) * 1000)
        if exc_type:
            log.error(f"{self.event} failed", extra={**self.fields, "ms": ms,
                                                     "error_type": exc_type.__name__})
        else:
            log.info(self.event, extra={**self.fields, "ms": ms})
        return False
