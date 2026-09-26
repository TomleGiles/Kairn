# Application web Kairn (Next.js, sortie standalone).
#   docker build -f apps/web/Dockerfile -t kairn/web .
FROM node:22-alpine AS deps
WORKDIR /app
COPY apps/web/package.json apps/web/package-lock.json ./
RUN npm ci --no-audit --no-fund

# Site de documentation (docs/*.md → HTML statique servi sous /docs).
FROM node:22-alpine AS docs
WORKDIR /repo
COPY apps/docs/package.json apps/docs/package-lock.json apps/docs/
RUN cd apps/docs && npm ci --no-audit --no-fund
COPY docs/ docs/
COPY CHANGELOG.md ./
COPY apps/docs/build.mjs apps/docs/
ARG DOCS_REPO_URL=""
RUN node apps/docs/build.mjs --out /out/docs --repo "${DOCS_REPO_URL}"

FROM node:22-alpine AS build
WORKDIR /app
ENV NEXT_TELEMETRY_DISABLED=1
# Les réécritures /api → API sont figées à la construction (développement et compose) ;
# en production, l'ingress route directement /api, /ingest et /scim vers l'API.
ARG KAIRN_API_URL=http://api:8080
ENV KAIRN_API_URL=${KAIRN_API_URL}
COPY --from=deps /app/node_modules ./node_modules
COPY apps/web/ ./
COPY --from=docs /out/docs ./public/docs
RUN npm run build

FROM node:22-alpine
WORKDIR /app
ENV NODE_ENV=production NEXT_TELEMETRY_DISABLED=1 PORT=3000 HOSTNAME=0.0.0.0
RUN addgroup -S -g 10001 kairn && adduser -S -u 10001 -G kairn kairn
COPY --from=build --chown=kairn:kairn /app/.next/standalone ./
COPY --from=build --chown=kairn:kairn /app/.next/static ./.next/static
COPY --from=build --chown=kairn:kairn /app/public ./public
USER 10001
EXPOSE 3000
CMD ["node", "server.js"]
