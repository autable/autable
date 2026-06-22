# syntax=docker/dockerfile:1

ARG GO_VERSION=1.26.4
ARG NODE_VERSION=24

FROM node:${NODE_VERSION}-bookworm-slim AS web-build
WORKDIR /src
COPY web/package*.json ./web/
RUN npm ci --prefix web
COPY web ./web
RUN npm --prefix web run build

FROM golang:${GO_VERSION}-bookworm AS go-build
WORKDIR /src
RUN apt-get update \
  && apt-get install -y --no-install-recommends build-essential ca-certificates \
  && rm -rf /var/lib/apt/lists/*
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web-build /src/web/dist ./internal/webui/dist
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILT_AT=unknown
ENV CGO_ENABLED=1
RUN go build \
  -ldflags "-s -w -X autable/internal/version.Version=${VERSION} -X autable/internal/version.Commit=${COMMIT} -X autable/internal/version.BuiltAt=${BUILT_AT}" \
  -o /out/autable \
  ./cmd/autable

FROM debian:bookworm-slim
RUN apt-get update \
  && apt-get install -y --no-install-recommends ca-certificates \
  && rm -rf /var/lib/apt/lists/* \
  && useradd --system --uid 10001 --create-home --home-dir /var/lib/autable autable \
  && mkdir -p /data /repository /etc/autable \
  && chown -R autable:autable /data /repository /etc/autable /var/lib/autable
COPY --from=go-build /out/autable /usr/local/bin/autable
COPY docker/config.yml /etc/autable/config.yml
USER autable
EXPOSE 8080
VOLUME ["/data", "/repository"]
ENTRYPOINT ["autable"]
CMD ["-config", "/etc/autable/config.yml"]
