"""The ${{ values.name }} service (what it does: README.md).

What its chart (deploy/chart) expects: HTTP on PORT (8080) with /healthz and /readyz, Prometheus
metrics on :9090/metrics, clean shutdown on SIGTERM (uvicorn handles it).
"""

import os
from contextlib import asynccontextmanager

from fastapi import FastAPI, Request
from prometheus_client import Counter, start_http_server

SERVICE = "${{ values.name }}"
VERSION = os.environ.get("VERSION", "dev")

REQUESTS = Counter(
    f"{SERVICE.replace('-', '_')}_http_requests_total",
    "HTTP requests served.",
    ["method", "status"],
)


@asynccontextmanager
async def lifespan(_: FastAPI):
    # Metrics on their own port, so the gateway never exposes them.
    if os.environ.get("METRICS_PORT", "9090") != "off":
        start_http_server(int(os.environ.get("METRICS_PORT", "9090")))
    yield


app = FastAPI(title=SERVICE, version=VERSION, lifespan=lifespan, docs_url=None, redoc_url=None)


@app.middleware("http")
async def count_requests(request: Request, call_next):
    response = await call_next(request)
    REQUESTS.labels(request.method, str(response.status_code)).inc()
    response.headers["X-Content-Type-Options"] = "nosniff"
    return response


@app.get("/healthz")
def healthz() -> dict[str, str]:
    return {"status": "ok"}


@app.get("/readyz")
def readyz() -> dict[str, str]:
    return {"status": "ok"}


@app.get("/")
def root() -> dict[str, str]:
    return {"service": SERVICE, "version": VERSION}
