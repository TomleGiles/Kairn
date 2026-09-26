// Package logging configure slog pour tous les services (JSON en production).
// Les journaux ne contiennent jamais de credentials, jetons ni données de facture brutes.
package logging

import (
	"log/slog"
	"os"
	"strings"
)

// New crée le logger d'un service.
func New(service, level, format string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn", "warning":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: lvl, ReplaceAttr: redact}
	var h slog.Handler
	if format == "text" {
		h = slog.NewTextHandler(os.Stderr, opts)
	} else {
		h = slog.NewJSONHandler(os.Stderr, opts)
	}
	return slog.New(h).With("service", service)
}

// sensitive liste les clés d'attributs dont la valeur est masquée.
var sensitive = []string{"password", "secret", "token", "authorization", "cookie", "api_key", "apikey", "credential", "private_key"}

func redact(_ []string, a slog.Attr) slog.Attr {
	k := strings.ToLower(a.Key)
	for _, s := range sensitive {
		if strings.Contains(k, s) {
			return slog.String(a.Key, "[REDACTED]")
		}
	}
	return a
}
