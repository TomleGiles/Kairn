import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";

export function cn(...inputs: ClassValue[]): string {
  return twMerge(clsx(inputs));
}

export function initials(name: string): string {
  return name
    .split(/[\s@._-]+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((p) => p[0]?.toUpperCase() ?? "")
    .join("");
}

/** Libellé court d'un type de ressource normalisé. */
export function resourceTypeLabel(type: string, locale: "fr" | "en" = "fr"): string {
  const fr: Record<string, string> = {
    project: "Projet",
    "compute.instance": "Instance",
    "storage.volume": "Volume",
    "storage.snapshot": "Snapshot",
    "storage.bucket": "Bucket",
    "network.ip": "IP publique",
    "network.loadbalancer": "Load balancer",
    "database.instance": "Base de données",
    "k8s.cluster": "Cluster K8s",
    "k8s.node": "Node K8s",
    "k8s.namespace": "Namespace",
    "k8s.workload": "Workload",
    "k8s.pod": "Pod",
    host: "Hôte",
  };
  const en: Record<string, string> = {
    project: "Project",
    "compute.instance": "Instance",
    "storage.volume": "Volume",
    "storage.snapshot": "Snapshot",
    "storage.bucket": "Bucket",
    "network.ip": "Public IP",
    "network.loadbalancer": "Load balancer",
    "database.instance": "Database",
    "k8s.cluster": "K8s cluster",
    "k8s.node": "K8s node",
    "k8s.namespace": "Namespace",
    "k8s.workload": "Workload",
    "k8s.pod": "Pod",
    host: "Host",
  };
  return (locale === "fr" ? fr : en)[type] ?? type;
}

export const riskTone = (r?: string) => (r === "high" ? "danger" : r === "medium" ? "warning" : "success") as "danger" | "warning" | "success";

export const severityTone = (s?: string) => (s === "critical" ? "danger" : s === "warning" ? "warning" : "info") as "danger" | "warning" | "info";

export const statusTone = (s?: string) =>
  (s === "ok" ? "success" : s === "degraded" ? "warning" : s === "error" ? "danger" : "neutral") as "success" | "warning" | "danger" | "neutral";
