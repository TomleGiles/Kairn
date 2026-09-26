"""Configuration par variables d'environnement."""

from __future__ import annotations

import os
from dataclasses import dataclass, field


@dataclass(frozen=True)
class Settings:
    api_url: str = field(default_factory=lambda: os.environ.get("KAIRN_API_URL", "http://localhost:8080").rstrip("/"))
    service_token: str = field(default_factory=lambda: os.environ.get("KAIRN_SERVICE_TOKEN", ""))
    # Anthropic (défaut SaaS)
    anthropic_model: str = field(default_factory=lambda: os.environ.get("KAIRN_ANTHROPIC_MODEL", "claude-opus-5"))
    anthropic_effort: str = field(default_factory=lambda: os.environ.get("KAIRN_ANTHROPIC_EFFORT", "medium"))
    # Mistral (option souveraine, hébergement UE)
    mistral_url: str = field(default_factory=lambda: os.environ.get("KAIRN_MISTRAL_URL", "https://api.mistral.ai/v1"))
    mistral_key: str = field(default_factory=lambda: os.environ.get("MISTRAL_API_KEY", ""))
    mistral_model: str = field(default_factory=lambda: os.environ.get("KAIRN_MISTRAL_MODEL", "mistral-large-latest"))
    # Modèle local via API compatible OpenAI (self-hosted)
    local_url: str = field(default_factory=lambda: os.environ.get("KAIRN_LOCAL_LLM_URL", "http://localhost:8000/v1"))
    local_key: str = field(default_factory=lambda: os.environ.get("KAIRN_LOCAL_LLM_KEY", ""))
    local_model: str = field(default_factory=lambda: os.environ.get("KAIRN_LOCAL_LLM_MODEL", "mistral-small"))
    # Rapports
    report_font: str = field(default_factory=lambda: os.environ.get("KAIRN_REPORT_FONT", ""))
    public_url: str = field(default_factory=lambda: os.environ.get("KAIRN_PUBLIC_URL", "http://localhost:3000").rstrip("/"))
    max_tool_turns: int = field(default_factory=lambda: int(os.environ.get("KAIRN_ASSISTANT_MAX_TURNS", "8")))


def settings() -> Settings:
    return Settings()
