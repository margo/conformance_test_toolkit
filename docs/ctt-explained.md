# The Margo Conformance Test Toolkit, explained

This page explains what the toolkit (CTT) does, how the Margo `1.0.0-rc.3`
flow works, how the CTT is built around that flow, and what to say when
someone asks a question during a demo. It is written to be read from top to
bottom once; the setup steps themselves are in the two setup guides.

---

## 1. In one minute

Margo defines how a **device** and a **Workload Fleet Manager (WFM)** talk to
each other. A vendor builds one of the two. The CTT plays the other one, runs
real requests, and writes a report in which every test names the requirement
it is evidence for.

| You are testing | The CTT plays | Demo setup |
|---|---|---|
| a WFM (WFM Supplier) | a **WFM Client** that accesses the Margo Management Interface of the WFM | sandbox Symphony |
| a device (Device Supplier) | a **mock WFM** that the device connects to | sandbox device-agent |
| an Application Package (Application Supplier) | a validator that reads the package | — |

Two command-line tools drive it: `ctt-creator` makes test groups,
`ctt-runner` runs them.

---

## 2. The Margo rc.3 flow in plain words

### Who is involved

| Actor | What it is |
|---|---|
| **MIS** (Margo Identity Service) | issues identities and publishes the list of trusted issuers (the Trust Bundle) |
| **WFM** | tells devices what to run |
| **WFM Client** | the agent on the device that talks to the WFM |
| **Operator** | the person who issues identities and decides which clients a WFM accepts |

### Identity: a certificate instead of a login

Every WFM and every device gets an **X.509-SVID**: a certificate whose name is
a SPIFFE ID.

```
WFM         spiffe://<trust-domain>/margo/wfm/<wfm-id>
WFM Client  spiffe://<trust-domain>/margo/wfm/<wfm-id>/client/<wfm-client-id>
```

The client's ID starts with the WFM's ID. That is how each side knows the other
belongs to it. The operator issues these with the MIS before anything connects.
There is no onboarding call and no request signing any more; earlier versions
of the specification had both.

### The four phases

1. **Provisioning.** The operator issues the SVIDs with the MIS and adds the
   client to the local authorization policy of the WFM (in the sandbox: the SPIFFE ID allowlist).
2. **Trust.** Each side gets the Trust Bundle (its X.509 trust anchor is the SVID Root CA certificate of the MIS), either as a file or
   from the MIS.
3. **Mutual TLS.** When the device connects, both sides show their SVID. Each
   checks that the other's certificate comes from the MIS and carries the
   expected SPIFFE ID. If not, the connection is refused.
4. **Management Interface.** Over that connection the device makes these calls,
   again and again:

| Call | Purpose |
|---|---|
| `PUT /api/v1/capabilities/{deviceId}` | "this is what I am and what I can run" |
| `DELETE /api/v1/capabilities/{deviceId}` | unregister |
| `GET /api/v1/deployments` | "what should I be running?" — the desired-state manifest |
| `GET /api/v1/bundles/{digest}` | all deployment files in one archive |
| `GET /api/v1/deployments/{deploymentId}/{digest}` | one deployment file (YAML) |
| `POST /api/v1/deployments/{deploymentId}/status` | "this is how far I am" |

### The rules that the tests are about

- **The manifest is a table of contents.** It lists the bundle and each
  deployment by **digest** (`sha256:<hex>`), with a `manifestVersion` that only
  goes up.
- **Fetch by digest, then check.** The device fetches a file by its digest and
  must verify that the content really has that digest.
- **Reject what is wrong.** A manifest with an unknown digest algorithm, with a
  version that does not increase, or content that does not match its digest
  must not be applied; the device keeps its previous state.
- **`sizeBytes` is not a security check.** A wrong size with a correct digest
  must still be accepted.
- **Ask only for what changed.** The manifest has an `ETag` that is the digest
  of its body; a request with `If-None-Match` gets `304 Not Modified`.
- **Errors have one shape.** RFC 9457 problem details
  (`application/problem+json`) with a registered `type`.
