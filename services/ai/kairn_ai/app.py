"""Service HTTP ai-service (M-10).

- POST /v1/assistant/chat    flux SSE de l'assistant (appelé par l'API, jeton de service + jeton utilisateur)
- POST /v1/reports/generate  rapport mensuel exécutif (appelé par le worker)
- POST /v1/explain           reformulation d'une explication d'anomalie (appelé par analytics)
- /mcp                       serveur MCP Kairn (jeton d'API du client)
"""

from __future__ import annotations

import hmac
import json
import logging
import os
import queue
import threading
from collections.abc import AsyncIterator, Iterator
from contextlib import asynccontextmanager
from typing import Any
from urllib.parse import urlparse

from fastapi import FastAPI, Header, HTTPException
from fastapi.concurrency import run_in_threadpool
from fastapi.responses import StreamingResponse
from mcp.server.transport_security import TransportSecuritySettings
from pydantic import BaseModel, Field

from . import __version__, reports
from .assistant import Assistant, ChatMessage, usage_payload
from .config import Settings, settings
from .explain import ExplainRequest, explain
from .kairn import KairnError, ServiceAPI, UserAPI
from .mcp_server import build
from .providers import ProviderUnavailable, for_org

logging.basicConfig(level=os.environ.get("KAIRN_LOG_LEVEL", "INFO").upper(), format="%(asctime)s %(levelname)s %(name)s %(message)s")
log = logging.getLogger("kairn.ai")

CFG: Settings = settings()
MCP = build(CFG)


def _allowed_hosts(cfg: Settings) -> list[str]:
    hosts = {"localhost", "127.0.0.1", "localhost:8091", "127.0.0.1:8091"}
    u = urlparse(cfg.public_url)
    if u.netloc:
        hosts.add(u.netloc)
    hosts |= {h.strip() for h in os.environ.get("KAIRN_MCP_ALLOWED_HOSTS", "").split(",") if h.strip()}
    return sorted(hosts)


_mcp_app = MCP.streamable_http_app(
    streamable_http_path="/mcp",
    transport_security=TransportSecuritySettings(allowed_hosts=_allowed_hosts(CFG), allowed_origins=[CFG.public_url]),
)


@asynccontextmanager
async def lifespan(_: FastAPI) -> AsyncIterator[None]:
    async with MCP.session_manager.run():
        yield


app = FastAPI(title="Kairn AI", version=__version__, docs_url=None, redoc_url=None, lifespan=lifespan)


def _check(token: str | None) -> None:
    expected = CFG.service_token
    if not expected or not token or not hmac.compare_digest(expected, token):
        raise HTTPException(status_code=401, detail="invalid service token")


@app.get("/healthz")
def healthz() -> dict[str, str]:
    return {"status": "ok", "version": __version__}


class ChatIn(BaseModel):
    org_id: str
    messages: list[ChatMessage] = Field(min_length=1, max_length=50)
    locale: str = "fr"


def _sse(ev: dict[str, Any]) -> str:
    return f"data: {json.dumps(ev, ensure_ascii=False, default=str)}\n\n"


@app.post("/v1/assistant/chat")
def chat(
    body: ChatIn, x_kairn_service_token: str | None = Header(default=None), x_kairn_user_token: str | None = Header(default=None)
) -> StreamingResponse:
    _check(x_kairn_service_token)
    if not x_kairn_user_token:
        raise HTTPException(status_code=401, detail="user token required")
    service = ServiceAPI(CFG)
    try:
        org = service.org(body.org_id)
    except KairnError as exc:
        service.close()
        raise HTTPException(status_code=404 if exc.status == 404 else 502, detail="organization not found") from exc
    settings_ = org.get("settings") or {}
    events: queue.Queue[dict[str, Any] | None] = queue.Queue()

    def work() -> None:
        api = UserAPI(CFG, x_kairn_user_token or "", body.org_id)
        try:
            provider = for_org(CFG, str(settings_.get("llm_provider") or ""), bool(settings_.get("allow_external_llm")))
            Assistant(provider, api, service, org, body.locale, CFG.max_tool_turns).run(body.messages, events.put)
        except ProviderUnavailable as exc:
            events.put({"type": "error", "message": str(exc)})
        except Exception:
            log.exception("assistant failure")
            events.put({"type": "error", "message": "Erreur interne de l'assistant." if body.locale != "en" else "Internal assistant error."})
        finally:
            api.close()
            service.close()
            events.put(None)

    threading.Thread(target=work, name="assistant", daemon=True).start()

    def stream() -> Iterator[str]:
        while True:
            try:
                ev = events.get(timeout=15)
            except queue.Empty:
                yield ": keep-alive\n\n"
                continue
            if ev is None:
                return
            yield _sse(ev)

    return StreamingResponse(stream(), media_type="text/event-stream", headers={"Cache-Control": "no-cache", "X-Accel-Buffering": "no"})


class ReportIn(BaseModel):
    org_id: str
    period: str = Field(pattern=r"^\d{4}-\d{2}$")


@app.post("/v1/reports/generate")
async def generate_report(body: ReportIn, x_kairn_service_token: str | None = Header(default=None)) -> dict[str, Any]:
    _check(x_kairn_service_token)

    def work() -> dict[str, Any]:
        with_api = ServiceAPI(CFG)
        try:
            return reports.generate(CFG, with_api, body.org_id, body.period)
        except KairnError as exc:
            raise HTTPException(status_code=502, detail=f"Kairn API error {exc.status}") from exc
        finally:
            with_api.close()

    return await run_in_threadpool(work)


@app.post("/v1/explain")
async def explain_anomaly(body: ExplainRequest, x_kairn_service_token: str | None = Header(default=None)) -> dict[str, Any]:
    _check(x_kairn_service_token)

    def work() -> dict[str, Any]:
        api = ServiceAPI(CFG)
        try:
            org = api.org(body.org_id)
            s = org.get("settings") or {}
            try:
                provider = for_org(CFG, str(s.get("llm_provider") or ""), bool(s.get("allow_external_llm")))
            except ProviderUnavailable:
                return {"explanation": None}
            text, usage = explain(provider, body)
            if usage.input_tokens or usage.output_tokens:
                api.record_usage(body.org_id, usage_payload(usage, "explain"))
            return {"explanation": text}
        except KairnError as exc:
            raise HTTPException(status_code=502, detail=f"Kairn API error {exc.status}") from exc
        finally:
            api.close()

    return await run_in_threadpool(work)


# Le serveur MCP (et ses métadonnées OAuth) est monté en dernier : les routes ci-dessus sont prioritaires.
app.mount("/", _mcp_app)


def main() -> None:
    import uvicorn

    uvicorn.run(app, host=os.environ.get("KAIRN_AI_HOST", "0.0.0.0"), port=int(os.environ.get("KAIRN_AI_PORT", "8091")))  # noqa: S104
