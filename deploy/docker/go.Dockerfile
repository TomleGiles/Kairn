# Image des binaires Go de Kairn (API, ingestion, cost-engine, notifier, CLI, agent).
#   docker build -f deploy/docker/go.Dockerfile --build-arg PKG=./services/api --build-arg BIN=kairn-api -t kairn/api .
# Binaire statique, image distroless sans shell, utilisateur non root.
ARG GO_VERSION=1.26

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS build
ARG PKG=./services/api
ARG BIN=kairn-api
ARG VERSION=dev
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/app ${PKG}

FROM gcr.io/distroless/static-debian12:nonroot
ARG BIN=kairn-api
LABEL org.opencontainers.image.source="https://github.com/kairn-io/kairn" \
      org.opencontainers.image.licenses="Proprietary"
COPY --from=build /out/app /usr/local/bin/app
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/app"]
