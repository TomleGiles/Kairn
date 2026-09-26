"""Moteur de recommandations (M-06).

Chaque recommandation porte : économie mensuelle estimée, niveau de risque,
preuves chiffrées et étapes d'application avec une commande prête à l'emploi
(CLI du fournisseur, manifest Kubernetes, Terraform). Aucune action n'est
exécutée : l'application reste une décision humaine.
"""

from __future__ import annotations

import hashlib
import math
from collections import defaultdict
from dataclasses import dataclass, field
from datetime import datetime, timedelta
from decimal import Decimal

from .models import CatalogItem, Edge, Recommendation, Remediation, Resource
from .money import HOURS_PER_MONTH, cents, dec, fmt_eur
from .stats import percentile

NON_PROD_ENVS = {"dev", "development", "staging", "test", "qa", "recette", "preprod", "sandbox"}
# Heures hors ouverture par mois (730 h - 12 h × 21,7 jours ouvrés).
OFF_HOURS_PER_MONTH = Decimal(470)
MIN_SAVINGS = Decimal("5")  # € / mois


def fingerprint(*parts: str) -> str:
    return hashlib.sha256("|".join(parts).encode()).hexdigest()[:24]


@dataclass
class PriceBook:
    """Index des prix en vigueur par fournisseur et SKU."""

    items: dict[tuple[str, str], CatalogItem] = field(default_factory=dict)

    @classmethod
    def build(cls, items: list[CatalogItem]) -> PriceBook:
        pb = cls()
        for it in items:
            key = (it.provider, it.sku)
            if key not in pb.items or it.region == "":
                pb.items[key] = it
        return pb

    def get(self, provider: str, sku: str) -> CatalogItem | None:
        return self.items.get((provider, sku))

    def monthly(self, provider: str, sku: str, qty: Decimal = Decimal(1)) -> Decimal | None:
        it = self.get(provider, sku)
        if it is None:
            return None
        if it.unit in ("hour", "gb_hour", "vcpu_hour"):
            return it.price * qty * HOURS_PER_MONTH
        if it.unit in ("month", "gb_month"):
            return it.price * qty
        return None

    def flavors(self, provider: str) -> list[tuple[str, int, float, Decimal, str]]:
        """(nom, vcpus, ram_go, prix mensuel, famille) triés par prix."""
        out = []
        for (p, sku), it in self.items.items():
            if p != provider or not sku.startswith("compute.flavor.") or sku.endswith(".monthly"):
                continue
            try:
                vcpus = int(float(it.attributes.get("vcpus", "0")))
                ram = float(it.attributes.get("ram_gb", "0"))
            except ValueError:
                continue
            m = self.monthly(p, sku)
            if m is None or vcpus <= 0:
                continue
            out.append((sku.removeprefix("compute.flavor."), vcpus, ram, m, it.attributes.get("family", "")))
        out.sort(key=lambda x: (x[3], x[0]))
        return out


@dataclass
class Context:
    now: datetime
    currency: str
    locale: str
    window_days: int
    percentile: int
    resources: list[Resource]
    edges: list[Edge]
    prices: PriceBook
    series: dict[tuple[str, str], list[float]]  # (resource_id, métrique) → valeurs horaires
    night_series: dict[tuple[str, str], list[float]]  # valeurs hors heures ouvrées
    workload_pods: dict[str, list[str]]  # workload_id → pods
    history: dict[str, list[Resource]] = field(default_factory=dict)

    def fmt(self, x: Decimal) -> str:
        return fmt_eur(x, self.currency, self.locale)


