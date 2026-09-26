import { ImageResponse } from "next/og";

// Image de partage (LinkedIn, Slack, X…) générée à la construction sous
// /og.png : l'extension garantit le bon type MIME sur tout hébergement statique.
export const dynamic = "force-static";

const size = { width: 1200, height: 630 };

export function GET() {
  return new ImageResponse(
    (
      <div
        style={{
          width: "100%",
          height: "100%",
          display: "flex",
          flexDirection: "column",
          justifyContent: "space-between",
          padding: 72,
          background: "radial-gradient(circle at 50% -10%, #134e4a 0%, #070b12 55%)",
          color: "#e8edf4",
          fontFamily: "sans-serif",
        }}
      >
        <div style={{ display: "flex", alignItems: "center", gap: 20 }}>
          <div
            style={{
              width: 64,
              height: 64,
              borderRadius: 16,
              background: "linear-gradient(135deg, #14b8a6, #6366f1)",
              display: "flex",
              alignItems: "center",
              justifyContent: "center",
              fontSize: 36,
              fontWeight: 700,
              color: "#fff",
            }}
          >
            K
          </div>
          <div style={{ fontSize: 40, fontWeight: 700 }}>Kairn</div>
        </div>
        <div style={{ display: "flex", flexDirection: "column", gap: 24 }}>
          <div style={{ fontSize: 68, fontWeight: 700, lineHeight: 1.1, letterSpacing: -1.5 }}>Le coût réel de votre cloud, enfin expliqué.</div>
          <div style={{ fontSize: 30, color: "#a3aebd" }}>OVHcloud · Scaleway · OUTSCALE · OpenStack · Kubernetes</div>
        </div>
        <div style={{ display: "flex", gap: 16, fontSize: 24, color: "#5eead4" }}>
          <span>FinOps souverain</span>
          <span style={{ color: "#475569" }}>•</span>
          <span>Hébergé dans l&apos;UE ou chez vous</span>
          <span style={{ color: "#475569" }}>•</span>
          <span>Lecture seule</span>
        </div>
      </div>
    ),
    size,
  );
}
