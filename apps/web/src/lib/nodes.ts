import type { Schemas } from "@/lib/api/server";

/** Index des nœuds d'allocation : nom et chemin lisible. */
export function nodeIndex(nodes: Schemas["AllocationNode"][], unallocated: string) {
  const byId = new Map(nodes.map((n) => [n.id ?? "", n]));
  const path = (id: string): string => {
    const parts: string[] = [];
    let cur = byId.get(id);
    const seen = new Set<string>();
    while (cur && !seen.has(cur.id ?? "")) {
      seen.add(cur.id ?? "");
      parts.unshift(cur.name ?? "");
      cur = cur.parent_id ? byId.get(cur.parent_id) : undefined;
    }
    return parts.join(" / ");
  };
  return {
    name: (id: string | undefined) => (!id || id === "unallocated" ? unallocated : (byId.get(id)?.name ?? id.slice(0, 8))),
    path: (id: string | undefined) => (!id || id === "unallocated" ? unallocated : path(id) || id.slice(0, 8)),
    nodes,
  };
}
