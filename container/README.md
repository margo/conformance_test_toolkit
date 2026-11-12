# CTT in containers

| Container | Image | Dockerfile | What it does |
|---|---|---|---|
| `ctt-test-suites` | `ghcr.io/margo/margo.org/ctt-test-suites:latest` | `ctt-test-suites/Dockerfile` | Copies `test-suites/` into a shared volume, then exits with 0 |
| `ctt-runner` | `ghcr.io/margo/margo.org/ctt-runner:latest` | `ctt-runner/Dockerfile` | CTT code and tools, plus the CTT's own OCI registry (Zot); stays running |

`ctt-runner/Dockerfile` chains stages: `builder` (`golang:1.24.4`) downloads the Go
modules, and the `zot`, `oras` and `helm` stages are the projects' published images.
The Ubuntu 24.04 runtime copies from them with `COPY --from`. Nothing in
`ctt-runner/` or `test-suites/` changes.

## Start

```bash
cd container
CTT_REGISTRY_HOST=<IP or name of this host> docker compose up -d --build
docker compose ps -a
docker logs ctt-runner        # wait for "[ctt-entrypoint] Ready"
```

At start, `ctt-runner` creates the registry CA and certificate in `ctt-data/zot/certs/`,
starts Zot on port 5443 (login `ctt` / `margo-ctt`), trusts it and every
`ctt-data/trusted-ca/*.crt`, logs in, and pushes `library/margo-ctt-hello-world:1.0.0`.

## Run the CTT

```bash
docker compose exec ctt-runner bash ctt-runner/ctt-start.sh
```

- **Setup MIAF Identity:** copy `client-svid-cert.pem`, `client-svid-key.pem` and
  `mis-ca.crt` to `ctt-data/input/`, then enter `/ctt-data/input` as the folder.
- **Registry checks:** `REGISTRY_REF` points to the CTT registry, so `push.sh` is not needed.
- **Registry with a private CA** (for example the sandbox Harbor): put its CA in
  `ctt-data/trusted-ca/`, then run `docker compose restart ctt-runner`.
- **Other CTT commands** run the same way:
  `docker compose exec ctt-runner bash ctt-runner/wfm-supplier/utils/fixtures/provision-multi-component.sh ...`

## Kept on the host

| `ctt-data/` | Contents |
|---|---|
| `zot/certs/ca.crt` | Registry CA for clients on other hosts |
| `zot/` | Registry keys, login, storage, audit log |
| `input/`, `trusted-ca/` | SVID files and extra CAs you provide |
| `wfm-identity/` | Identity installed by Setup MIAF Identity |
| `signing-identity/` | CA that signs the PDF reports |
| `reports/` | WFM Supplier reports (HTML, signed PDF, `ca_cert.pem`) |

`ctt-data/` holds private keys. Git and `.dockerignore` both leave it out.

## Ports

| Port | Use |
|---|---|
| 5443 | CTT registry (Zot) |
| 3001, 3003 | Device Supplier mock WFM (HTTP, mTLS) |

Inside the container the registry is `127.0.0.1:5443`. It can't be `localhost`,
because ORAS uses plain HTTP for `localhost`.

## Stop

```bash
docker compose down                     # ctt-data/ is kept
docker compose down -v && rm -rf ctt-data
```