def _cli_resize(provider: str, ext: str, target: str) -> tuple[str, str]:
    if provider == "scaleway":
        cli = [f"scw instance server stop {ext}", f"scw instance server update {ext} commercial-type={target}",
               f"scw instance server start {ext}"]
        return "\n".join(cli), f'resource "scaleway_instance_server" "this" {{\n  type = "{target}"\n}}'
    if provider == "outscale":
        ids = f"'[\"{ext}\"]'"
        cli = [f"osc-cli api StopVms --VmIds {ids}", f"osc-cli api UpdateVm --VmId {ext} --VmType {target}",
               f"osc-cli api StartVms --VmIds {ids}"]
        return "\n".join(cli), f'resource "outscale_vm" "this" {{\n  vm_type = "{target}"\n}}'
    if provider == "aws":
        value = "'{\"Value\": \"" + target + "\"}'"
        cli = [f"aws ec2 stop-instances --instance-ids {ext}",
               f"aws ec2 modify-instance-attribute --instance-id {ext} --instance-type {value}",
               f"aws ec2 start-instances --instance-ids {ext}"]
        return "\n".join(cli), f'resource "aws_instance" "this" {{\n  instance_type = "{target}"\n}}'
    return (f"openstack server resize --flavor {target} {ext}\n# après vérification :\nopenstack server resize confirm {ext}",
            f'resource "openstack_compute_instance_v2" "this" {{\n  flavor_name = "{target}"\n}}')


def _backing_vms(edges: list[Edge]) -> set[str]:
    return {e.parent_id for e in edges if e.relation == "backs"}


def _is_non_prod(r: Resource) -> bool:
    env = (r.labels.get("env") or r.attr("env") or "").lower()
    return env in NON_PROD_ENVS


def rightsize_vms(ctx: Context) -> list[Recommendation]:
    out: list[Recommendation] = []
    backing = _backing_vms(ctx.edges)
    for r in ctx.resources:
        if r.type != "compute.instance" or r.id in backing or r.attr("billing_state") == "stopped_unbilled":
            continue
        flavor = r.attr("flavor") or r.attr("instance_type")
        current = ctx.prices.monthly(r.provider, "compute.flavor." + flavor)
        cpu = ctx.series.get((r.id, "cpu.utilization"), [])
        mem = ctx.series.get((r.id, "mem.utilization"), [])
        expected = ctx.window_days * 24
        if current is None or len(cpu) < expected * 0.6:
            continue
        vcpus, ram = r.attr_num("vcpus"), r.attr_num("ram_gb")
        p_cpu, max_cpu = percentile(cpu, ctx.percentile), max(cpu)
        p_mem = percentile(mem, ctx.percentile) if mem else 0.9
        need_cpu = max(1, math.ceil(vcpus * p_cpu * 1.3))
        need_ram = max(1.0, ram * p_mem * 1.2)
        fam = ctx.prices.get(r.provider, "compute.flavor." + flavor)
        family = fam.attributes.get("family", "") if fam else ""
        candidates = [f for f in ctx.prices.flavors(r.provider) if f[1] >= need_cpu and f[2] >= need_ram and f[0] != flavor]
        same = [f for f in candidates if f[4] == family] or candidates
        if not same:
            continue
        target = same[0]
        savings = current - target[3]
        if savings < MIN_SAVINGS or savings < current * Decimal("0.1"):
            continue
        coverage = len(cpu) / expected
        risk = "low" if p_cpu < 0.3 and coverage >= 0.9 else "medium"
        if max_cpu * vcpus > target[1] * 0.9:
            risk = "high"
        cli, tf = _cli_resize(r.provider, r.external_id, target[0])
        out.append(Recommendation(
            type="rightsize_vm", resource_id=r.id, fingerprint=fingerprint("rightsize_vm", r.id, target[0]),
            title=f"Redimensionner {r.name} : {flavor} → {target[0]}",
            summary=(f"Sur {ctx.window_days} jours, le CPU de {r.name} reste sous {p_cpu:.0%} (P{ctx.percentile}) "
                     f"et la mémoire sous {p_mem:.0%}. La gamme {target[0]} ({target[1]} vCPU, {target[2]:g} Go) "
                     f"couvre ce besoin avec une marge de 30 %."),
            savings_monthly=cents(savings), currency=ctx.currency, risk=risk,
            evidence={"window_days": ctx.window_days, "percentile": ctx.percentile, "cpu_p": round(p_cpu, 4),
                      "cpu_max": round(max_cpu, 4), "mem_p": round(p_mem, 4), "samples": len(cpu),
                      "current_flavor": flavor, "current_monthly": str(cents(current)), "target_flavor": target[0],
                      "target_monthly": str(cents(target[3])), "metrics": ["cpu.utilization", "mem.utilization"]},
            remediation=Remediation(steps=[
                "Vérifier l'absence de pic saisonnier non couvert par la fenêtre d'observation.",
                "Planifier une fenêtre de maintenance (le redimensionnement redémarre la VM).",
                f"Redimensionner vers {target[0]}, valider le service puis confirmer.",
            ], cli=cli, terraform=tf),
        ))
    return out


