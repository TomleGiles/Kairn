"use client";

import { useState } from "react";

import { Button, Input, Label } from "@/components/ui/primitives";
import { browserApi, problemMessage } from "@/lib/api/browser";

export function LoginForm({ labels, next }: { labels: { email: string; submit: string }; next: string }) {
  const [email, setEmail] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  return (
    <form
      onSubmit={async (e) => {
        e.preventDefault();
        setBusy(true);
        setError(null);
        const { error } = await browserApi.POST("/api/v1/auth/login", { body: { email } });
        setBusy(false);
        if (error) {
          setError(problemMessage(error));
          return;
        }
        window.location.href = next.startsWith("/") && !next.startsWith("//") ? next : "/";
      }}
      className="space-y-3"
    >
      <div>
        <Label htmlFor="email">{labels.email}</Label>
        <Input id="email" type="email" autoComplete="email" required value={email} onChange={(e) => setEmail(e.target.value)} placeholder="demo@kairn.local" />
      </div>
      {error ? (
        <p role="alert" className="text-sm text-danger">
          {error}
        </p>
      ) : null}
      <Button type="submit" className="w-full" disabled={busy}>
        {labels.submit}
      </Button>
    </form>
  );
}
