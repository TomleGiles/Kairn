"""Service HTTP analytics : déclenché par le bus via l'ordonnanceur Kairn."""

from __future__ import annotations

import hmac
import logging
import os
from typing import Any

from fastapi import FastAPI, Header, HTTPException
from fastapi.concurrency import run_in_threadpool
from pydantic import BaseModel

from . import __version__
from .client import KairnClient
from .runner import run_all, run_org

logging.basicConfig(level=os.environ.get("KAIRN_LOG_LEVEL", "INFO").upper(),
                    format="%(asctime)s %(levelname)s %(name)s %(message)s")
log = logging.getLogger("kairn.analytics")

app = FastAPI(title="Kairn analytics", version=__version__, docs_url=None, redoc_url=None)


class RunRequest(BaseModel):
    org_id: str


def _check(token: str | None) -> None:
    expected = os.environ.get("KAIRN_SERVICE_TOKEN", "")
    if not expected or not token or not hmac.compare_digest(expected, token):
        raise HTTPException(status_code=401, detail="invalid service token")


@app.get("/healthz")
def healthz() -> dict[str, str]:
    return {"status": "ok", "version": __version__}


@app.post("/v1/run")
async def run(req: RunRequest, x_kairn_service_token: str | None = Header(default=None)) -> dict[str, Any]:
    _check(x_kairn_service_token)

    def work() -> dict[str, Any]:
        with KairnClient() as client:
            org = next((o for o in client.orgs() if o.id == req.org_id), None)
            if org is None:
                raise HTTPException(status_code=404, detail="organization not found")
            return run_org(client, org)

    return await run_in_threadpool(work)


@app.post("/v1/run-all")
async def run_everything(x_kairn_service_token: str | None = Header(default=None)) -> list[dict[str, Any]]:
    _check(x_kairn_service_token)

    def work() -> list[dict[str, Any]]:
        with KairnClient() as client:
            return run_all(client)

    return await run_in_threadpool(work)


def main() -> None:
    import uvicorn

    uvicorn.run("kairn_analytics.app:app", host=os.environ.get("KAIRN_HOST", "0.0.0.0"),  # noqa: S104
                port=int(os.environ.get("KAIRN_PORT", "8090")), log_level="info")


if __name__ == "__main__":
    main()