def rightsize_workloads(ctx: Context) -> list[Recommendation]:
    """Ajuste les requests des workloads Kubernetes au P95 de l'usage réel."""
    out: list[Recommendation] = []
    by_id = {r.id: r for r in ctx.resources}
    # Coût d'un cœur et d'un Go de RAM, dérivé de la gamme des nodes.
    core_month, gb_month = _k8s_unit_costs(ctx)
    if core_month is None or gb_month is None:
        return out
    for wl_id, pods in ctx.workload_pods.items():
        wl = by_id.get(wl_id)
        if wl is None or not pods:
            continue
        req_cpu, req_mem = wl.attr_num("cpu_request_cores"), wl.attr_num("mem_request_gb")
        replicas = max(1, int(wl.attr_num("replicas") or len(pods)))
        cpu_vals: list[float] = []
        mem_vals: list[float] = []
        for p in pods:
            cpu_vals += ctx.series.get((p, "cpu.usage_cores"), [])
            mem_vals += [v / 2**30 for v in ctx.series.get((p, "mem.usage_bytes"), [])]
        if len(cpu_vals) < 48 or req_cpu <= 0:
            continue
        p_cpu = percentile(cpu_vals, ctx.percentile)
        max_mem = max(mem_vals) if mem_vals else req_mem
        rec_cpu = max(0.05, round(p_cpu * 1.2, 2))
        rec_mem = max(0.064, round(max_mem * 1.15, 2)) if mem_vals else req_mem
        ns, name = wl.attr("k8s.namespace"), wl.name
        manifest = (f"apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: {name}\n  namespace: {ns}\nspec:\n  template:\n"
                    f"    spec:\n      containers:\n        - name: {name}\n          resources:\n            requests:\n"
                    f"              cpu: \"{int(rec_cpu * 1000)}m\"\n              memory: \"{int(rec_mem * 1024)}Mi\"\n")
        cli = f"kubectl -n {ns} set resources deployment/{name} --requests=cpu={int(rec_cpu * 1000)}m,memory={int(rec_mem * 1024)}Mi"
        if p_cpu > req_cpu * 1.1:
            # Sous-dimensionné : risque de throttling, pas d'économie.
            out.append(Recommendation(
                type="rightsize_workload", resource_id=wl_id, fingerprint=fingerprint("rightsize_workload_up", wl_id),
                title=f"Augmenter les requests CPU de {ns}/{name}",
                summary=(f"L'usage CPU P{ctx.percentile} ({p_cpu:.2f} cœur) dépasse la request ({req_cpu:.2f}) : "
                         "le workload consomme de la capacité non réservée et risque la contention. "
                         "Le coût alloué augmentera mais la répartition sera juste."),
                savings_monthly=Decimal(0), currency=ctx.currency, risk="high",
                evidence={"cpu_p": round(p_cpu, 3), "cpu_request": req_cpu, "replicas": replicas, "samples": len(cpu_vals)},
                remediation=Remediation(steps=["Appliquer les nouvelles requests.", "Surveiller le throttling CPU."], cli=cli, manifest=manifest),
            ))
            continue
        d_cpu = req_cpu - rec_cpu
        d_mem = max(0.0, req_mem - rec_mem)
        if d_cpu < req_cpu * 0.2:
            continue
        savings = (dec(d_cpu) * core_month + dec(d_mem) * gb_month) * replicas
        if savings < MIN_SAVINGS:
            continue
        out.append(Recommendation(
            type="rightsize_workload", resource_id=wl_id, fingerprint=fingerprint("rightsize_workload", wl_id),
            title=f"Réduire les requests de {ns}/{name} ({req_cpu:g} → {rec_cpu:g} cœur)",
            summary=(f"Sur {ctx.window_days} jours, {name} utilise au plus {p_cpu:.2f} cœur par pod (P{ctx.percentile}) "
                     f"pour {req_cpu:g} réservé. Ajuster les requests libère de la capacité sur {replicas} réplica(s) "
                     "et permet de réduire le nombre de nodes."),
            savings_monthly=cents(savings), currency=ctx.currency, risk="low" if p_cpu < req_cpu * 0.5 else "medium",
            evidence={"cpu_p": round(p_cpu, 3), "cpu_request": req_cpu, "cpu_recommended": rec_cpu,
                      "mem_max_gb": round(max_mem, 3), "mem_request_gb": req_mem, "mem_recommended_gb": rec_mem,
                      "replicas": replicas, "samples": len(cpu_vals), "core_month": str(cents(core_month))},
            remediation=Remediation(steps=[
                "Appliquer les nouvelles requests via le dépôt GitOps du workload.",
                "Surveiller la latence et le throttling pendant 48 h.",
                "Laisser l'autoscaler de cluster retirer les nodes excédentaires.",
            ], cli=cli, manifest=manifest),
        ))
    return out