- **Report status** for every change, with the manifest version that was
  adopted (`adoptedManifestVersion`).

---

## 3. How the CTT sits on that flow

```
            WFM Supplier persona                         Device Supplier persona

   CTT (WFM Client) ─── mTLS ───► WFM under test   Device under test ─── mTLS ──► CTT mock WFM
   (run_wfm_scenarios.js)         (Symphony)       (sandbox device-agent)         (bin/server)
          │                                                                            │
   sends each step,                                             records every request, judges it
   checks the answer                                            with assertions.json; `core` reads
          │                                                     the verdicts
          ▼                                                                            ▼
   reports/wfm-supplier/*.html                                  reports/device-supplier/*.html

                         both use SVIDs from the same MIS (sandbox: mis.sh)
```

The two personas are not symmetric, and this is the most important thing to
explain:

- A **WFM answers requests**, so the CTT can send any request it likes — also
  wrong ones — and check the answer.
- A **device only makes requests**. Nothing can be sent to it. So the mock WFM
  records what the device sends, changes what it serves (also to manifests the
  device must reject), and the test checks what the device then did or did not
  do.

---

## 4. What is in the repository

| Folder | What it holds |
|---|---|
| `ctt-creator/ctt-start.sh` | CLI 1: reads a folder of test cases and writes a group's `group.json` |
| `ctt-runner/ctt-start.sh` | CLI 2: identity setup, group selection, running, reports |
| `ctt-runner/wfm-supplier/scripts/run_wfm_scenarios.js` | the WFM Client of the CTT: runs scenario files and Postman collections against a WFM |
| `ctt-runner/device-supplier/scripts/cmd/device-supplier/` | the mock WFM server (Go) |
| `ctt-runner/device-supplier/scripts/` | the device test runner (Go): `run_tests.go`, `observe.go` |
| `ctt-runner/device-supplier/utils/manifests/assertions.json` | the rules the mock WFM applies to every request |
| `ctt-runner/app-supplier/scripts/` | the Application Package validator (Go) |
| `test-suites/<persona>/<group>/` | `group.json` and `test-cases/` of each group |
| `docs/` | setup guides, templates for writing tests, this page |

---

## 5. What happens when you run it

### 5.1 WFM Supplier — CTT as WFM Client

| Step | Function | What it does |
|---|---|---|
| Setup MIAF Identity | `wfm_setup_miaf_identity`, `_wfm_miaf_vendor` in `ctt-start.sh` | copies the client SVID, its key and the SVID Root CA certificate from the given folder; checks the chain and that it is a client SVID; keeps the SVID it replaces for the rotation test |
| Pick a group | `select_wfm_group` | lists groups with their descriptions |
| Start a run | `execute_wfm_tests_with_group` → `run_wfm_scenario_group` | installs the identity for the run (`wfm_install_run_identity`), selects the group's tests (`build_device_group_scenarios`), for the deployment scenario prints which client needs a deployment assigned in the WFM (the run does not call any WFM operator API) |
| Run the tests | `run_wfm_scenarios.js`: `runStep` → `performHTTPStep` → `request` | sends each step over mTLS with the client SVID |
| Check the WFM's identity | `recognizeWfm` | on every connection: X.509-SVID validated against the trust anchor, SPIFFE ID is the WFM this WFM Client belongs to |
| Check the answer | `runValidations` → `validate` | status code, then each validation (headers, body fields, digests) |
| Carry values forward | `extractContext`, `substitute` | saves values such as `deploymentId` and puts them into later steps |
| Postman collections | `parsePostmanCollection`, `convertPostmanItem` | turns a collection into the same kind of steps |
| Registry checks | `performOrasStep` | inspects the Application Package in the OCI registry with `oras` |
| Report | `writeReport` | HTML: requirement table, scenario summary, every step |

### 5.2 Device Supplier — CTT as mock WFM

