-- Kairn — schéma initial ClickHouse (séries temporelles, coûts, événements).
-- Rétention (M-05) : brut 15 jours, agrégé 5 min 90 jours, agrégé 1 h 25 mois.
-- Les agrégats sont alimentés par des tâches de rollup idempotentes
-- (INSERT … SELECT … FINAL dans des ReplacingMergeTree) plutôt que par des
-- vues matérialisées incrémentales, qui compteraient deux fois les points
-- rejoués lors d'un backfill. Voir ADR-0005.

CREATE TABLE IF NOT EXISTS metrics_raw
(
    org_id      UUID,
    resource_id UUID,
    metric      LowCardinality(String),
    ts          DateTime64(3, 'UTC'),
    value       Float64
)
ENGINE = ReplacingMergeTree
PARTITION BY toYYYYMMDD(ts)
ORDER BY (org_id, resource_id, metric, ts)
TTL toDateTime(ts) + INTERVAL 15 DAY;

CREATE TABLE IF NOT EXISTS metrics_5m
(
    org_id      UUID,
    resource_id UUID,
    metric      LowCardinality(String),
    ts          DateTime('UTC'),
    avg         Float64,
    min         Float64,
    max         Float64,
    p95         Float64,
    samples     UInt32,
    version     DateTime64(3, 'UTC')
)
ENGINE = ReplacingMergeTree(version)
PARTITION BY toYYYYMM(ts)
ORDER BY (org_id, resource_id, metric, ts)
TTL ts + INTERVAL 90 DAY;

CREATE TABLE IF NOT EXISTS metrics_1h
(
    org_id      UUID,
    resource_id UUID,
    metric      LowCardinality(String),
    ts          DateTime('UTC'),
    avg         Float64,
    min         Float64,
    max         Float64,
    p95         Float64,
    samples     UInt32,
    version     DateTime64(3, 'UTC')
)
ENGINE = ReplacingMergeTree(version)
PARTITION BY toYYYYMM(ts)
ORDER BY (org_id, resource_id, metric, ts)
TTL ts + INTERVAL 25 MONTH;

-- Lignes de coût journalières. Montants en Decimal(18,6), jamais en flottant.
CREATE TABLE IF NOT EXISTS cost_lines
(
    org_id             UUID,
    day                Date,
    resource_id        String,
    connector_id       String,
    provider           LowCardinality(String),
    resource_type      LowCardinality(String),
    region             LowCardinality(String),
    cost_type          LowCardinality(String),
    sku                String,
    quantity           Decimal(18, 6),
    unit               LowCardinality(String),
    amount             Decimal(18, 6),
    currency           LowCardinality(String),
    source             LowCardinality(String),
    catalog_version    String,
    allocation_node_id String,
    source_ref         String,
    labels             Map(String, String),
    computed_at        DateTime64(3, 'UTC')
)
ENGINE = ReplacingMergeTree(computed_at)
PARTITION BY toYYYYMM(day)
ORDER BY (org_id, day, resource_id, cost_type, sku, source, allocation_node_id, source_ref);

-- Lignes de facture brutes remontées par les connecteurs (SyncBilling).
-- Entrée du cost-engine (préférence facture) et du rapprochement estimé/facturé.
CREATE TABLE IF NOT EXISTS billing_lines
(
    org_id        UUID,
    connector_id  String,
    provider      LowCardinality(String),
    day           Date,
    resource_id   String,
    service       String,
    sku           String,
    cost_type     LowCardinality(String),
    quantity      Decimal(18, 6),
    unit          LowCardinality(String),
    amount        Decimal(18, 6),
    currency      LowCardinality(String),
    invoice_id    String,
    imported_at   DateTime64(3, 'UTC')
)
ENGINE = ReplacingMergeTree(imported_at)
PARTITION BY toYYYYMM(day)
ORDER BY (org_id, connector_id, day, resource_id, service, sku, invoice_id);

-- Événements : déploiements, HPA, incidents, changements d'inventaire.
CREATE TABLE IF NOT EXISTS events
(
    org_id      UUID,
    ts          DateTime64(3, 'UTC'),
    event_id    String,
    kind        LowCardinality(String),
    source      LowCardinality(String),
    resource_id String,
    title       String,
    payload     String
)
ENGINE = ReplacingMergeTree
PARTITION BY toYYYYMM(ts)
ORDER BY (org_id, ts, event_id)
TTL toDateTime(ts) + INTERVAL 25 MONTH;

-- Résultats des sondes d'uptime.
CREATE TABLE IF NOT EXISTS uptime_results
(
    org_id      UUID,
    check_id    UUID,
    region      LowCardinality(String),
    ts          DateTime64(3, 'UTC'),
    up          UInt8,
    latency_ms  Float64,
    status_code UInt16,
    error       String
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(ts)
ORDER BY (org_id, check_id, ts)
TTL toDateTime(ts) + INTERVAL 13 MONTH;

-- Valeurs journalières des métriques métier (coût unitaire).
CREATE TABLE IF NOT EXISTS unit_metric_values
(
    org_id    UUID,
    metric_id UUID,
    day       Date,
    value     Decimal(18, 6),
    version   DateTime64(3, 'UTC')
)
ENGINE = ReplacingMergeTree(version)
ORDER BY (org_id, metric_id, day);
