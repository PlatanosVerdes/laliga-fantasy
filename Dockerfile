# No dependencies to install: the tool is standard library only.
FROM python:3.13-alpine

WORKDIR /app
COPY fantasy.py README.md ./
COPY fantasy/ ./fantasy/

# The session, cache, settings and logs all live here — mount it to persist them.
VOLUME /app/data

ENV PYTHONUNBUFFERED=1 \
    FANTASY_LOG_JSON=1 \
    FANTASY_DATA_DIR=/app/data

EXPOSE 8000

HEALTHCHECK --interval=60s --timeout=10s --start-period=180s --retries=3 \
    CMD python3 -c "import urllib.request,sys; \
sys.exit(0 if urllib.request.urlopen('http://127.0.0.1:8000/healthz', timeout=8).status==200 else 1)"

ENTRYPOINT ["python3", "fantasy.py"]
CMD ["serve", "--port", "8000", "--interval", "3600"]
