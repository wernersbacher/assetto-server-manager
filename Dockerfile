# syntax=docker/dockerfile:1

# Optional build secret "ca_cert": an extra PEM CA certificate, needed when building behind a TLS-intercepting
# proxy. It is only mounted during the build steps and never ends up in the image. Examples:
#   docker build --secret id=ca_cert,src=C:\path\to\corporate-root.crt -t acpitwall .
#   EXTRA_CA_CERT=C:\path\to\corporate-root.crt docker compose build
# WITH_CA adds that certificate to the trusted roots (for curl, go, npm) of a RUN step. The combined bundle is written
# to /tmp, which those steps mount as tmpfs, so it doesn't end up in the image either.
ARG WITH_CA='if [ -s /run/secrets/ca_cert ]; then cat /etc/ssl/certs/ca-certificates.crt /run/secrets/ca_cert > /tmp/ca.pem; export SSL_CERT_FILE=/tmp/ca.pem CURL_CA_BUNDLE=/tmp/ca.pem NODE_EXTRA_CA_CERTS=/run/secrets/ca_cert; fi'

# ---------------------------------------------------------------------------------------------------------------------
FROM node:24 AS frontend
ARG WITH_CA

WORKDIR /src/cmd/acpitwall/typescript
COPY cmd/acpitwall/typescript/package.json cmd/acpitwall/typescript/package-lock.json cmd/acpitwall/typescript/.npmrc ./
RUN --mount=type=secret,id=ca_cert,required=false --mount=type=tmpfs,target=/tmp \
    eval "$WITH_CA" && npm ci --no-audit --no-fund

COPY cmd/acpitwall/typescript/ ./
RUN mkdir -p ../static && npx gulp build

# ---------------------------------------------------------------------------------------------------------------------
FROM golang:1.27 AS build
ARG WITH_CA
ARG SM_VERSION=unstable

WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=secret,id=ca_cert,required=false --mount=type=tmpfs,target=/tmp \
    eval "$WITH_CA" && go mod download && go install github.com/mjibson/esc@v0.2.0

COPY . .
COPY --from=frontend /src/cmd/acpitwall/static/ cmd/acpitwall/static/
RUN go generate ./... && \
    CGO_ENABLED=0 go build -trimpath \
        -ldflags="-s -w -X github.com/wernersbacher/ACPitwall.BuildVersion=${SM_VERSION}" \
        -o /out/acpitwall ./cmd/acpitwall

# ---------------------------------------------------------------------------------------------------------------------
FROM ubuntu:24.04 AS run
ARG WITH_CA

ENV DEBIAN_FRONTEND=noninteractive
ENV SERVER_USER=assetto
ENV SERVER_MANAGER_DIR=/home/${SERVER_USER}/acpitwall
ENV SERVER_INSTALL_DIR=${SERVER_MANAGER_DIR}/assetto
ENV LANG=C.UTF-8
ENV STEAMCMD_URL="https://steamcdn-a.akamaihd.net/client/installer/steamcmd_linux.tar.gz"
ENV STEAMROOT=/opt/steamcmd
ENV PATH="${STEAMROOT}:${PATH}"

# steamcmd + 32 bit libraries for the acServer and plugins (e.g. stracker, kissmyrank), nodejs for kissmyrank
RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates curl lib32gcc-s1 lib32stdc++6 lib32z1 zlib1g nodejs && \
    rm -rf /var/lib/apt/lists/*

# steamcmd updates itself on first start; doing it at build time keeps container start fast
RUN --mount=type=secret,id=ca_cert,required=false --mount=type=tmpfs,target=/tmp \
    eval "$WITH_CA" && mkdir -p ${STEAMROOT} && curl -fsSL ${STEAMCMD_URL} | tar -xz -C ${STEAMROOT} && \
    (steamcmd.sh +quit || true)

RUN useradd -ms /bin/bash ${SERVER_USER} && \
    mkdir -p ${SERVER_INSTALL_DIR} && \
    chown -R ${SERVER_USER}:${SERVER_USER} ${SERVER_MANAGER_DIR} ${STEAMROOT}

COPY --from=build /out/acpitwall /usr/bin/acpitwall

USER ${SERVER_USER}
WORKDIR ${SERVER_MANAGER_DIR}

# recommend volume mounting the entire assetto corsa directory
VOLUME ["${SERVER_INSTALL_DIR}"]
# http, udp plugin, acServer http
EXPOSE 8772
EXPOSE 9600
EXPOSE 8081

ENTRYPOINT ["acpitwall"]
