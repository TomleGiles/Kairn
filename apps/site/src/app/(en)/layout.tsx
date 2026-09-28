import { buildMetadata, Document } from "@/components/document";

export { viewport } from "@/components/document";
export const metadata = buildMetadata("en");

export default function EnglishLayout({ children }: { children: React.ReactNode }) {
  return <Document locale="en">{children}</Document>;
}