def _k8s_unit_costs(ctx: Context) -> tuple[Decimal | None, Decimal | None]:
    nodes = [r for r in ctx.resources if r.type == "k8s.node"]
    vms = {r.external_id: r for r in ctx.resources if r.type == "compute.instance"}
    for n in nodes:
        vm = vms.get(n.attr("provider_instance_id"))
        provider = vm.provider if vm else n.provider
        flavor = n.attr("instance_type") or (vm.attr("flavor") if vm else "")
        monthly = ctx.prices.monthly(provider, "compute.flavor." + flavor)
        cpu, ram = n.attr_num("cpu_capacity_cores"), n.attr_num("mem_capacity_gb")
        if monthly is None or cpu <= 0 or ram <= 0:
            continue
        ratio = 7.5
        cpu_w = cpu * ratio / (cpu * ratio + ram)
        return monthly * dec(cpu_w) / dec(cpu), monthly * dec(1 - cpu_w) / dec(ram)
    return None, None


def orphans(ctx: Context) -> list[Recommendation]:
    out: list[Recommendation] = []
    for r in ctx.resources:
        if r.type == "storage.volume" and not r.attr("attached_to") and r.attr("status").lower() == "available":
            since = _unattached_since(ctx, r)
            if ctx.now - since < timedelta(days=7):
                continue
            size = dec(r.attr_num("size_gb"))
            monthly = (ctx.prices.monthly(r.provider, "storage.volume." + r.attr("volume_type"), size)
                       or ctx.prices.monthly(r.provider, "storage.volume", size))
            if not monthly or monthly < Decimal(1):
                continue
            days = (ctx.now - since).days
            out.append(Recommendation(
                type="orphan_volume", resource_id=r.id, fingerprint=fingerprint("orphan_volume", r.id),
                title=f"Supprimer le volume non attaché {r.name} ({r.attr('size_gb')} Go)",
                summary=f"Le volume {r.name} n'est attaché à aucune instance depuis {days} jours.",
                savings_monthly=cents(monthly), currency=ctx.currency, risk="medium",
                evidence={"unattached_since": since.isoformat(), "days": days, "size_gb": r.attr("size_gb"), "volume_type": r.attr("volume_type")},
                remediation=Remediation(steps=[
                    "Confirmer avec l'équipe propriétaire que les données ne sont plus utiles.",
                    "Créer un snapshot de sauvegarde (facultatif, moins coûteux).",
                    "Supprimer le volume.",
                ], cli=f"openstack volume snapshot create --volume {r.external_id} --force backup-{r.name}\nopenstack volume delete {r.external_id}"),
            ))
        elif r.type == "network.ip" and not r.attr("attached_to"):
            monthly = ctx.prices.monthly(r.provider, "network.ip." + (r.attr("ip_kind") or "floating"))
            if not monthly:
                continue
            out.append(Recommendation(
                type="orphan_ip", resource_id=r.id, fingerprint=fingerprint("orphan_ip", r.id),
                title=f"Libérer l'IP publique inutilisée {r.name}",
                summary=f"L'adresse {r.name} n'est associée à aucune ressource.",
                savings_monthly=cents(monthly), currency=ctx.currency, risk="low",
                evidence={"status": r.attr("status")},
                remediation=Remediation(steps=["Vérifier qu'aucun enregistrement DNS ne pointe vers cette IP.", "Libérer l'adresse."],
                                        cli=f"openstack floating ip delete {r.external_id}"),
            ))
        elif r.type == "storage.snapshot":
            created = r.attr("created_at")
            try:
                created_at = datetime.fromisoformat(created.replace("Z", "+00:00")) if created else r.valid_from
            except ValueError:
                created_at = r.valid_from
            age = (ctx.now - created_at).days
            if age < 90:
                continue
            monthly = ctx.prices.monthly(r.provider, "storage.snapshot", dec(r.attr_num("size_gb")))
            if not monthly or monthly < Decimal(1):
                continue
            out.append(Recommendation(
                type="old_snapshot", resource_id=r.id, fingerprint=fingerprint("old_snapshot", r.id),
                title=f"Supprimer le snapshot ancien {r.name} ({age} jours)",
                summary=f"Le snapshot {r.name} a {age} jours ; vérifier la politique de rétention.",
                savings_monthly=cents(monthly), currency=ctx.currency, risk="medium",
                evidence={"age_days": age, "size_gb": r.attr("size_gb")},
                remediation=Remediation(steps=["Vérifier la politique de rétention applicable.", "Supprimer le snapshot."],
                                        cli=f"openstack volume snapshot delete {r.external_id}"),
            ))
    return out


