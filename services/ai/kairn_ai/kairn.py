"""Accès à l'API Kairn.

- `UserAPI` agit avec le jeton de l'utilisateur (session ou jeton d'API) : les
  outils de l'assistant et du serveur MCP n'ont ainsi jamais plus de droits
  que la personne qui pose la question (RBAC, scopes, plan, isolation).
- `ServiceAPI` agit avec le jeton de service, pour les traitements planifiés
  (rapports) et l'enregistrement de la consommation LLM.
"""

from __future__ import annotations

import contextlib
from typing import Any

import httpx

from .config import Settings


class KairnError(RuntimeError):
    def __init__(self, status: int, detail: str) -> None:
        super().__init__(f"Kairn API {status}: {detail}")
        self.status = status
        self.detail = detail


def _raise(r: httpx.Response) -> None:
    if r.status_code >= 400:
        try:
            detail = str(r.json().get("detail") or r.json().get("title") or r.text)
        except ValueError:
            detail = r.text
        raise KairnError(r.status_code, detail[:500])


class UserAPI:
    """API publique avec les droits de l'utilisateur."""

    def __init__(self, cfg: Settings, token: str, org_id: str, transport: httpx.BaseTransport | None = None) -> None:
        self.org_id = org_id
        self._http = httpx.Client(
            base_url=f"{cfg.api_url}/api/v1",
            headers={"Authorization": f"Bearer {token}", "User-Agent": "kairn-ai"},
            timeout=60,
            transport=transport,
        )

    def close(self) -> None:
        self._http.close()

    def get(self, path: str, params: dict[str, Any] | None = None) -> Any:
        clean = {k: v for k, v in (params or {}).items() if v not in (None, "", [])}
        r = self._http.get(path.replace("{org}", self.org_id), params=clean)
        _raise(r)
        return r.json()

    def post(self, path: str, body: Any) -> Any:
        r = self._http.post(path.replace("{org}", self.org_id), json=body)
        _raise(r)
        return r.json() if r.content else None


class ServiceAPI:
    """API avec le jeton de service (routes publiques et /internal/v1)."""

    def __init__(self, cfg: Settings, transport: httpx.BaseTransport | None = None) -> None:
        self._http = httpx.Client(
            base_url=cfg.api_url,
            headers={"X-Kairn-Service-Token": cfg.service_token, "User-Agent": "kairn-ai"},
            timeout=120,
            transport=transport,
        )

    def close(self) -> None:
        self._http.close()

    def get(self, path: str, params: dict[str, Any] | None = None) -> Any:
        clean = {k: v for k, v in (params or {}).items() if v not in (None, "", [])}
        r = self._http.get(path, params=clean)
        _raise(r)
        return r.json()

    def post(self, path: str, body: Any) -> Any:
        r = self._http.post(path, json=body)
        _raise(r)
        return r.json() if r.content else None

    def put(self, path: str, body: Any) -> Any:
        r = self._http.put(path, json=body)
        _raise(r)
        return r.json() if r.content else None

    def org(self, org_id: str) -> dict[str, Any]:
        for o in self.get("/internal/v1/orgs"):
            if o.get("id") == org_id:
                return dict(o)
        raise KairnError(404, "organization not found")

    def record_usage(self, org_id: str, usage: dict[str, Any]) -> None:
        # La comptabilisation ne doit jamais faire échouer une réponse.
        with contextlib.suppress(KairnError, httpx.HTTPError):
            self.post(f"/internal/v1/orgs/{org_id}/llm-usage", usage)
