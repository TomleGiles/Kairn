# Image des services Python (analytics, ai).
#   docker build -f deploy/docker/python.Dockerfile --build-arg SERVICE=analytics --build-arg MODULE=kairn_analytics.app -t kairn/analytics .
FROM python:3.12-slim AS build
ARG SERVICE=analytics
ENV PIP_NO_CACHE_DIR=1 PIP_DISABLE_PIP_VERSION_CHECK=1
WORKDIR /src
COPY services/${SERVICE}/ ./
RUN python -m venv /opt/venv && /opt/venv/bin/pip install .

FROM python:3.12-slim
ARG MODULE=kairn_analytics.app
ARG SERVICE=analytics
ENV PATH=/opt/venv/bin:$PATH PYTHONUNBUFFERED=1 PYTHONDONTWRITEBYTECODE=1 KAIRN_MODULE=${MODULE}
# Polices Unicode pour les rapports PDF (service ai).
RUN apt-get update && apt-get install -y --no-install-recommends fonts-dejavu-core && rm -rf /var/lib/apt/lists/* \
    && useradd --uid 10001 --create-home --shell /usr/sbin/nologin kairn
COPY --from=build /opt/venv /opt/venv
USER 10001
# Point d'entrée commun : chaque module expose main() (uvicorn).
ENTRYPOINT ["sh", "-c", "exec python -c \"import importlib, os; importlib.import_module(os.environ['KAIRN_MODULE']).main()\""]