def _unattached_since(ctx: Context, r: Resource) -> datetime:
    hist = ctx.history.get(r.id) or [r]
    since = r.valid_from
    for v in reversed(hist):
        if v.attr("attached_to"):
            break
        since = v.valid_from
    return since


def off_hours(ctx: Context) -> list[Recommendation]:
    out: list[Recommendation] = []
    backing = _backing_vms(ctx.edges)
    for r in ctx.resources:
        if r.type != "compute.instance" or r.id in backing or not _is_non_prod(r):
            continue
        night = ctx.night_series.get((r.id, "cpu.utilization"), [])
        if len(night) < 48 or percentile(night, 90) > 0.08:
            continue
        hourly = ctx.prices.monthly(r.provider, "compute.flavor." + (r.attr("flavor") or r.attr("instance_type")))
        if hourly is None:
            continue
        savings = hourly / HOURS_PER_MONTH * OFF_HOURS_PER_MONTH * Decimal("0.9")
        if savings < MIN_SAVINGS:
            continue
        ext = r.external_id
        out.append(Recommendation(
            type="off_hours_schedule", resource_id=r.id, fingerprint=fingerprint("off_hours", r.id),
            title=f"Mettre en veille {r.name} hors heures ouvrées",
            summary=(f"{r.name} ({r.labels.get('env') or r.attr('env')}) est quasi inactive la nuit et le week-end "
                     f"(CPU P90 {percentile(night, 90):.0%}). Une mise en veille de 20 h à 8 h et le week-end évite "
                     f"environ {OFF_HOURS_PER_MONTH} heures de facturation par mois."),
            savings_monthly=cents(savings), currency=ctx.currency, risk="low",
            evidence={"night_cpu_p90": round(percentile(night, 90), 4), "night_samples": len(night), "off_hours_month": 470},
            remediation=Remediation(steps=[
                "Valider le calendrier avec l'équipe (aucun traitement nocturne attendu).",
                "Planifier la mise en veille (shelve : seul le disque reste facturé) et le réveil.",
            ], cli=(f"# crontab de l'outil d'automatisation\n0 20 * * 1-5 openstack server shelve {ext}\n"
                    f"0 8 * * 1-5 openstack server unshelve {ext}"),
                manifest=(f"apiVersion: batch/v1\nkind: CronJob\nmetadata:\n  name: shelve-{r.name}\nspec:\n  schedule: \"0 20 * * 1-5\"\n"
                          f"  jobTemplate:\n    spec:\n      template:\n        spec:\n          restartPolicy: Never\n          containers:\n"
                          f"            - name: shelve\n              image: openstacktools/openstack-client\n"
                          f"              args: [\"openstack\", \"server\", \"shelve\", \"{ext}\"]\n")),
        ))
    return out