| Step | Function | What it does |
|---|---|---|
| Setup Identity | `device_generate_certs` in `ctt-start.sh` | copies the mock WFM's SVID, the runner's client SVID and the SVID Root CA certificate; checks that the client belongs to the WFM |
| Start Mock WFM Server | `device_start_server` → `bin/server` | builds and starts the mock WFM: port 3003 (mTLS), port 3001 (health) |
| Accept a connection | `startMIAFServer`, `requireSVID`, `recognizeWFMClient` in `miaf_server.go` | client X.509-SVID validated against the trust anchor, exactly one SPIFFE URI, a client of this WFM |
| Serve the API | `handleMIAFPutCapabilities`, `…GetDeployments`, `…GetBundle`, `…GetDeploymentManifest`, `…PostStatus`, `…DeleteCapabilities` | the six rc.3 calls with correct ETag, digest and cache headers |
| Judge a request | `validateRequest`, `applyRule` in `main.go`, rules from `assertions.json` | a body that breaks a rule is answered with problem details; `device_expectations` rules are evaluated and recorded without rejecting |
| Record a request | `journaled`, `handleTestRequests` in `journal.go` | who sent what, what was answered, the verdicts |
| Serve something wrong on purpose | `applyNegativeFixture`, `tamper` in `negative_fixtures.go`; `handleTestServerIdentity` | wrong digest algorithm, lower version, wrong size, tampered content, an untrusted WFM certificate |
| Run a group | `device_run_tests` / `execute_device_tests` → `bin/run_tests` | `runStep` either sends a request (`executeStep`) or judges what the device sent (`runObserve` in `observe.go`) |
| Report | `generateHTMLReport` | HTML with PASS / FAIL / NOT RUN and requirement IDs (same template as the WFM report) |

Two kinds of device groups exist:

- **`core`** (and `management-interface`, `manifest-integrity`, `identity`,
  `observability`) judge a **real device**. Steps are `observe` steps.
- **`self-check`** (and `quick-check`, `random-order`) need **no device**: the
  CTT's own client talks to the mock WFM. They prove the setup and the mock WFM
  are in order.

### 5.3 Making a group — `ctt-creator`

`create_test_group` asks for a group name, the specification version, a
description and a folder. It collects the ID of every scenario, step and
Postman request in that folder and writes `group.json`. A group therefore
contains everything in its folder; a smaller group has its own folder with
fewer test cases.

---

## 6. The test data

| File | What it is |
|---|---|
| scenario file (`test-cases/*.json`) | scenarios, each a list of steps: request, expected status, validations, requirement IDs |
| Postman collection (WFM only) | the same, written in Postman; converted when it runs |
| `group.json` | name, version, description, test folder, test IDs — written by `ctt-creator` |
| `assertions.json` | the mock WFM's rules; loaded when the mock WFM starts — change a rule, restart, the verdict changes |

Every test step lists the requirement IDs it is evidence for (`crIds`). IDs
come from the published Margo requirement list. For points of rc.3 that have no
published ID yet, the suite uses its own: `…-MIAF-001` to `-004` (identity) and
`…-RC3-001`, `-002`. `docs/device-supplier-requirements-coverage.md` lists
every device requirement and the step that judges it.

---

## 7. The groups and what they show

| Group | Persona | Shows |
|---|---|---|
| `quick-check` | both | the setup works (seconds) |
| `core` | both | the conformance result |
| `management-interface` | both | the six API calls, accepted and rejected |
| `identity` | both | mTLS, refused identities, SVID rotation |
| `deployment-delivery` | WFM | a deployment delivered with correct digests |
| `postman-lifecycle`, `postman-error-handling` | WFM | tests written in Postman |
| `manifest-integrity` | Device | what a device must reject, what it must accept, state kept across a restart |
| `self-check`, `random-order` | Device | the CTT itself, no device needed |

Why groups: an author adds tests without touching the runner; a vendor picks
one name; the report states which group and version was run, so two vendors'
results can be compared; risky tests stay out of the default run.

---

## 8. The demo, in order

Results are from the full run of 11 October 2026 on a freshly built sandbox.

