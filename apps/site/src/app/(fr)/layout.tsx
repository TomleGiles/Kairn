import { buildMetadata, Document } from "@/components/document";

export { viewport } from "@/components/document";
export const metadata = buildMetadata("fr");

export default function FrenchLayout({ children }: { children: React.ReactNode }) {
  return <Document locale="fr">{children}</Document>;
}