def storage_tier(ctx: Context) -> list[Recommendation]:
    out: list[Recommendation] = []
    for r in ctx.resources:
        if r.type != "storage.volume" or not r.attr("attached_to"):
            continue
        vt = r.attr("volume_type")
        if vt not in ("high-speed", "high-speed-gen2", "io1", "sbs_15k", "gp2"):
            continue
        iops = ctx.series.get((r.id, "disk.iops"), [])
        if len(iops) < 48 or percentile(iops, 95) > 200:
            continue
        size = dec(r.attr_num("size_gb"))
        cur = ctx.prices.monthly(r.provider, "storage.volume." + vt, size)
        cheaper_type = {"high-speed": "classic", "high-speed-gen2": "classic", "io1": "standard", "gp2": "standard", "sbs_15k": "sbs_5k"}[vt]
        tgt = ctx.prices.monthly(r.provider, "storage.volume." + cheaper_type, size)
        if cur is None or tgt is None or cur - tgt < MIN_SAVINGS:
            continue
        out.append(Recommendation(
            type="storage_tier", resource_id=r.id, fingerprint=fingerprint("storage_tier", r.id, cheaper_type),
            title=f"Passer le volume {r.name} de {vt} à {cheaper_type}",
            summary=(f"Le volume {r.name} ({r.attr('size_gb')} Go) ne dépasse pas {percentile(iops, 95):.0f} IOPS (P95) : "
                     f"le stockage {vt} est surdimensionné."),
            savings_monthly=cents(cur - tgt), currency=ctx.currency, risk="medium",
            evidence={"iops_p95": round(percentile(iops, 95), 1), "samples": len(iops), "current_type": vt, "target_type": cheaper_type},
            remediation=Remediation(steps=["Planifier une fenêtre (la migration peut dégrader les performances).", "Changer le type du volume."],
                                    cli=f"openstack volume retype --migration-policy on-demand {r.external_id} {cheaper_type}"),
        ))
    return out


