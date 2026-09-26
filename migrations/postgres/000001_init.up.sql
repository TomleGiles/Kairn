-- Kairn — schéma initial PostgreSQL 16.
-- Isolation multi-tenant : chaque table client porte org_id et est protégée par
-- Row-Level Security (FORCE). L'application positionne, dans chaque transaction :
--   set_config('app.org_id',  <org>,  true)
--   set_config('app.user_id', <user>, true)   -- facultatif
-- Les accès transverses (authentification, ordonnanceurs) passent uniquement
-- par les fonctions SECURITY DEFINER définies en fin de fichier.

CREATE OR REPLACE FUNCTION kairn_current_org() RETURNS uuid
LANGUAGE sql STABLE AS $$
  SELECT nullif(current_setting('app.org_id', true), '')::uuid
$$;

CREATE OR REPLACE FUNCTION kairn_current_user() RETURNS uuid
LANGUAGE sql STABLE AS $$
  SELECT nullif(current_setting('app.user_id', true), '')::uuid
$$;

CREATE OR REPLACE FUNCTION kairn_is_system() RETURNS boolean
LANGUAGE sql STABLE AS $$
  SELECT coalesce(current_setting('app.system', true), '') = 'on'
$$;

-- ---------------------------------------------------------------- organisations
CREATE TABLE organizations (
  id            uuid PRIMARY KEY,
  parent_org_id uuid REFERENCES organizations(id) ON DELETE RESTRICT,
  name          text NOT NULL,
  slug          text NOT NULL UNIQUE,
  plan          text NOT NULL DEFAULT 'starter' CHECK (plan IN ('starter','team','enterprise','msp')),
  currency      text NOT NULL DEFAULT 'EUR',
  locale        text NOT NULL DEFAULT 'fr',
  timezone      text NOT NULL DEFAULT 'Europe/Paris',
  vat_rate      numeric(6,4),
  settings      jsonb NOT NULL DEFAULT '{}',
  trial_ends_at timestamptz,
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX organizations_parent_idx ON organizations(parent_org_id);

CREATE TABLE users (
  id           uuid PRIMARY KEY,
  email        text NOT NULL UNIQUE,
  name         text NOT NULL DEFAULT '',
  oidc_subject text UNIQUE,
  locale       text NOT NULL DEFAULT 'fr',
  created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE memberships (
  org_id     uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  role       text NOT NULL CHECK (role IN ('owner','admin','finance','engineer','viewer')),
  scopes     text[] NOT NULL DEFAULT '{}',
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (org_id, user_id)
);
CREATE INDEX memberships_user_idx ON memberships(user_id);

CREATE TABLE api_tokens (
  id           uuid PRIMARY KEY,
  org_id       uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  name         text NOT NULL,
  prefix       text NOT NULL UNIQUE,
  hash         text NOT NULL,
  role         text NOT NULL CHECK (role IN ('owner','admin','finance','engineer','viewer')),
  scopes       text[] NOT NULL DEFAULT '{}',
  created_by   uuid REFERENCES users(id) ON DELETE SET NULL,
  expires_at   timestamptz,
  last_used_at timestamptz,
  revoked_at   timestamptz,
  created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX api_tokens_org_idx ON api_tokens(org_id);

CREATE TABLE audit_events (
  id          uuid PRIMARY KEY,
  org_id      uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  actor_type  text NOT NULL,
  actor_id    text NOT NULL,
  action      text NOT NULL,
  target_type text NOT NULL DEFAULT '',
  target_id   text NOT NULL DEFAULT '',
  ip          text NOT NULL DEFAULT '',
  user_agent  text NOT NULL DEFAULT '',
  details     jsonb NOT NULL DEFAULT '{}',
  at          timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX audit_events_org_at_idx ON audit_events(org_id, at DESC);

CREATE TABLE subscriptions (
  org_id                 uuid PRIMARY KEY REFERENCES organizations(id) ON DELETE CASCADE,
  plan                   text NOT NULL,
  status                 text NOT NULL DEFAULT 'trialing',
  stripe_customer_id     text UNIQUE,
  stripe_subscription_id text UNIQUE,
  current_period_end     timestamptz,
  trial_ends_at          timestamptz,
  updated_at             timestamptz NOT NULL DEFAULT now()
);

-- ---------------------------------------------------------------- connecteurs & inventaire
CREATE TABLE connectors (
  id               uuid PRIMARY KEY,
  org_id           uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  type             text NOT NULL,
  name             text NOT NULL,
  settings         jsonb NOT NULL DEFAULT '{}',
  secrets_enc      bytea,
  status           text NOT NULL DEFAULT 'pending',
  status_message   text NOT NULL DEFAULT '',
  interval_seconds integer NOT NULL DEFAULT 3600 CHECK (interval_seconds >= 60),
  enabled          boolean NOT NULL DEFAULT true,
  last_sync_at     timestamptz,
  last_success_at  timestamptz,
  webhook_token    text UNIQUE,
  created_at       timestamptz NOT NULL DEFAULT now(),
  updated_at       timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX connectors_org_idx ON connectors(org_id);

CREATE TABLE connector_runs (
  id           uuid PRIMARY KEY,
  org_id       uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  connector_id uuid NOT NULL REFERENCES connectors(id) ON DELETE CASCADE,
  kind         text NOT NULL,
  status       text NOT NULL,
  items        integer NOT NULL DEFAULT 0,
  error        text NOT NULL DEFAULT '',
  started_at   timestamptz NOT NULL,
  finished_at  timestamptz
);
CREATE INDEX connector_runs_conn_idx ON connector_runs(org_id, connector_id, started_at DESC);

CREATE TABLE resources (
  id           uuid NOT NULL,
  org_id       uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  connector_id uuid NOT NULL REFERENCES connectors(id) ON DELETE CASCADE,
  provider     text NOT NULL,
  type         text NOT NULL,
  external_id  text NOT NULL,
  name         text NOT NULL DEFAULT '',
  region       text NOT NULL DEFAULT '',
  attributes   jsonb NOT NULL DEFAULT '{}',
  labels       jsonb NOT NULL DEFAULT '{}',
  valid_from   timestamptz NOT NULL,
  valid_to     timestamptz,
  PRIMARY KEY (org_id, id, valid_from),
  CHECK (valid_to IS NULL OR valid_to > valid_from)
);
CREATE UNIQUE INDEX resources_current_uidx ON resources(org_id, id) WHERE valid_to IS NULL;
CREATE INDEX resources_org_type_idx ON resources(org_id, type) WHERE valid_to IS NULL;
CREATE INDEX resources_org_window_idx ON resources(org_id, valid_from, valid_to);
CREATE INDEX resources_labels_idx ON resources USING gin(labels);
CREATE INDEX resources_name_trgm_idx ON resources(org_id, lower(name));

CREATE TABLE resource_edges (
  org_id     uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  parent_id  uuid NOT NULL,
  child_id   uuid NOT NULL,
  relation   text NOT NULL,
  valid_from timestamptz NOT NULL,
  valid_to   timestamptz,
  PRIMARY KEY (org_id, parent_id, child_id, relation, valid_from)
);
CREATE INDEX resource_edges_child_idx ON resource_edges(org_id, child_id);
CREATE UNIQUE INDEX resource_edges_current_uidx ON resource_edges(org_id, parent_id, child_id, relation) WHERE valid_to IS NULL;

-- ---------------------------------------------------------------- tarification
CREATE TABLE price_catalogs (
  id          uuid PRIMARY KEY,
  org_id      uuid REFERENCES organizations(id) ON DELETE CASCADE, -- NULL = grille publique
  provider    text NOT NULL,
  version     text NOT NULL,
  source      text NOT NULL,
  currency    text NOT NULL DEFAULT 'EUR',
  valid_from  timestamptz NOT NULL,
  imported_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX price_catalogs_version_uidx ON price_catalogs(coalesce(org_id, '00000000-0000-0000-0000-000000000000'::uuid), provider, version);

CREATE TABLE price_items (
  catalog_id uuid NOT NULL REFERENCES price_catalogs(id) ON DELETE CASCADE,
  org_id     uuid REFERENCES organizations(id) ON DELETE CASCADE,
  sku        text NOT NULL,
  region     text NOT NULL DEFAULT '',
  unit       text NOT NULL,
  price      numeric(18,6) NOT NULL CHECK (price >= 0),
  currency   text NOT NULL,
  attributes jsonb NOT NULL DEFAULT '{}',
  PRIMARY KEY (catalog_id, sku, region)
);

CREATE TABLE onprem_cost_models (
  id                  uuid PRIMARY KEY,
  org_id              uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  name                text NOT NULL,
  connector_id        uuid REFERENCES connectors(id) ON DELETE SET NULL,
  selector            jsonb NOT NULL DEFAULT '{}',
  currency            text NOT NULL DEFAULT 'EUR',
  hardware_cost       numeric(18,6) NOT NULL DEFAULT 0,
  amortization_months integer NOT NULL DEFAULT 36 CHECK (amortization_months > 0),
  power_kw            numeric(18,6) NOT NULL DEFAULT 0,
  power_price_per_kwh numeric(18,6) NOT NULL DEFAULT 0,
  pue                 numeric(6,3) NOT NULL DEFAULT 1.5,
  licenses_monthly    numeric(18,6) NOT NULL DEFAULT 0,
  labor_monthly       numeric(18,6) NOT NULL DEFAULT 0,
  other_monthly       numeric(18,6) NOT NULL DEFAULT 0,
  capacity_vcpu       numeric(18,6) NOT NULL DEFAULT 0,
  capacity_ram_gb     numeric(18,6) NOT NULL DEFAULT 0,
  capacity_storage_gb numeric(18,6) NOT NULL DEFAULT 0,
  weight_cpu          numeric(6,4) NOT NULL DEFAULT 0.5,
  weight_ram          numeric(6,4) NOT NULL DEFAULT 0.4,
  weight_storage      numeric(6,4) NOT NULL DEFAULT 0.1,
  valid_from          timestamptz NOT NULL,
  valid_to            timestamptz,
  created_at          timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX onprem_cost_models_org_idx ON onprem_cost_models(org_id);

CREATE TABLE pricing_adjustments (
  id            uuid PRIMARY KEY,
  org_id        uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  kind          text NOT NULL CHECK (kind IN ('discount','commitment','credit')),
  name          text NOT NULL,
  provider      text NOT NULL DEFAULT '',
  sku_pattern   text NOT NULL DEFAULT '',
  percent       numeric(8,4),
  amount        numeric(18,6),
  covered_units numeric(18,6),
  currency      text NOT NULL DEFAULT 'EUR',
  valid_from    timestamptz NOT NULL,
  valid_to      timestamptz,
  created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX pricing_adjustments_org_idx ON pricing_adjustments(org_id);

-- Données de référence globales (non client) : pas de RLS.
CREATE TABLE exchange_rates (
  base  text NOT NULL,
  quote text NOT NULL,
  day   date NOT NULL,
  rate  numeric(18,8) NOT NULL CHECK (rate > 0),
  PRIMARY KEY (base, quote, day)
);

CREATE TABLE reconciliations (
  org_id       uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  connector_id uuid NOT NULL REFERENCES connectors(id) ON DELETE CASCADE,
  provider     text NOT NULL,
  month        date NOT NULL,
  estimated    numeric(18,6) NOT NULL,
  billed       numeric(18,6) NOT NULL,
  currency     text NOT NULL,
  computed_at  timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (org_id, connector_id, month)
);

-- ---------------------------------------------------------------- allocation
CREATE TABLE allocation_nodes (
  id         uuid PRIMARY KEY,
  org_id     uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  parent_id  uuid REFERENCES allocation_nodes(id) ON DELETE CASCADE,
  kind       text NOT NULL CHECK (kind IN ('organization','business_unit','team','service','environment')),
  name       text NOT NULL,
  path       text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX allocation_nodes_org_path_idx ON allocation_nodes(org_id, path text_pattern_ops);

CREATE TABLE allocation_rules (
  id         uuid PRIMARY KEY,
  org_id     uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  node_id    uuid NOT NULL REFERENCES allocation_nodes(id) ON DELETE CASCADE,
  name       text NOT NULL,
  priority   integer NOT NULL DEFAULT 100,
  conditions jsonb NOT NULL DEFAULT '[]',
  enabled    boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX allocation_rules_org_idx ON allocation_rules(org_id, priority);

CREATE TABLE shared_cost_rules (
  id         uuid PRIMARY KEY,
  org_id     uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  name       text NOT NULL,
  source     jsonb NOT NULL DEFAULT '[]',
  method     text NOT NULL CHECK (method IN ('proportional','fixed','weighted')),
  targets    jsonb NOT NULL DEFAULT '[]',
  enabled    boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE unit_metrics (
  id           uuid PRIMARY KEY,
  org_id       uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  name         text NOT NULL,
  unit_label   text NOT NULL,
  node_id      uuid REFERENCES allocation_nodes(id) ON DELETE SET NULL,
  source       text NOT NULL CHECK (source IN ('prometheus','api')),
  connector_id uuid REFERENCES connectors(id) ON DELETE SET NULL,
  query        text NOT NULL DEFAULT '',
  created_at   timestamptz NOT NULL DEFAULT now()
);

-- ---------------------------------------------------------------- budgets & alerting
CREATE TABLE notification_channels (
  id          uuid PRIMARY KEY,
  org_id      uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  kind        text NOT NULL CHECK (kind IN ('email','slack','teams','mattermost','webhook','pagerduty')),
  name        text NOT NULL,
  settings    jsonb NOT NULL DEFAULT '{}',
  secrets_enc bytea,
  enabled     boolean NOT NULL DEFAULT true,
  created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE budgets (
  id             uuid PRIMARY KEY,
  org_id         uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  node_id        uuid REFERENCES allocation_nodes(id) ON DELETE CASCADE,
  name           text NOT NULL,
  period         text NOT NULL CHECK (period IN ('monthly','quarterly','yearly')),
  amount         numeric(18,6) NOT NULL CHECK (amount > 0),
  currency       text NOT NULL,
  thresholds     integer[] NOT NULL DEFAULT '{50,80,100}',
  forecast_alert boolean NOT NULL DEFAULT true,
  channel_ids    text[] NOT NULL DEFAULT '{}',
  created_at     timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE alert_rules (
  id                   uuid PRIMARY KEY,
  org_id               uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  name                 text NOT NULL,
  kind                 text NOT NULL,
  config               jsonb NOT NULL DEFAULT '{}',
  channel_ids          text[] NOT NULL DEFAULT '{}',
  business_hours       jsonb,
  group_window_seconds integer NOT NULL DEFAULT 3600,
  enabled              boolean NOT NULL DEFAULT true,
  created_at           timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE alert_events (
  id          uuid PRIMARY KEY,
  org_id      uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  rule_id     uuid REFERENCES alert_rules(id) ON DELETE SET NULL,
  kind        text NOT NULL,
  severity    text NOT NULL,
  fingerprint text NOT NULL,
  title       text NOT NULL,
  body        text NOT NULL DEFAULT '',
  link        text NOT NULL DEFAULT '',
  payload     jsonb NOT NULL DEFAULT '{}',
  status      text NOT NULL,
  count       integer NOT NULL DEFAULT 1,
  first_at    timestamptz NOT NULL,
  last_at     timestamptz NOT NULL,
  notified_at timestamptz
);
CREATE INDEX alert_events_org_fp_idx ON alert_events(org_id, fingerprint, last_at DESC);

CREATE TABLE silences (
  id         uuid PRIMARY KEY,
  org_id     uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  matchers   jsonb NOT NULL DEFAULT '{}',
  starts_at  timestamptz NOT NULL,
  ends_at    timestamptz NOT NULL,
  reason     text NOT NULL DEFAULT '',
  created_by text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  CHECK (ends_at > starts_at)
);

-- ---------------------------------------------------------------- analytics
CREATE TABLE recommendations (
  id                       uuid PRIMARY KEY,
  org_id                   uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  type                     text NOT NULL,
  resource_id              uuid NOT NULL,
  fingerprint              text NOT NULL,
  title                    text NOT NULL,
  summary                  text NOT NULL DEFAULT '',
  savings_monthly          numeric(18,6) NOT NULL,
  currency                 text NOT NULL,
  risk                     text NOT NULL CHECK (risk IN ('low','medium','high')),
  status                   text NOT NULL CHECK (status IN ('open','accepted','postponed','dismissed','applied')),
  status_reason            text NOT NULL DEFAULT '',
  postponed_until          timestamptz,
  evidence                 jsonb NOT NULL DEFAULT '{}',
  remediation              jsonb NOT NULL DEFAULT '{}',
  applied_at               timestamptz,
  measured_savings_monthly numeric(18,6),
  created_at               timestamptz NOT NULL DEFAULT now(),
  updated_at               timestamptz NOT NULL DEFAULT now(),
  UNIQUE (org_id, fingerprint)
);
CREATE INDEX recommendations_org_status_idx ON recommendations(org_id, status, savings_monthly DESC);

CREATE TABLE anomalies (
  id                  uuid PRIMARY KEY,
  org_id              uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  series_key          text NOT NULL,
  kind                text NOT NULL CHECK (kind IN ('cost','usage')),
  title               text NOT NULL DEFAULT '',
  window_start        timestamptz NOT NULL,
  window_end          timestamptz NOT NULL,
  severity            text NOT NULL,
  expected            numeric(18,6) NOT NULL,
  actual              numeric(18,6) NOT NULL,
  currency            text NOT NULL DEFAULT '',
  score               double precision NOT NULL,
  correlated_events   jsonb NOT NULL DEFAULT '[]',
  explanation         text NOT NULL DEFAULT '',
  explanation_sources jsonb NOT NULL DEFAULT '[]',
  status              text NOT NULL DEFAULT 'open',
  created_at          timestamptz NOT NULL DEFAULT now(),
  UNIQUE (org_id, series_key, window_start)
);

CREATE TABLE forecasts (
  org_id       uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  node_id      text NOT NULL DEFAULT '',
  generated_at timestamptz NOT NULL,
  model        text NOT NULL,
  currency     text NOT NULL,
  points       jsonb NOT NULL,
  period_end   timestamptz NOT NULL,
  total        numeric(18,6) NOT NULL,
  lower        numeric(18,6) NOT NULL,
  upper        numeric(18,6) NOT NULL,
  PRIMARY KEY (org_id, node_id)
);

-- ---------------------------------------------------------------- uptime
CREATE TABLE uptime_checks (
  id               uuid PRIMARY KEY,
  org_id           uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  name             text NOT NULL,
  kind             text NOT NULL CHECK (kind IN ('http','tcp','icmp')),
  target           text NOT NULL,
  interval_seconds integer NOT NULL DEFAULT 60 CHECK (interval_seconds >= 30),
  timeout_ms       integer NOT NULL DEFAULT 10000,
  regions          text[] NOT NULL DEFAULT '{eu-west}',
  expected_status  integer NOT NULL DEFAULT 200,
  keyword          text NOT NULL DEFAULT '',
  node_id          uuid REFERENCES allocation_nodes(id) ON DELETE SET NULL,
  fail_threshold   integer NOT NULL DEFAULT 2,
  enabled          boolean NOT NULL DEFAULT true,
  created_at       timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE status_pages (
  id          uuid PRIMARY KEY,
  org_id      uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  slug        text NOT NULL UNIQUE,
  title       text NOT NULL,
  public      boolean NOT NULL DEFAULT true,
  access_hash text,
  check_ids   text[] NOT NULL DEFAULT '{}',
  branding    jsonb NOT NULL DEFAULT '{}',
  created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE incidents (
  id          uuid PRIMARY KEY,
  org_id      uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  check_id    uuid REFERENCES uptime_checks(id) ON DELETE SET NULL,
  title       text NOT NULL,
  status      text NOT NULL CHECK (status IN ('open','resolved')),
  source      text NOT NULL,
  started_at  timestamptz NOT NULL,
  resolved_at timestamptz,
  updates     jsonb NOT NULL DEFAULT '[]'
);
CREATE INDEX incidents_org_idx ON incidents(org_id, started_at DESC);

-- ---------------------------------------------------------------- rapports, exports, IA
CREATE TABLE reports (
  id         uuid PRIMARY KEY,
  org_id     uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  kind       text NOT NULL,
  period     text NOT NULL,
  status     text NOT NULL,
  object_key text NOT NULL DEFAULT '',
  summary    jsonb NOT NULL DEFAULT '{}',
  created_at timestamptz NOT NULL DEFAULT now(),
  sent_at    timestamptz,
  UNIQUE (org_id, kind, period)
);

CREATE TABLE export_jobs (
  id          uuid PRIMARY KEY,
  org_id      uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  name        text NOT NULL,
  format      text NOT NULL CHECK (format IN ('csv','parquet')),
  destination jsonb NOT NULL DEFAULT '{}',
  secrets_enc bytea,
  schedule    text NOT NULL CHECK (schedule IN ('daily','monthly')),
  last_run_at timestamptz,
  enabled     boolean NOT NULL DEFAULT true,
  created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE webhook_subscriptions (
  id         uuid PRIMARY KEY,
  org_id     uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  url        text NOT NULL,
  secret_enc bytea,
  events     text[] NOT NULL DEFAULT '{}',
  enabled    boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE llm_usage (
  id            uuid PRIMARY KEY,
  org_id        uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  at            timestamptz NOT NULL DEFAULT now(),
  feature       text NOT NULL,
  provider      text NOT NULL,
  model         text NOT NULL,
  input_tokens  integer NOT NULL DEFAULT 0,
  output_tokens integer NOT NULL DEFAULT 0,
  cost          numeric(18,6) NOT NULL DEFAULT 0,
  currency      text NOT NULL DEFAULT 'EUR'
);
CREATE INDEX llm_usage_org_at_idx ON llm_usage(org_id, at DESC);

-- ---------------------------------------------------------------- Row-Level Security
-- Politique générique « org_id = organisation courante » pour toutes les tables client.
DO $$
DECLARE t text;
BEGIN
  FOREACH t IN ARRAY ARRAY[
    'api_tokens','audit_events','subscriptions','connectors','connector_runs','resources',
    'resource_edges','onprem_cost_models','pricing_adjustments','reconciliations',
    'allocation_nodes','allocation_rules','shared_cost_rules','unit_metrics',
    'notification_channels','budgets','alert_rules','alert_events','silences',
    'recommendations','anomalies','forecasts','uptime_checks','status_pages','incidents',
    'reports','export_jobs','webhook_subscriptions','llm_usage'
  ] LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
    EXECUTE format(
      'CREATE POLICY tenant_isolation ON %I USING (org_id = kairn_current_org()) WITH CHECK (org_id = kairn_current_org())', t);
  END LOOP;
END $$;

-- Organisations : l'organisation courante, ses clientes (MSP) et celles de l'utilisateur courant.
ALTER TABLE organizations ENABLE ROW LEVEL SECURITY;
ALTER TABLE organizations FORCE ROW LEVEL SECURITY;
CREATE POLICY org_select ON organizations FOR SELECT USING (
  id = kairn_current_org()
  OR parent_org_id = kairn_current_org()
  OR EXISTS (SELECT 1 FROM memberships m WHERE m.org_id = organizations.id AND m.user_id = kairn_current_user())
);
CREATE POLICY org_insert ON organizations FOR INSERT WITH CHECK (id = kairn_current_org());
CREATE POLICY org_update ON organizations FOR UPDATE USING (id = kairn_current_org()) WITH CHECK (id = kairn_current_org());
CREATE POLICY org_delete ON organizations FOR DELETE USING (id = kairn_current_org());

-- Appartenances : celles de l'organisation courante et celles de l'utilisateur courant.
ALTER TABLE memberships ENABLE ROW LEVEL SECURITY;
ALTER TABLE memberships FORCE ROW LEVEL SECURITY;
CREATE POLICY membership_select ON memberships FOR SELECT USING (
  org_id = kairn_current_org() OR user_id = kairn_current_user()
);
CREATE POLICY membership_write ON memberships FOR ALL USING (org_id = kairn_current_org()) WITH CHECK (org_id = kairn_current_org());

-- Utilisateurs : soi-même et les membres de l'organisation courante.
ALTER TABLE users ENABLE ROW LEVEL SECURITY;
ALTER TABLE users FORCE ROW LEVEL SECURITY;
CREATE POLICY users_select ON users FOR SELECT USING (
  id = kairn_current_user()
  OR EXISTS (SELECT 1 FROM memberships m WHERE m.user_id = users.id AND m.org_id = kairn_current_org())
);
CREATE POLICY users_insert ON users FOR INSERT WITH CHECK (id = kairn_current_user());
CREATE POLICY users_update ON users FOR UPDATE USING (id = kairn_current_user()) WITH CHECK (id = kairn_current_user());

-- Grilles tarifaires : publiques (org_id NULL, écriture système) ou propres à l'organisation.
ALTER TABLE price_catalogs ENABLE ROW LEVEL SECURITY;
ALTER TABLE price_catalogs FORCE ROW LEVEL SECURITY;
CREATE POLICY catalogs_select ON price_catalogs FOR SELECT USING (org_id IS NULL OR org_id = kairn_current_org());
CREATE POLICY catalogs_write ON price_catalogs FOR ALL
  USING (org_id = kairn_current_org() OR (org_id IS NULL AND kairn_is_system()))
  WITH CHECK (org_id = kairn_current_org() OR (org_id IS NULL AND kairn_is_system()));

ALTER TABLE price_items ENABLE ROW LEVEL SECURITY;
ALTER TABLE price_items FORCE ROW LEVEL SECURITY;
CREATE POLICY items_select ON price_items FOR SELECT USING (org_id IS NULL OR org_id = kairn_current_org());
CREATE POLICY items_write ON price_items FOR ALL
  USING (org_id = kairn_current_org() OR (org_id IS NULL AND kairn_is_system()))
  WITH CHECK (org_id = kairn_current_org() OR (org_id IS NULL AND kairn_is_system()));

-- ---------------------------------------------------------------- accès transverses contrôlés
-- Chaque fonction ne renvoie que le strict nécessaire à son usage.

-- Authentification OIDC : retrouver un utilisateur par sujet ou e-mail.
CREATE OR REPLACE FUNCTION kairn_find_user(p_subject text, p_email text)
RETURNS SETOF users LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public AS $$
  SELECT * FROM users
  WHERE (p_subject IS NOT NULL AND oidc_subject = p_subject)
     OR (p_email IS NOT NULL AND lower(email) = lower(p_email))
  ORDER BY (oidc_subject = p_subject) DESC NULLS LAST
  LIMIT 1
$$;

-- Authentification par jeton d'API : recherche par préfixe (le hash est vérifié côté application).
CREATE OR REPLACE FUNCTION kairn_find_api_token(p_prefix text)
RETURNS SETOF api_tokens LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public AS $$
  SELECT * FROM api_tokens WHERE prefix = p_prefix AND revoked_at IS NULL LIMIT 1
$$;

-- Ordonnanceurs : liste des identifiants d'organisations.
CREATE OR REPLACE FUNCTION kairn_list_org_ids()
RETURNS SETOF uuid LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public AS $$
  SELECT id FROM organizations ORDER BY id
$$;

-- Webhooks entrants : connecteur associé à un jeton de webhook.
CREATE OR REPLACE FUNCTION kairn_find_connector_by_webhook(p_token text)
RETURNS TABLE(id uuid, org_id uuid, type text) LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public AS $$
  SELECT c.id, c.org_id, c.type FROM connectors c WHERE c.webhook_token = p_token AND c.enabled LIMIT 1
$$;

-- Pages de statut publiques : résolution du slug.
CREATE OR REPLACE FUNCTION kairn_find_status_page(p_slug text)
RETURNS TABLE(id uuid, org_id uuid, public boolean, access_hash text) LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public AS $$
  SELECT s.id, s.org_id, s.public, s.access_hash FROM status_pages s WHERE s.slug = p_slug LIMIT 1
$$;

-- Webhooks Stripe : organisation associée à un client Stripe.
CREATE OR REPLACE FUNCTION kairn_find_org_by_stripe_customer(p_customer text)
RETURNS SETOF uuid LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public AS $$
  SELECT org_id FROM subscriptions WHERE stripe_customer_id = p_customer LIMIT 1
$$;
