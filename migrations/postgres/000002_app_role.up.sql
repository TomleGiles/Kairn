-- Rôle applicatif : les services se connectent avec un rôle SANS BYPASSRLS,
-- distinct du rôle propriétaire qui exécute les migrations (et possède les
-- fonctions SECURITY DEFINER). Voir docs/adr/0002-isolation-multi-tenant-rls.md.
-- Le rôle est créé hors migration (deploy/postgres/init-roles.sql) ; s'il
-- n'existe pas (ex. tests), cette migration ne fait rien.
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'kairn_app') THEN
    GRANT USAGE ON SCHEMA public TO kairn_app;
    GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO kairn_app;
    GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA public TO kairn_app;
    ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO kairn_app;
    ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT EXECUTE ON FUNCTIONS TO kairn_app;
    -- Journal d'audit en ajout seul pour l'application.
    REVOKE UPDATE, DELETE ON audit_events FROM kairn_app;
    -- La table de suivi des migrations n'est pas accessible à l'application.
    IF EXISTS (SELECT 1 FROM pg_tables WHERE schemaname = 'public' AND tablename = 'schema_migrations') THEN
      REVOKE ALL ON schema_migrations FROM kairn_app;
    END IF;
  END IF;
END $$;
