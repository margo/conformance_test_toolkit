FROM golang:1.24.4 AS builder

WORKDIR /app

COPY ctt-runner/device-supplier/go.mod ctt-runner/device-supplier/go.sum ./device-supplier/
COPY ctt-runner/app-supplier/go.mod ctt-runner/app-supplier/go.sum ./app-supplier/
RUN cd device-supplier && go mod download && cd ../app-supplier && go mod download

FROM ghcr.io/project-zot/zot-minimal-linux-amd64:v2.1.22 AS zot
FROM ghcr.io/oras-project/oras:v1.3.4 AS oras
FROM alpine/helm:3.15.1 AS helm

FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive \
    PATH=/usr/local/go/bin:$PATH \
    GOPATH=/root/go \
    GOTOOLCHAIN=local

RUN apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates curl jq openssl sudo apache2-utils nodejs npm python3 python3-venv \
        libpango-1.0-0 libpangocairo-1.0-0 libcairo2 libgdk-pixbuf2.0-0 libffi-dev shared-mime-info libglib2.0-0 \
        fontconfig fonts-noto-color-emoji \
    && rm -rf /var/lib/apt/lists/*

RUN npm install -g newman newman-reporter-htmlextra @apideck/portman@1.34.1 && npm cache clean --force

WORKDIR /ctt

RUN python3 -m venv ctt-runner/reports/scripts/.venv \
    && ctt-runner/reports/scripts/.venv/bin/pip install --no-cache-dir \
        weasyprint==70.0 pyhanko==0.37.0 cryptography==50.0.2 pikepdf==10.17.0

COPY --from=builder /usr/local/go /usr/local/go
COPY --from=builder /go/pkg/mod /root/go/pkg/mod
COPY --from=zot /usr/local/bin/zot-linux-amd64-minimal /usr/local/bin/zot
COPY --from=oras /bin/oras /usr/local/bin/oras
COPY --from=helm /usr/bin/helm /usr/local/bin/helm

COPY ctt-runner ./ctt-runner
COPY ctt-creator ./ctt-creator
COPY container/ctt-runner/zot-config.json /etc/zot/config.json
COPY container/ctt-runner/entrypoint.sh /usr/local/bin/ctt-entrypoint

EXPOSE 5443 3001 3003

ENTRYPOINT ["ctt-entrypoint"]
