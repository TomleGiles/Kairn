import { ogImage } from "@/lib/og";

// Image de partage de la page anglaise (/en/og.png).
export const dynamic = "force-static";

export function GET() {
  return ogImage("en");
}
