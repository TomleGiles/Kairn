-- Rôles PostgreSQL de Kairn (exécuté une fois, par un superutilisateur, à l'initialisation).
--  - kairn_owner : propriétaire du schéma, exécute les migrations et possède les fonctions
--    SECURITY DEFINER ; BYPASSRLS est nécessaire pour ces seules fonctions (tables en FORCE RLS).
--  - kairn_app   : rôle de connexion des services ; NOBYPASSRLS, soumis à la RLS.
-- Les mots de passe sont fournis par des variables psql (-v), jamais écrits dans ce fichier.
\set ON_ERROR_STOP on
SELECT format('CREATE ROLE kairn_owner LOGIN BYPASSRLS PASSWORD %L', :'owner_password')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'kairn_owner') \gexec
SELECT format('CREATE ROLE kairn_app LOGIN NOBYPASSRLS PASSWORD %L', :'app_password')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'kairn_app') \gexec
SELECT format('CREATE DATABASE %I OWNER kairn_owner', :'dbname')
WHERE NOT EXISTS (SELECT 1 FROM pg_database WHERE datname = :'dbname') \gexec
\connect :dbname
ALTER SCHEMA public OWNER TO kairn_owner;
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
GRANT USAGE ON SCHEMA public TO kairn_app;
