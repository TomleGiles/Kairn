#!/bin/sh
# Initialisation du conteneur PostgreSQL de développement (docker compose).
set -eu
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname postgres \
  -v owner_password="${KAIRN_OWNER_PASSWORD:-kairn_owner}" \
  -v app_password="${KAIRN_APP_PASSWORD:-kairn_app}" \
  -v dbname=kairn \
  -f /kairn/init-roles.sql