def commitments(ctx: Context) -> list[Recommendation]:
    """Facturation mensuelle / engagement pour les instances allumées en continu."""
    out: list[Recommendation] = []
    for r in ctx.resources:
        if r.type != "compute.instance" or _is_non_prod(r):
            continue
        flavor = r.attr("flavor") or r.attr("instance_type")
        monthly_plan = ctx.prices.monthly(r.provider, f"compute.flavor.{flavor}.monthly")
        hourly = ctx.prices.monthly(r.provider, "compute.flavor." + flavor)
        if monthly_plan is None or hourly is None:
            continue
        age = (ctx.now - r.valid_from).days
        if age < 30:
            continue
        savings = hourly - monthly_plan
        if savings < MIN_SAVINGS:
            continue
        out.append(Recommendation(
            type="commitment", resource_id=r.id, fingerprint=fingerprint("commitment", r.id, flavor),
            title=f"Passer {r.name} en facturation mensuelle",
            summary=(f"{r.name} tourne en continu depuis {age} jours : le forfait mensuel {flavor} coûte "
                     f"{ctx.fmt(monthly_plan)} contre {ctx.fmt(hourly)} à l'heure."),
            savings_monthly=cents(savings), currency=ctx.currency, risk="low",
            evidence={"running_days": age, "hourly_monthly": str(cents(hourly)), "monthly_plan": str(cents(monthly_plan))},
            remediation=Remediation(steps=["Confirmer que l'instance restera en service au moins un mois.",
                                           "Activer la facturation mensuelle depuis la console ou l'API du fournisseur."]),
        ))
    return out


def idle(ctx: Context) -> list[Recommendation]:
    out: list[Recommendation] = []
    backing = _backing_vms(ctx.edges)
    for r in ctx.resources:
        if r.type != "compute.instance" or r.id in backing:
            continue
        cpu = ctx.series.get((r.id, "cpu.utilization"), [])
        net = ctx.series.get((r.id, "net.rx_bytes_per_sec"), [])
        if len(cpu) < ctx.window_days * 24 * 0.6:
            continue
        if percentile(cpu, 95) > 0.03 or (net and percentile(net, 95) > 50_000):
            continue
        monthly = ctx.prices.monthly(r.provider, "compute.flavor." + (r.attr("flavor") or r.attr("instance_type")))
        if monthly is None or monthly < MIN_SAVINGS:
            continue
        out.append(Recommendation(
            type="idle_resource", resource_id=r.id, fingerprint=fingerprint("idle", r.id),
            title=f"Instance inactive : {r.name}",
            summary=f"{r.name} n'a quasiment aucune activité CPU ni réseau sur {ctx.window_days} jours (CPU P95 {percentile(cpu, 95):.1%}).",
            savings_monthly=cents(monthly), currency=ctx.currency, risk="medium",
            evidence={"cpu_p95": round(percentile(cpu, 95), 4), "net_rx_p95": round(percentile(net, 95), 1) if net else None},
            remediation=Remediation(steps=["Identifier le propriétaire et l'usage réel.", "Mettre en veille (shelve) puis supprimer si inutile."],
                                    cli=f"openstack server shelve {r.external_id}"),
        ))
    return out


def build_workload_pods(resources: list[Resource], edges: list[Edge]) -> dict[str, list[str]]:
    types = {r.id: r.type for r in resources}
    out: dict[str, list[str]] = defaultdict(list)
    for e in edges:
        if e.relation == "owns" and types.get(e.parent_id) == "k8s.workload" and types.get(e.child_id) == "k8s.pod":
            out[e.parent_id].append(e.child_id)
    return dict(out)


ALL_TYPES = ["rightsize_vm", "rightsize_workload", "orphan_volume", "orphan_ip", "old_snapshot", "off_hours_schedule",
             "storage_tier", "commitment", "idle_resource", "flavor_change"]


def generate(ctx: Context) -> list[Recommendation]:
    recos = (rightsize_vms(ctx) + rightsize_workloads(ctx) + orphans(ctx) + off_hours(ctx) + storage_tier(ctx)
             + commitments(ctx) + idle(ctx))
    # Une seule recommandation de dimensionnement par ressource : la plus rentable.
    best: dict[tuple[str, str], Recommendation] = {}
    for r in recos:
        # Dimensionnement, inactivité et mise en veille d'une même VM ne s'additionnent pas.
        key = (r.resource_id, "compute" if r.type in ("rightsize_vm", "idle_resource", "off_hours_schedule") else r.type)
        if key not in best or r.savings_monthly > best[key].savings_monthly:
            best[key] = r
    return sorted(best.values(), key=lambda r: (-r.savings_monthly, r.fingerprint))
