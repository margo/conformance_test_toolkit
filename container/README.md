# CTT in containers

The CTT runs as two containers. The CTT code and test-suites are the same as in
the repository; nothing in `ctt-runner/` or `test-suites/` changes.

| Container | Image | What it does |
|---|---|---|
| `ctt-test-suites` | `margo-ctt-test-suites:dev` (`Dockerfile.test-suites`) | Copies `test-suites/` into a shared volume, then exits |
| `ctt-runner` | `margo-ctt-runner:dev` (`Dockerfile.runner`) | CTT code and scripts, all their tools, and the CTT's own OCI registry (Zot); stays running |

The runner image contains Go 1.24.4, Node.js and Newman, ORAS v1.3.4, Zot v2.1.22
(minimal binary, SHA-256 checked at build), jq, openssl, and the PDF signing
tools, so the CTT needs no installation step and no internet at run time.
The test-suites are mounted read-only at `/ctt/test-suites`.

## Start

Requires Docker with the Compose plugin. Run from this folder:

```bash
# CTT_REGISTRY_HOST: the IP or name other machines use to reach this host.
# It goes into the registry's TLS certificate.
CTT_REGISTRY_HOST=10.139.9.151 docker compose up -d --build
docker compose logs ctt-runner          # wait for "[ctt-entrypoint] Ready."
```

At start, the runner:

1. Creates the registry CA and TLS certificate once, in `ctt-data/zot/certs/`. The
   certificate is created again when `CTT_REGISTRY_HOST` changes.
2. Starts Zot on port 5443 with TLS and a login (default `ctt` / `margo-ctt`;
   set `CTT_REGISTRY_USER` and `CTT_REGISTRY_PASSWORD` to change it).
3. Trusts the registry CA, plus every `ctt-data/trusted-ca/*.crt`, and logs in.
4. Pushes the CTT reference package `library/margo-ctt-hello-world:1.0.0`, if it
   isn't there yet.

## Run the CTT

```bash
docker compose exec ctt-runner bash ctt-runner/ctt-start.sh
```

The menus are the same as without containers. Differences from the
[WFM Supplier guide](../docs/wfm-supplier-setup-guide.md):

- **Setup MIAF Identity:** copy `client-svid-cert.pem`, `client-svid-key.pem`
  and `mis-ca.crt` to `ctt-data/input/`, then enter `/input` as the folder.
- **Application Package (guide step 6):** already pushed to the CTT registry, and
  `REGISTRY_REF` points to it. `push.sh` is not needed.
- **Other commands** run inside the container, for example:
  ```bash
  docker compose exec ctt-runner bash ctt-runner/wfm-supplier/utils/fixtures/provision-multi-component.sh \
    --nbi-url https://<wfm-host>:8082/v1alpha2 --mi-url https://<wfm-host>:8084/v1alpha2/margo \
    --device-id wfm-core-device
  ```
- **A registry with a private CA** (for example the sandbox Harbor, or a vendor's
  registry): put its CA in `ctt-data/trusted-ca/<name>.crt` and run
  `docker compose restart ctt-runner`. The container does not see the host's
  trust store.

## What is kept on the host (`ctt-data/`)

| Path | Contents |
|---|---|
| `zot/certs/ca.crt` | Registry CA: give it to clients outside the container |
| `zot/certs/`, `zot/htpasswd` | Registry CA key, server certificate and key, login |
| `zot/storage/`, `zot/audit.log` | Registry content, and the log of who pushed and pulled what |
| `trusted-ca/` | Extra CAs the container trusts |
| `input/` | Files you give the CTT (SVIDs) |
| `wfm-identity/`, `device-certs/` | Identities installed by Setup MIAF Identity |
| `signing-identity/` | CA that signs the PDF reports |
| `reports/`, `reports-device/`, `reports-application/` | Reports (HTML, signed PDF, `ca_cert.pem`) |

`ctt-data/` holds private keys. It is in `.gitignore`, and `.dockerignore` keeps
keys and reports out of the images.

## Use the registry from another machine

```bash
scp <ctt-host>:<this folder>/ctt-data/zot/certs/ca.crt ./ctt-registry-ca.crt
oras pull --ca-file ctt-registry-ca.crt -u ctt -p margo-ctt \
  <ctt-host>:5443/library/margo-ctt-hello-world:1.0.0
```

Inside the container, the registry is `127.0.0.1:5443`, not `localhost:5443`,
because ORAS uses plain HTTP for `localhost`.

## Ports

| Port | Use |
|---|---|
| 5443 | CTT registry (Zot). Change the host side with `CTT_REGISTRY_PORT` |
| 3001 | Mock WFM of the Device Supplier persona |

## Stop and clean up

```bash
docker compose down                 # stop; ctt-data/ is kept
docker compose down -v              # also remove the test-suites volume
rm -rf ctt-data                     # remove registry data, identities and reports
```
