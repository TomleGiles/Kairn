DROP FUNCTION IF EXISTS kairn_find_org_by_stripe_customer(text);
DROP FUNCTION IF EXISTS kairn_find_status_page(text);
DROP FUNCTION IF EXISTS kairn_find_connector_by_webhook(text);
DROP FUNCTION IF EXISTS kairn_list_org_ids();
DROP FUNCTION IF EXISTS kairn_find_api_token(text);
DROP FUNCTION IF EXISTS kairn_find_user(text, text);

DROP TABLE IF EXISTS llm_usage, webhook_subscriptions, export_jobs, reports, incidents,
  status_pages, uptime_checks, forecasts, anomalies, recommendations, silences, alert_events,
  alert_rules, budgets, notification_channels, unit_metrics, shared_cost_rules, allocation_rules,
  allocation_nodes, reconciliations, exchange_rates, pricing_adjustments, onprem_cost_models,
  price_items, price_catalogs, resource_edges, resources, connector_runs, connectors,
  subscriptions, audit_events, api_tokens, memberships, users, organizations CASCADE;

DROP FUNCTION IF EXISTS kairn_is_system();
DROP FUNCTION IF EXISTS kairn_current_user();
DROP FUNCTION IF EXISTS kairn_current_org();
