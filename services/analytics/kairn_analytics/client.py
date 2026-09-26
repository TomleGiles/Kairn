"""Client de l'API interne Kairn (/internal/v1), authentifié par jeton de service."""

from __future__ import annotations

import os
from datetime import datetime
from typing import Any, TypeVar

import httpx
from pydantic import BaseModel, TypeAdapter

from .models import (
    AllocationNode,
    CatalogItem,
    CostRow,
    Edge,
    Event,
    Organization,
    Recommendation,
    Resource,
    Series,
)

T = TypeVar("T")


class KairnClient:
    """Accès typé aux données d'une instance Kairn."""

    def __init__(self, base_url: str | None = None, token: str | None = None, timeout: float = 120.0) -> None:
        self.base_url = (base_url or os.environ.get("KAIRN_API_URL", "http://localhost:8080")).rstrip("/")
        self.token = token or os.environ.get("KAIRN_SERVICE_TOKEN", "")
        self._http = httpx.Client(
            base_url=self.base_url + "/internal/v1",
            headers={"X-Kairn-Service-Token": self.token, "User-Agent": "kairn-analytics"},
            timeout=timeout,
        )

    def close(self) -> None:
        self._http.close()

    def __enter__(self) -> KairnClient:
        return self

    def __exit__(self, *_: object) -> None:
        self.close()

    def _get(self, path: str, adapter: TypeAdapter[T], params: dict[str, Any] | None = None) -> T:
        r = self._http.get(path, params=params)
        r.raise_for_status()
        return adapter.validate_python(r.json())

    def _post(self, path: str, body: Any, adapter: TypeAdapter[T] | None = None) -> T | None:
        payload = body.model_dump(mode="json") if isinstance(body, BaseModel) else body
        r = self._http.post(path, json=payload)
        r.raise_for_status()
        if adapter is None or not r.content:
            return None
        return adapter.validate_python(r.json())

    # ---- lecture
    def orgs(self) -> list[Organization]:
        return self._get("/orgs", TypeAdapter(list[Organization]))

    def resources(self, org: str, types: list[str] | None = None) -> list[Resource]:
        return self._get(f"/orgs/{org}/resources", TypeAdapter(list[Resource]), {"type": types} if types else None)

    def edges(self, org: str) -> list[Edge]:
        return self._get(f"/orgs/{org}/edges", TypeAdapter(list[Edge]))

    def allocation_nodes(self, org: str) -> list[AllocationNode]:
        return self._get(f"/orgs/{org}/allocation-nodes", TypeAdapter(list[AllocationNode]))

    def history(self, org: str, resource_id: str) -> list[Resource]:
        return self._get(f"/orgs/{org}/resources/{resource_id}/history", TypeAdapter(list[Resource]))

    def metrics(
        self, org: str, resource_ids: list[str], metrics: list[str], start: datetime, end: datetime,
        step_seconds: int = 3600, agg: str = "avg",
    ) -> list[Series]:
        out: list[Series] = []
        for i in range(0, len(resource_ids), 2000):
            body = {
                "resource_ids": resource_ids[i : i + 2000], "metrics": metrics, "from": start.isoformat(),
                "to": end.isoformat(), "step_seconds": step_seconds, "agg": agg,
            }
            res = self._post(f"/orgs/{org}/metrics/query", body, TypeAdapter(list[Series]))
            out.extend(res or [])
        return out

    def costs(
        self, org: str, start: datetime, end: datetime, granularity: str = "day",
        group_by: list[str] | None = None, filters: dict[str, list[str]] | None = None,
    ) -> list[CostRow]:
        body = {"from": start.isoformat(), "to": end.isoformat(), "granularity": granularity,
                "group_by": group_by or [], "filters": filters or {}}
        return self._post(f"/orgs/{org}/costs/query", body, TypeAdapter(list[CostRow])) or []

    def events(self, org: str, start: datetime, end: datetime) -> list[Event]:
        return self._get(f"/orgs/{org}/events", TypeAdapter(list[Event]), {"from": start.isoformat(), "to": end.isoformat()})

    def catalog(self, org: str) -> list[CatalogItem]:
        return self._get(f"/orgs/{org}/catalog", TypeAdapter(list[CatalogItem]))

    def recommendations(self, org: str) -> list[Recommendation]:
        return self._get(f"/orgs/{org}/recommendations", TypeAdapter(list[Recommendation]))

    # ---- écriture
    def publish_recommendations(
        self, org: str, items: list[Recommendation], replace_types: list[str], measured: dict[str, str],
    ) -> dict[str, int]:
        body = {"items": [i.model_dump(mode="json", exclude={"id", "status", "applied_at"}) for i in items],
                "replace_types": replace_types, "measured": measured}
        return self._post(f"/orgs/{org}/recommendations/batch", body, TypeAdapter(dict[str, int])) or {}

    def publish_anomalies(self, org: str, items: list[Any]) -> dict[str, int]:
        body = {"items": [i.model_dump(mode="json", exclude={"id"}) for i in items]}
        return self._post(f"/orgs/{org}/anomalies/batch", body, TypeAdapter(dict[str, int])) or {}

    def publish_forecasts(self, org: str, items: list[Any]) -> dict[str, int]:
        body = {"items": [i.model_dump(mode="json") for i in items]}
        return self._post(f"/orgs/{org}/forecasts/batch", body, TypeAdapter(dict[str, int])) or {}
