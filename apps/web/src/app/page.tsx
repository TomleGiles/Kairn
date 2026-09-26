import { redirect } from "next/navigation";

import { api, must } from "@/lib/api/server";

/** Redirige vers la première organisation de l'utilisateur, ou vers l'onboarding. */
export default async function Home() {
  const client = await api();
  const me = must(await client.GET("/api/v1/me"));
  const first = me.memberships?.[0];
  if (!first?.org_id) redirect("/onboarding");
  redirect(`/o/${first.org_id}`);
}