| # | What you show | Command / menu | Expected |
|---|---|---|---|
| 1 | Identities come from one MIS | `mis.sh generate-svid` | folders `x509svid-…` in `~/mis-deployment` |
| 2 | WFM: identity setup | WFM Supplier → 1 | "Identity setup complete" |
| 3 | WFM: quick check | WFM Supplier → 2 → `quick-check` | 5 of 6 (Symphony marks the manifest `immutable`) |
| 4 | WFM: full run on Symphony | `provision-multi-component.sh` once (assigns the test deployment), then → `core` | 52 steps: 39 passed, 11 failed, 2 not run (about 1 minute) |
| 5 | Device: identity, mock WFM, self-check | Device Supplier → 1, 2, 3 → `self-check` | 90 of 90 |
| 6 | Device: the sandbox device connects | `device-agent.sh docker start-docker`, `tail -f /tmp/wfm-server.log` | capabilities 201, deployments 200, status 200 |
| 7 | Device: full run on the sandbox device | → `core`, restart the device when asked | 53 passed, 3 failed, 5 not run (about 12 minutes) |
| 8 | A test author adds a group | `ctt-creator` → folder → `group.json` | the group appears in the runner's list |

The failures are the interesting part of the demo: they are real differences
between the reference implementations and rc.3, found by the suite. The guides
list each one.

---

## 9. Questions you may get

**Why do 11 tests fail on Symphony? Is the CTT wrong?**
No. Each failure is a point where that Symphony build differs from rc.3:
it accepts invalid capability values (201 instead of 422), answers a wrong
`Accept` with 400 instead of 406, marks the manifest `immutable`, uses an
`ETag` that is not the digest of the body, and builds manifest URLs with the
client ID in the path. The same tests pass against the CTT's own mock WFM.

**How can you test a device if you cannot send it anything?**
The mock WFM records every request. The test changes what the mock WFM serves
and then checks what the device sent, or did not send. Example: the mock WFM
serves a manifest with `sha512` digests; the device passes if it does not fetch
anything named in it and does not report status for it.

**What decides whether a device's request is valid?**
`assertions.json`, which the mock WFM loads at start. The test steps contain
no rules of their own; they read the mock WFM's verdict.

**What are the 3 failures of the sandbox device-agent?**
On a `sha512` manifest it still tries to fetch by that digest, and after
manifests it had to reject it sends their ETag back, which means it stored
them as its state.

**Why is a step NOT RUN, and not PASS or FAIL?**
Its precondition was missing: no SVID was rotated, the device was not
restarted, the device does not declare that role, or it never downloaded the
content the check is about. The reason is printed.

**How does a requirement map to a test?**
Each step lists requirement IDs; the report shows them next to the result and
in a coverage table. A step is evidence only for the IDs it lists itself.

**Which requirements are not tested?**
Device: the order of installation with `wait`, management by a single WFM,
and most observability internals; 12 listed requirements describe request
signing and no longer apply. WFM: see section 10. The coverage document lists
each one with the reason.

**How does a vendor or spec author add a test?**
Write a scenario (or, for a WFM, a Postman request), put the file in a folder,
run `ctt-creator`. No code change. The formats are in
`docs/margo-conformance-approach.md`.

**What changed from the earlier specification?**
The onboarding API, the client ID in every URL and per-request signatures
(RFC 9421) are gone. Identity is an X.509-SVID issued by the MIS, and every
call is mutual TLS. `docs/conformance-flows-and-miaf-migration.md` has the
details.

**Does the CTT run its own MIS?**
No. It uses SVIDs issued by the MIS of the trust domain; in the sandbox that
is `mis.sh`. For the plugfest one central MIS issues all of them.

**Does the CTT use the WFM's northbound API (NBI)?**
No. The NBI is outside the Margo specification and outside the CTT. Every check
runs on the Management Interface over mTLS. The deployment scenario needs a
deployment assigned to the CTT's client; that is an operator action, so the run
only says which client needs it and waits for the deployment to appear. For the
sandbox Symphony there is a separate helper,
`ctt-runner/wfm-supplier/utils/fixtures/provision-multi-component.sh`, run by
hand before the run. It uses Symphony's operator API on port 8082, exactly as
the sandbox `wfm-cli.sh` (Upload Application Package, Deploy Instance) does
through the `maestro` CLI. Other WFMs assign the deployment in their own way.

