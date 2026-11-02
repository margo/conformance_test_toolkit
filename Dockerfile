# CTT container image: Ubuntu 24.04 with every tool the CTT CLIs need.
# The CTT code itself is not copied in; docker-compose.yaml mounts the repository at /ctt.

FROM ubuntu:24.04

ARG GO_VERSION=1.24.4
ARG ORAS_VERSION=1.1.0
ARG HELM_VERSION=3.15.1
ARG PORTMAN_VERSION=1.34.1
# Same UID/GID as the host user, so files the CTT writes to the repository stay owned by that user
ARG UID=1000
ARG GID=1000
# amd64 or arm64, set by docker build
ARG TARGETARCH

ENV DEBIAN_FRONTEND=noninteractive

# System tools, Node.js, Python and the libraries WeasyPrint needs for the signed PDF report
RUN apt-get update && apt-get install -y --no-install-recommends \
        bash ca-certificates curl git jq lsof openssl sudo iproute2 \
        nodejs npm \
        python3 python3-venv python3-pip \
        libpango-1.0-0 libpangocairo-1.0-0 libcairo2 libgdk-pixbuf2.0-0 \
        libffi-dev shared-mime-info libglib2.0-0 fontconfig fonts-noto-color-emoji \
    && rm -rf /var/lib/apt/lists/*

# Go, oras and helm
RUN curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-${TARGETARCH}.tar.gz" | tar -C /usr/local -xz \
    && curl -fsSL "https://github.com/oras-project/oras/releases/download/v${ORAS_VERSION}/oras_${ORAS_VERSION}_linux_${TARGETARCH}.tar.gz" \
        | tar -C /usr/local/bin -xz oras \
    && curl -fsSL "https://get.helm.sh/helm-v${HELM_VERSION}-linux-${TARGETARCH}.tar.gz" \
        | tar -C /usr/local/bin -xz --strip-components=1 "linux-${TARGETARCH}/helm"

# Portman for ctt-creator, newman for ctt-runner
RUN npm install -g "@apideck/portman@${PORTMAN_VERSION}" newman newman-reporter-htmlextra \
    && npm cache clean --force

# User "ctt" replaces the image's default "ubuntu" user. The CTT scripts call sudo, so it gets sudo without a password.
RUN userdel -r ubuntu 2>/dev/null || true \
    && groupadd -o -g "${GID}" ctt \
    && useradd -o -m -u "${UID}" -g "${GID}" -s /bin/bash ctt \
    && echo "ctt ALL=(ALL) NOPASSWD:ALL" > /etc/sudoers.d/ctt \
    && mkdir -p /ctt && chown ctt:ctt /ctt

USER ctt
ENV PATH=/usr/local/go/bin:/home/ctt/go/bin:$PATH \
    GOTOOLCHAIN=local

# Download the Go modules now, so the go build that runs during a test run needs no internet
COPY --chown=ctt:ctt ctt-runner/device-supplier/go.mod ctt-runner/device-supplier/go.sum /tmp/mods/device-supplier/
COPY --chown=ctt:ctt ctt-runner/app-supplier/go.mod ctt-runner/app-supplier/go.sum /tmp/mods/app-supplier/
RUN cd /tmp/mods/device-supplier && go mod download \
    && cd /tmp/mods/app-supplier && go mod download \
    && rm -rf /tmp/mods

# Python environment of the PDF signer (pdf_ops.sh), at the path that script uses.
# docker-compose.yaml keeps it in a volume, so it is not replaced by the mounted repository.
RUN python3 -m venv /ctt/ctt-runner/reports/scripts/.venv \
    && /ctt/ctt-runner/reports/scripts/.venv/bin/pip install --no-cache-dir \
        weasyprint==70.0 pyhanko==0.37.0 cryptography==50.0.2 pikepdf==10.17.0

WORKDIR /ctt