**Is the report signed?**
Yes. At the end of every run, for both personas, `ctt-runner/ctt-start.sh`
calls `ctt-runner/reports/scripts/pdf_ops.sh`, which turns the HTML report into a PDF and signs it
(`ctt-runner/reports/scripts/pdf_signer.py`: WeasyPrint for the PDF, pyHanko for the signature).
The files `..._signed.pdf` and `ca_cert.pem` appear next to the HTML report.
`ctt-runner/reports/scripts/pdf_verify.py` checks a signed report.

**What does the signature prove?**
That the PDF was not changed after the CTT wrote it, and that it was signed by
the certificate authority in `ca_cert.pem`. By default that authority is
created by the CTT on the machine that ran the tests (kept in
`ctt-runner/reports/scripts/signing-identity/`, the same one for every report of that machine).
So it proves integrity, not who ran the tests: whoever runs the CTT holds the
signing key. For a result that Margo itself vouches for, the run is signed with
a certificate Margo issues (`CTT_SIGN_CERT`, a `.p12` file).

**Do both personas produce the same report?**
Yes. `writeReport` in `run_wfm_scenarios.js` and `generateHTMLReport` in
`run_tests.go` write the same template: header (persona, group, versions,
system under test, run time), result line, Requirements Coverage, Scenario
Summary, Step Details. A requirement is FAIL when any step that checks it
failed, PASS when at least one passed and none failed, NOT RUN otherwise.

**Does it need the internet or a cloud service?**
No. It runs on one Linux machine. Internet is needed only to install the tools.

---

## 10. Alignment with rc.3 — where the suite stands

### Aligned and verified by runs

The six endpoints and their paths; mutual TLS with SVIDs in both directions and
the SPIFFE ID check; problem details with the registered types; the manifest,
status and deployment shapes of rc.3; ETag, digest and cache rules; status
states; `adoptedManifestVersion`; `assertions.json` matches the rc.3 schemas.

### Points of rc.3 that no test covers yet

| Area | Not covered |
|---|---|
| WFM | gateway and child devices (`deviceId` with `/`, `404 gateway-not-found`); `403` for a valid SVID that is not accepted or was retired; `retryable` / `Retry-After`; compression (`Accept-Encoding`, `Vary`); `manifestVersion` increasing after a change; an expired SVID; malformed JSON (`400`) |
| Device | installation order with `wait`; unregistering; gateway devices; reaction to `retryable` / `Retry-After`; Trust Bundle refresh; a Helm (K3s) device was not run |
| MIS | the Trust Bundle and discovery endpoints are not a tested persona |
| Application Supplier | the validator has not been brought to rc.3; both sample packages in the repository fail it; it has no groups |

### Left over from before rc.3

They do not affect results, but they will raise questions if someone reads the
code:

- The mock WFM still registers the onboarding and `/clients/{clientId}/…`
  routes and their signature checks.
- The mock WFM answers capability and status reports with a small body; rc.3
  defines none. The `self-check` tests look at it.
- Both runners still contain the request-signing code; no rc.3 step uses it.
- `ctt-runner/ctt-start.sh` still has the Newman path and the "CTT-generated
  identity" options; `ctt-creator` still has generators that are not on the
  menu, and `postman_collection_functional.json` is from the onboarding API.

### Things that can go wrong in a demo

| Risk | What to do |
|---|---|
| SVIDs issued before the MIS was rebuilt | issue new ones and run the identity setup again |
| `wfm.sh` option 1 run a second time | do not: it empties Symphony's client list and rebuilds Harbor |
| `mis.margo.org` pointing at `127.0.0.1` | use the machine's address, or the device-agent cannot start |
| a test file was changed but its group was not recreated | run `ctt-creator` for the group again; new steps are otherwise left out without a message |
| device `core` started on a mock WFM or device with old state | restart the mock WFM and start the device with empty state |
| Application Supplier shown live | not ready; leave it out |
