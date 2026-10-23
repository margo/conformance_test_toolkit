#!/usr/bin/env node

const fs = require('fs');
const path = require('path');
const https = require('https');
const crypto = require('crypto');
const { execFileSync } = require('child_process');

function usage() {
  console.error(
    'Usage: node run_wfm_scenarios.js <base-url> <scenarios.json> <report.html> <cert-dir> [group-name] [group-version] [--miaf-url <url>]\n' +
    '\n' +
    '  <base-url>    WFM SBI URL; use the mTLS port for Symphony, e.g.:\n' +
    '    https://wfm.vendor.com:4443\n' +
    '    https://symphony.machine:8084/v1alpha2/margo   (Symphony mTLS SBI)\n' +
    '\n' +
    '  --miaf-url    Override URL for mtls:true steps (use only when WFM has separate\n' +
    '                mTLS and plain-TLS ports); defaults to <base-url> when omitted\n' +
    '\n' +
    '   or: node run_wfm_scenarios.js --curl <METHOD> <endpoint> --base-url <url> --cert-dir <dir> ' +
    '[--body <json>] [--header "Name: value"]... [--unsigned]\n' +
    '   or: node run_wfm_scenarios.js --fetch-trust-bundle <mis-base-url> <output-ca.pem>'
  );
  process.exit(2);
}

// --fetch-trust-bundle: the MIAF "one-time, at suite startup" step — fetch the
// WFM supplier's published trust bundle from their MIS and write it out as a CA
// PEM file, once, before any scenario runs. Not a per-test-case operation and not
// wired into the scenario loop at all: run this once (e.g. in CI setup, or by
// hand), then point a cert dir's svid-ca.pem at the output for every later run.
// No identity of our own is needed for this — MIS's discovery/bundle endpoints
// are unauthenticated by design (MIAF spec) — so this branches out before any
// cert loading happens below, unlike --curl mode.
const isTrustBundleMode = process.argv[2] === '--fetch-trust-bundle';
if (isTrustBundleMode) {
  fetchTrustBundleMode(process.argv.slice(3))
    .then((ok) => process.exit(ok ? 0 : 1))
    .catch((err) => { console.error(err.stack || err); process.exit(1); });
  return;
}

// --curl debug mode: sign and fire ONE ad-hoc request, print the equivalent curl
// command plus the raw response — for manually poking at a requirement on the CLI,
// the way you'd use Postman. Reuses signRequest/prepareContentDigest/request/
// injectCertificate below (hoisted function declarations) instead of a second,
// separate signing implementation. See runCurlMode() further down.
const isCurlMode = process.argv[2] === '--curl';

let baseUrlArg, scenariosFile, reportFile, certDir, groupName, groupVersion, curlArgs;
let miafUrlArg;
if (isCurlMode) {
  curlArgs = parseCurlArgs(process.argv.slice(3));
  baseUrlArg = curlArgs.baseUrl;
  certDir = curlArgs.certDir;
  if (!baseUrlArg || !certDir || !curlArgs.method || !curlArgs.endpoint) usage();
} else {
  const rawArgs = process.argv.slice(2);
  const miafIdx = rawArgs.indexOf('--miaf-url');
  if (miafIdx !== -1) {
    miafUrlArg = rawArgs[miafIdx + 1];
    rawArgs.splice(miafIdx, 2);
  }
  [baseUrlArg, scenariosFile, reportFile, certDir, groupName, groupVersion] = rawArgs;
  if (!baseUrlArg || !scenariosFile || !reportFile || !certDir) usage();
}

const baseUrl = baseUrlArg.replace(/\/+$/, '');
const miafUrl = (miafUrlArg || '').replace(/\/+$/, '') || baseUrl;
const privateKeyPath = path.join(certDir, 'device.key');
const deviceCertPath = path.join(certDir, 'device-cert.pem');
const caCertPath = path.join(certDir, 'ca-cert.pem');

// CTT Margo Version — the Margo spec version this conformance tool validates against
function readCttMargoVersion() {
  try {
    const specText = fs.readFileSync(path.join(__dirname, 'spec.yaml'), 'utf8');
    const match = specText.match(/^\s*version:\s*(\S+)/m);
    return match ? match[1] : 'unknown';
  } catch {
    return 'unknown';
  }
}
const cttMargoVersion = readCttMargoVersion();

// Compute SHA-256 hex thumbprint of PKIX DER public key — matches ComputeKeyIDFromPrivateKeyPEM
// in shared-lib/crypto/keyid.go so the WFM can correlate the keyid to the registered cert.
function computeKeyId(privateKeyPem) {
  try {
    const privObj = crypto.createPrivateKey(privateKeyPem);
    const pubDer = crypto.createPublicKey(privObj).export({ format: 'der', type: 'spki' });
    return crypto.createHash('sha256').update(pubDer).digest('hex');
  } catch {
    return 'device-key'; // fallback for unexpected key formats
  }
}

let privateKey = fs.readFileSync(privateKeyPath, 'utf8');
let deviceCertificate = fs.readFileSync(deviceCertPath, 'utf8');
let deviceCertificateBase64 = Buffer.from(deviceCertificate).toString('base64');
const caCertificate = fs.existsSync(caCertPath) ? fs.readFileSync(caCertPath) : undefined;

let keyid = computeKeyId(privateKey);

// MIAF (mTLS) identity — loaded only if present, so scenarios/environments that
// don't have one yet are completely unaffected. Same certDir as the RFC 9421
// identity above; separate files because it's a different credential (an
// X.509-SVID with a SPIFFE URI SAN), not a replacement for the old one.
// See wfm-supplier/fixtures/miaf/README.md for how to generate/replace it.
const svidCertPath = path.join(certDir, 'svid-cert.pem');
const svidKeyPath  = path.join(certDir, 'svid-key.pem');
const svidCaPath   = path.join(certDir, 'svid-ca.pem');
const miafIdentity = (fs.existsSync(svidCertPath) && fs.existsSync(svidKeyPath))
  ? {
      cert: fs.readFileSync(svidCertPath, 'utf8'),
      key: fs.readFileSync(svidKeyPath, 'utf8'),
      ca: fs.existsSync(svidCaPath) ? fs.readFileSync(svidCaPath, 'utf8') : caCertificate,
    }
  : null;

// The SVID this suite used before its client SVID was last re-issued, kept by
// Setup MIAF Identity. Only the SVID Rotation scenario uses it.
const svidPreviousCertPath = path.join(certDir, 'svid-previous-cert.pem');
const svidPreviousKeyPath  = path.join(certDir, 'svid-previous-key.pem');
const previousIdentity = (miafIdentity && fs.existsSync(svidPreviousCertPath) && fs.existsSync(svidPreviousKeyPath))
  ? {
      cert: fs.readFileSync(svidPreviousCertPath, 'utf8'),
      key: fs.readFileSync(svidPreviousKeyPath, 'utf8'),
      ca: miafIdentity.ca,
    }
  : null;

// "Recognition by the WFM Client" (MIAF WFM Identity Profile): the WFM this
// client may talk to is fixed by its own SVID — a client named
// spiffe://<trust-domain>/margo/wfm/<wfm-id>/client/<wfm-client-id> belongs to
// the WFM spiffe://<trust-domain>/margo/wfm/<wfm-id>, and the server's SVID
// must carry exactly that SPIFFE ID.
function spiffeIdOf(subjectAltName) {
  const match = /URI:(spiffe:\/\/[^,\s]+)/.exec(subjectAltName || '');
  return match ? match[1] : null;
}

const ownSpiffeId = miafIdentity ? spiffeIdOf(new crypto.X509Certificate(miafIdentity.cert).subjectAltName) : null;
const expectedWfmSpiffeId =
  ownSpiffeId && ownSpiffeId.includes('/client/') ? ownSpiffeId.slice(0, ownSpiffeId.lastIndexOf('/client/')) : null;

function recognizeWfm(peerCertificate) {
  if (!expectedWfmSpiffeId) {
    return new Error(
      `this suite's SVID (${ownSpiffeId || 'no SPIFFE ID'}) is not a WFM Client identity — ` +
      'expected spiffe://<trust-domain>/margo/wfm/<wfm-id>/client/<wfm-client-id>'
    );
  }
  const presented = spiffeIdOf(peerCertificate.subjectaltname);
  if (presented !== expectedWfmSpiffeId) {
    return new Error(`WFM presented ${presented || 'no SPIFFE ID'}, expected ${expectedWfmSpiffeId} (the WFM this client SVID belongs to)`);
  }
  return undefined;
}

// step.svid_source "untrusted": a certificate with the CTT's own SPIFFE ID that
// the MIS did not issue (self-signed, made here with openssl). A WFM must refuse it.
let untrustedIdentityCache = null;
function untrustedIdentity() {
  if (untrustedIdentityCache) return untrustedIdentityCache;
  const { execFileSync } = require('child_process');
  const dir = fs.mkdtempSync(path.join(require('os').tmpdir(), 'ctt-untrusted-'));
  const keyFile = path.join(dir, 'key.pem');
  const certFile = path.join(dir, 'cert.pem');
  try {
    execFileSync('openssl', ['ecparam', '-name', 'prime256v1', '-genkey', '-noout', '-out', keyFile], { stdio: 'ignore' });
    execFileSync('openssl', ['req', '-new', '-x509', '-days', '1', '-key', keyFile, '-out', certFile,
      '-subj', '/CN=ctt-untrusted-client',
      '-addext', `subjectAltName=URI:${ownSpiffeId}`,
      '-addext', 'basicConstraints=critical,CA:FALSE',
      '-addext', 'extendedKeyUsage=clientAuth'], { stdio: 'ignore' });
    untrustedIdentityCache = { cert: fs.readFileSync(certFile, 'utf8'), key: fs.readFileSync(keyFile, 'utf8'), ca: miafIdentity.ca };
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
  return untrustedIdentityCache;
}

// step.svid_source drives the SVID Rotation scenario: "previous" presents the
// SVID in use before the rotation, "refreshed" the current one. Without a kept
// previous SVID no rotation has happened, so there is nothing to test — the
// step is reported as NOT RUN instead of passing on the unrotated SVID.
// Returns the reason a step cannot run, or ''.
function svidSourceBlocker(step) {
  if (!step.svid_source || step.svid_source === 'untrusted') return '';
  if (!previousIdentity) {
    return 'no SVID rotation to test — re-issue the client SVID for the same SPIFFE ID and run Setup MIAF Identity again (it keeps the SVID it replaces)';
  }
  const previous = new crypto.X509Certificate(previousIdentity.cert);
  if (spiffeIdOf(previous.subjectAltName) !== ownSpiffeId) {
    return 'the kept pre-rotation SVID belongs to a different SPIFFE ID than the current SVID';
  }
  if (step.svid_source === 'previous' && new Date(previous.validTo) < new Date()) {
    return 'the pre-rotation SVID has expired';
  }
  return '';
}

// Identity/certs are loaded above exactly as the normal run does — curl mode exits
// here, before anything below that depends on a scenarios file (a bare top-level
// `return` is valid because Node wraps this file in a function).
if (isCurlMode) {
  runCurlMode(curlArgs)
    .then((failed) => process.exit(failed ? 1 : 0))
    .catch((err) => { console.error(err.stack || err); process.exit(1); });
  return;
}

// ─────────────────────────────────────────────────────────────────────────────
// Postman collection format detection and conversion
// ─────────────────────────────────────────────────────────────────────────────

function isPostmanCollection(data) {
  if (!data || typeof data !== 'object' || Array.isArray(data)) return false;
  if (!Array.isArray(data.item)) return false;
  // Standard Postman v2.1 / portman generated: info.schema points to getpostman.com
  if (data.info && typeof data.info.schema === 'string' && data.info.schema.includes('getpostman.com')) return true;
  // Postman export with info but without schema (older/different formats)
  if (data.info && typeof data.info === 'object' && (data.info.name || data.info._postman_id)) return true;
  // Portman/openapi-to-postman style: metadata lives in _ key instead of info
  if (data._ && typeof data._ === 'object' && data._.postman_id) return true;
  return false;
}

// A Postman collection is converted to one scenario of mTLS steps (rc.3 has no
// other transport). Postman variables become the runner's context placeholders:
// ":name" path segments and "{{name}}" anywhere turn into "{name}".
function postmanVarsToPlaceholders(value) {
  if (typeof value === 'string') return value.replace(/\{\{\s*([^}\s]+)\s*\}\}/g, '{$1}');
  if (Array.isArray(value)) return value.map(postmanVarsToPlaceholders);
  if (value && typeof value === 'object') {
    return Object.fromEntries(Object.entries(value).map(([k, v]) => [k, postmanVarsToPlaceholders(v)]));
  }
  return value;
}

// The spec names the path parameter "digest" on both the bundle and the
// deployment endpoint; they are different values, so they get different names.
// A ":name" segment takes the variable its Path Variables row refers to
// ("{{myDevice}}"), otherwise the variable called "name". A literal typed in
// that row is ignored: generated collections fill it with sample text.
function postmanSegToContextVar(seg, precedingPath, pathVariables) {
  const match = /^:(.+)$/.exec(seg) || /^\{\{\s*([^}\s]+)\s*\}\}$/.exec(seg);
  if (!match) return postmanVarsToPlaceholders(seg);
  const row = (pathVariables || []).find((v) => v.key === match[1]);
  const reference = /^\{\{\s*([^}\s]+)\s*\}\}$/.exec((row && row.value) || '');
  const name = reference ? reference[1] : match[1];
  if (name === 'digest') {
    return precedingPath[precedingPath.length - 1] === 'bundles' ? '{bundleDigest}' : '{deploymentDigest}';
  }
  return `{${name}}`;
}

// The request path after the base URL, as segments. Uses url.path when the
// export has it, otherwise the raw URL without "{{baseUrl}}" or scheme and host.
function postmanRequestPath(url) {
  if (url && typeof url === 'object' && Array.isArray(url.path) && url.path.length) return url.path;
  const raw = (typeof url === 'string' ? url : (url && url.raw) || '')
    .replace(/^\{\{[^}]+\}\}/, '')
    .replace(/^[a-z]+:\/\/[^/]+/i, '')
    .split('?')[0];
  return raw.split('/').filter(Boolean);
}

function postmanPathToEndpoint(pathArr, pathVariables) {
  return '/' + pathArr.map((seg, i) => postmanSegToContextVar(seg, pathArr.slice(0, i), pathVariables)).join('/');
}

// The JSON request body. A bare {{name}} used as a value (for example a number)
// is quoted first so the body parses; substitute() puts the typed value back.
function postmanRequestBody(req) {
  const raw = req.body && req.body.raw;
  if (!raw) return null;
  const parseable = raw.replace(/([:\[,]\s*)\{\{\s*([^}\s]+)\s*\}\}(\s*[,}\]])/g, '$1"{{$2}}"$3');
  try {
    return postmanVarsToPlaceholders(JSON.parse(parseable));
  } catch (_) {
    return null;
  }
}

// What the runner knows about the rc.3 endpoints.
//  - extract_context: values kept from a successful response, so later requests
//    can use {deploymentId}, {deploymentDigest}, {bundleDigest}, {manifestVersion}, {manifestEtag}.
//  - order, headers, request_body: used only for a request that documents several
//    responses (an API description, typically generated from the OpenAPI file).
//    Its sample values are not a usable request, so the runner sends this one
//    and places the endpoint in device lifecycle order.
const POSTMAN_ENDPOINT_RULES = {
  'PUT /api/v1/capabilities/{deviceId}': {
    order: 0,
    request_body: {
      properties: {
        id: '{deviceId}', vendor: 'Conformance Test Suite', modelNumber: 'CTS-POSTMAN-1', serialNumber: 'SN-POSTMAN-001',
        cpus: [{ cores: 4, architecture: 'arm64' }], memory: '16Gi', storage: '256Gi',
        interfaces: [{ type: 'ethernet' }], peripherals: [], otelCollector: false,
        supportedRuntimes: ['oci'], supportedDeploymentTypes: ['helm', 'compose'],
      },
    },
  },
  'GET /api/v1/deployments': {
    order: 1,
    headers: { Accept: 'application/vnd.margo.manifest.v1+json' },
    extract_context: {
      deploymentId: 'deployments.0.deploymentId',
      deploymentDigest: 'deployments.0.digest',
      bundleDigest: 'bundle.digest',
      manifestVersion: 'manifestVersion',
      manifestEtag: '_headers.etag',
    },
  },
  'GET /api/v1/deployments/{deploymentId}/{deploymentDigest}': { order: 2, headers: { Accept: 'application/yaml' } },
  'GET /api/v1/bundles/{bundleDigest}': { order: 3, headers: { Accept: 'application/vnd.margo.bundle.v1+tar+gzip' } },
  'POST /api/v1/deployments/{deploymentId}/status': {
    order: 4,
    request_body: {
      deploymentId: '{deploymentId}', adoptedManifestVersion: '{manifestVersion}',
      status: { state: 'installing' }, components: [],
    },
  },
  // Unregistering ends the device's session, so it comes last.
  'DELETE /api/v1/capabilities/{deviceId}': { order: 9 },
};
const POSTMAN_UNKNOWN_ENDPOINT_ORDER = 5;

const ZERO_DIGEST = 'sha256:' + '0'.repeat(64);

// For a request that documents several responses: how to change the request
// so the WFM should answer with this error. Returns null when a client cannot
// provoke it (for example 403 by WFM policy) — that example is reported NOT RUN.
function deriveErrorRequest(code, method, endpoint) {
  if (code === 304) {
    if (endpoint === '/api/v1/deployments') return { headers: { 'If-None-Match': '{manifestEtag}' } };
    if (endpoint.includes('{bundleDigest}')) return { headers: { 'If-None-Match': '"{bundleDigest}"' } };
    if (endpoint.includes('{deploymentDigest}')) return { headers: { 'If-None-Match': '"{deploymentDigest}"' } };
    return null;
  }
  if (code === 406) return { headers: { Accept: 'text/plain' } };
  // The success response of this request has already unregistered the device.
  if (code === 404 && method === 'DELETE' && endpoint.startsWith('/api/v1/capabilities/')) return {};
  if (code === 404 && /\{(bundle|deployment)Digest\}/.test(endpoint)) {
    return { endpoint: endpoint.replace(/\{(bundle|deployment)Digest\}/, ZERO_DIGEST) };
  }
  if (code === 422) {
    if (endpoint.includes('/capabilities/')) {
      return {
        request_body: {
          properties: {
            id: '{deviceId}', vendor: 'Conformance Test Suite', modelNumber: 'CTS-1', serialNumber: 'SN-1',
            supportedRuntimes: ['oci'], supportedDeploymentTypes: ['__invalid_deployment_type__'],
          },
        },
      };
    }
    if (endpoint.endsWith('/status')) {
      return {
        request_body: {
          deploymentId: '{deploymentId}', adoptedManifestVersion: '{manifestVersion}',
          status: { state: '__invalid_state__' }, components: [],
        },
      };
    }
  }
  return null;
}

function derivePostmanValidations(item) {
  const validations = [];
  const scripts = (item.event || [])
    .filter((e) => e.listen === 'test')
    .flatMap((e) => e.script?.exec || [])
    .join('\n');

  // Content-Type header check: ...headers.get("Content-Type")...to.include("value")
  const ctMatch = scripts.match(/headers\.get\(["']Content-Type["']\)[^"']*["']([^"']+)["']\)/);
  if (ctMatch) {
    validations.push({ field: '_headers.content-type', operation: 'contains', value: ctMatch[1] });
  }

  // Top-level schema fields: extract simple string/number properties
  const schemaMatch = scripts.match(/const schema = (\{[\s\S]*?\})\s*\n\n/);
  if (schemaMatch) {
    try {
      const schema = JSON.parse(schemaMatch[1]);
      for (const [key, def] of Object.entries(schema.properties || {})) {
        if (!Array.isArray(def.type)) {
          if (def.type === 'string') validations.push({ field: key, operation: 'is_string' });
          else if (def.type === 'number') validations.push({ field: key, operation: 'is_number' });
        }
      }
    } catch (_) { /* large or complex schemas may not parse — skip */ }
  }

  return validations;
}

function postmanTestScript(item) {
  return (item.event || [])
    .filter((e) => e.listen === 'test')
    .flatMap((e) => (e.script && e.script.exec) || [])
    .join('\n');
}

// The status a request without saved examples expects, read from the two usual
// Postman assertions: to.have.status(N) and expect([N, M]).to.include(pm.response.code).
function postmanStatusesFromScript(item) {
  const script = postmanTestScript(item);
  const one = /to\.have\.status\((\d{3})\)/.exec(script);
  if (one) return [Number(one[1])];
  const several = /expect\(\[([\d\s,]+)\]\)\.to\.(?:include|contain)\(pm\.response\.code\)/.exec(script);
  if (several) return several[1].split(',').map((n) => Number(n.trim())).filter(Boolean);
  return [];
}

// Requirement IDs of a request: its "crIds" field, otherwise every ID written in
// its name or description — the description survives a Postman import and export.
function postmanRequirementIds(item, resp) {
  const explicit = resp.crIds || resp.crids || item.crIds || item.crids;
  if (explicit) return explicit;
  const description = item.request && item.request.description;
  const text = [item.name, typeof description === 'object' && description ? description.content : description].join(' ');
  return [...new Set(text.match(/MARGO-[A-Z]+-[A-Z]+-\d{3}/g) || [])];
}

function postmanHasAssertions(item) {
  return /pm\.(expect|test)\(/.test(postmanTestScript(item));
}

// One request item becomes one step per saved example (or a single step when
// it has none). Besides the standard Postman fields, an item — or one of its
// examples — may carry "crIds", "validations", "extract_context",
// "accepted_statuses" and "expected_status", written exactly as in a scenario file.
function convertPostmanItem(item) {
  const req = item.request;
  const url = req.url && typeof req.url === 'object' ? req.url : {};
  const endpoint = postmanPathToEndpoint(postmanRequestPath(req.url), url.variable);
  const method = (req.method || 'GET').toUpperCase();
  const rules = POSTMAN_ENDPOINT_RULES[`${method} ${endpoint}`] || {};
  const itemId = item.id || String(item.name || 'request').trim().toLowerCase().replace(/\s+/g, '_');

  const examples = item.response || [];
  // Several saved responses: an API description, not a ready request (see POSTMAN_ENDPOINT_RULES).
  const isApiDescription = examples.length > 1;

  let headers = {};
  for (const h of req.header || []) {
    if (!h.disabled) headers[h.key] = postmanVarsToPlaceholders(h.value);
  }
  let request_body = postmanRequestBody(req);
  if (isApiDescription && rules.order !== undefined) {
    headers = { ...(rules.headers || {}) };
    request_body = rules.request_body || null;
  }

  const scriptStatuses = postmanStatusesFromScript(item);
  const variants = examples.length
    ? examples.map((resp) => ({ resp, code: resp.expected_status ?? resp.code ?? 200 }))
    : [{ resp: {}, code: item.expected_status ?? scriptStatuses[0] ?? 200 }];
  const isSuccess = (code) => code >= 200 && code < 300;
  const successCodes = variants.map((v) => v.code).filter(isSuccess);
  // A request's success response runs before its error responses.
  variants.sort((x, y) => Number(isSuccess(y.code)) - Number(isSuccess(x.code)));

  return variants.map(({ resp, code }) => {
    const step = {
      id: resp.id || (isApiDescription ? `${itemId}-${code}` : itemId),
      name: isApiDescription ? `${item.name} — ${resp.name || resp.status || code}` : item.name,
      crIds: postmanRequirementIds(item, resp),
      method,
      endpoint,
      headers: { ...headers },
      ...(request_body !== null ? { request_body } : {}),
      expected_status: code,
      mtls: true,
      validations: resp.validations || item.validations || (isSuccess(code) ? derivePostmanValidations(item) : []),
      extract_context: isSuccess(code) ? { ...(rules.extract_context || {}), ...(item.extract_context || {}) } : {},
    };

    // Other statuses that also pass: the author's field, the list in the test
    // script, or — when the API documents several success codes — any of those.
    let accepted = resp.accepted_statuses || item.accepted_statuses;
    if (!accepted && !examples.length && scriptStatuses.length > 1) accepted = scriptStatuses;
    if (!accepted && isApiDescription && isSuccess(code) && successCodes.length > 1) accepted = successCodes;
    accepted = (accepted || []).filter((c) => c !== code);
    if (accepted.length) step.accepted_statuses = accepted;

    // A lone error example is the author's own negative request and is sent as
    // written. Only a request that documents several responses needs deriving.
    if (!isSuccess(code) && isApiDescription) {
      const change = deriveErrorRequest(code, method, endpoint);
      if (!change) {
        step.not_run_reason =
          `the CTT cannot make the WFM answer ${code} from this API description — it depends on the WFM's own state or policy`;
      } else {
        if (change.endpoint) step.endpoint = change.endpoint;
        if (change.headers) Object.assign(step.headers, change.headers);
        if (change.request_body) step.request_body = change.request_body;
      }
    }
    return step;
  }).map((step) => Object.assign(step, { _order: isApiDescription ? (rules.order ?? POSTMAN_UNKNOWN_ENDPOINT_ORDER) : null }));
}

function parsePostmanCollection(collection) {
  const steps = [];
  let itemsWithScripts = 0;
  // Folders are walked in order; every request in them becomes steps of the one scenario.
  (function walk(items) {
    for (const item of items || []) {
      if (Array.isArray(item.item)) {
        walk(item.item);
      } else if (item.request) {
        if (postmanHasAssertions(item) && !item.validations) itemsWithScripts++;
        steps.push(...convertPostmanItem(item));
      }
    }
  })(collection.item);

  // Requests run in collection order. Only a collection of API descriptions is
  // put in lifecycle order, because its order is the order of the OpenAPI file.
  if (steps.length && steps.every((s) => s._order !== null)) {
    steps.sort((x, y) => x._order - y._order);   // stable: keeps each request's own steps together
  }
  steps.forEach((s) => delete s._order);

  // Collection variables are the starting values of {name} placeholders.
  const initial_context = {};
  for (const v of collection.variable || []) {
    if (v && v.key && v.key !== 'baseUrl' && v.value !== undefined && v.value !== '' && !v.disabled) {
      initial_context[v.key] = v.value;
    }
  }

  if (itemsWithScripts > 0) {
    console.log(
      ` ℹ  ${itemsWithScripts} request(s) have Postman test scripts. Postman scripts are not executed here: each ` +
      'request is checked against the status of its saved example(s) and its "validations" field.'
    );
  }

  const info = collection.info || {};
  const description =
    (typeof info.description === 'object' ? info.description.content : info.description) ||
    collection._?.description ||
    'Converted from a Postman collection';

  return [
    {
      id: info._postman_id || collection._?.postman_id || 'postman-collection',
      name: info.name || 'Postman Collection',
      description,
      initial_context,
      steps,
    },
  ];
}

// ─────────────────────────────────────────────────────────────────────────────

const rawData = JSON.parse(fs.readFileSync(scenariosFile, 'utf8'));
const scenarios = isPostmanCollection(rawData) ? parsePostmanCollection(rawData) : rawData;
let context = {};
const results = [];
const scenarioResults = [];
// scenario id → reason: set when a precondition of the scenario was not met
// (poll.on_timeout "skip_scenario"); its remaining steps are reported NOT RUN.
const scenarioBlockers = new Map();

function regenerateCertificate() {
  const { execSync } = require('child_process');
  const tempDeviceId = `device-${Date.now()}-${Math.floor(Math.random() * 100000)}`;
  execSync(`openssl ecparam -name prime256v1 -genkey -noout -out "${privateKeyPath}"`);
  execSync(
    `openssl req -new -x509 -days 365 -key "${privateKeyPath}" -out "${deviceCertPath}"` +
      ` -subj "/C=IN/ST=GGN/L=Sector48/O=Margo/OU=Conformance/CN=${tempDeviceId}"`
  );
  privateKey = fs.readFileSync(privateKeyPath, 'utf8');
  deviceCertificate = fs.readFileSync(deviceCertPath, 'utf8');
  deviceCertificateBase64 = Buffer.from(deviceCertificate).toString('base64');
  keyid = computeKeyId(privateKey);
}

// Fallback values for context vars that are empty or not yet set.
// These are used in URL path segments to produce a valid (but non-existent) URL
// instead of a double-slash like /deployments// which triggers redirects.
// A well-formed fake ID ensures the WFM returns a proper 404 rather than 301.
const CONTEXT_FALLBACKS = {
  deviceId:        'wfm-core-device',
  deploymentId:    'deployment-none-00000000',
  deploymentDigest:'sha256:0000000000000000000000000000000000000000000000000000000000000000',
  bundleDigest:    'sha256:0000000000000000000000000000000000000000000000000000000000000000',
};

function substitute(value) {
  if (typeof value === 'string') {
    // A value that is exactly one placeholder keeps the context value's own
    // type — a number extracted from a response is sent back as a number
    // (e.g. adoptedManifestVersion), not as its string form.
    const whole = /^\{([^}]+)\}$/.exec(value);
    if (whole) {
      const raw = context[whole[1]];
      if (raw !== undefined && raw !== null && typeof raw !== 'string') return raw;
    }
    return value.replace(/\{([^}]+)\}/g, (_, key) => {
      const val = context[key];
      if (val !== undefined && val !== null && val !== '') return String(val);
      return CONTEXT_FALLBACKS[key] ?? '';
    });
  }
  if (Array.isArray(value)) return value.map(substitute);
  if (value && typeof value === 'object') {
    return Object.fromEntries(Object.entries(value).map(([k, v]) => [k, substitute(v)]));
  }
  return value;
}

function getField(source, field) {
  if (field === '_body') return source._body;
  const parts = field.split('.');
  let current = source;
  let inHeaders = false;
  for (const part of parts) {
    if (current == null) return undefined;
    if (part === '_headers') { current = current._headers; inHeaders = true; continue; }
    if (inHeaders) {
      // Node.js lowercases all response header names; do case-insensitive lookup
      // so the JSON can use conventional capitalization like "ETag" or "Content-Type".
      const lower = part.toLowerCase();
      const key = Object.keys(current || {}).find((k) => k.toLowerCase() === lower);
      current = current ? current[key ?? part] : undefined;
      continue;
    }
    if (/^\d+$/.test(part)) { current = current[Number(part)]; continue; }
    current = current[part];
  }
  return current;
}

// Signs the request following RFC 9421 HTTP Message Signatures.
// Components signed: @method, @target-uri, @authority (always) + content-digest (when body present).
// @authority is required by the WFM verifier (shared-lib/crypto verifier.go WithComponents).
// Content-Digest is computed and stored in headers before this function is called.
function signRequest(method, url, headers, bodyText) {
  const parsedUrl = new URL(url);
  const authority = parsedUrl.host; // hostname:port

  const components = ['@method', '@target-uri', '@authority'];
  const lines = [
    `"@method": ${method.toUpperCase()}`,
    `"@target-uri": ${url}`,
    `"@authority": ${authority}`,
  ];

  // Include content-digest in signature only when it has a non-empty value.
  if (bodyText && headers['Content-Digest']) {
    components.push('content-digest');
    lines.push(`"content-digest": ${headers['Content-Digest']}`);
  }

  const created = Math.floor(Date.now() / 1000);
  const signatureParams =
    `(${components.map((c) => `"${c}"`).join(' ')});created=${created};keyid="${keyid}"`;
  lines.push(`"@signature-params": ${signatureParams}`);

  // Use ieee-p1363 dsaEncoding so ECDSA signatures are emitted as raw r||s (64 bytes for P-256)
  // rather than DER/ASN.1. The Go verifier (dsig.UnpackECDSASignature) checks len == keySize*2
  // and rejects DER-encoded signatures.
  const signatureBytes = crypto.sign('sha256', Buffer.from(lines.join('\n')), {
    key: privateKey,
    dsaEncoding: 'ieee-p1363',
  });

  headers['Signature-Input'] = `sig1=${signatureParams}`;
  headers.Signature = `sig1=:${signatureBytes.toString('base64')}:`;
}

// Prepares Content-Digest for any request that carries a body.
// If the step has already set Content-Digest (even to "") in its headers, that value is
// preserved so negative tests can exercise the empty-digest path.
function prepareContentDigest(headers, bodyText) {
  if (!bodyText) return;
  if (Object.prototype.hasOwnProperty.call(headers, 'Content-Digest')) return;
  const digest = crypto.createHash('sha256').update(bodyText).digest('base64');
  headers['Content-Digest'] = `sha-256=:${digest}:`;
}

// `mtls` is optional: { cert, key, ca } to present a client certificate for MIAF's
// mutual-TLS handshake. Omitted for every existing (RFC 9421 / server-TLS-only)
// call site — this only changes behavior for callers that opt in.
function request(method, url, headers, bodyText, mtls) {
  return new Promise((resolve) => {
    const parsedUrl = new URL(url);
    const options = {
      method,
      hostname: parsedUrl.hostname,
      port: parsedUrl.port || 443,
      path: parsedUrl.pathname + parsedUrl.search,
      headers,
      ca: (mtls && mtls.ca) || caCertificate,
      rejectUnauthorized: !!(mtls && mtls.ca),
      // X.509-SVIDs carry only a SPIFFE URI SAN, never a DNS/IP SAN, so the
      // server is identified by its SPIFFE ID instead of its hostname, on top
      // of the CA chain verification.
      ...(mtls && mtls.ca ? { checkServerIdentity: (_host, cert) => recognizeWfm(cert) } : {}),
      timeout: 30000,
    };
    if (mtls) {
      options.cert = mtls.cert;
      options.key = mtls.key;
    }

    const req = https.request(options, (res) => {
      const chunks = [];
      res.on('data', (chunk) => chunks.push(chunk));
      res.on('end', () => {
        const rawBody = Buffer.concat(chunks);
        resolve({ status: res.statusCode, headers: res.headers, body: rawBody.toString('utf8'), rawBody });
      });
    });

    req.on('timeout', () => {
      req.destroy(new Error('request timed out after 30s'));
    });
    req.on('error', (err) => {
      resolve({ status: 0, headers: {}, body: '', transportError: err.message });
    });

    if (bodyText) req.write(bodyText);
    req.end();
  });
}

// ─────────────────────────────────────────────────────────────────────────────
// --curl debug mode
// ─────────────────────────────────────────────────────────────────────────────

// Parses: --curl <METHOD> <endpoint> --base-url <url> --cert-dir <dir>
//         [--body <json>] [--header "Name: value"]... [--unsigned]
function parseCurlArgs(argv) {
  const out = { method: argv[0], endpoint: argv[1], headers: {}, unsigned: false };
  for (let i = 2; i < argv.length; i++) {
    const a = argv[i];
    if (a === '--base-url') out.baseUrl = argv[++i];
    else if (a === '--cert-dir') out.certDir = argv[++i];
    else if (a === '--body') out.body = argv[++i];
    else if (a === '--header') {
      const [name, ...rest] = (argv[++i] || '').split(':');
      if (name) out.headers[name.trim()] = rest.join(':').trim();
    } else if (a === '--unsigned') out.unsigned = true;
  }
  return out;
}

// Prints a curl command equivalent to the request this process is about to make,
// then makes that exact request and prints the raw response — signing it with
// the SAME signRequest()/prepareContentDigest() the scenario runner uses, so what
// you see here always matches real runner behaviour (no second signing path to
// keep in sync). Returns true if the request failed at the transport level.
async function runCurlMode(args) {
  const url = `${baseUrl}${args.endpoint}`;
  const headers = { ...args.headers };
  let bodyText = '';
  if (args.body !== undefined) {
    // Reuse injectCertificate's "./certs/device-cert.pem" marker when the body is
    // valid JSON; an intentionally-malformed body (negative testing) is sent as-is.
    try {
      const parsed = injectCertificate(JSON.parse(args.body), {});
      bodyText = JSON.stringify(parsed);
    } catch {
      bodyText = args.body;
    }
    headers['Content-Type'] = headers['Content-Type'] || 'application/json';
  }

  if (bodyText) prepareContentDigest(headers, bodyText);
  if (!args.unsigned) signRequest(args.method, url, headers, bodyText);

  const quote = (s) => `'${String(s).replace(/'/g, `'"'"'`)}'`;
  const curlLines = [`curl -sS -k -i -X ${args.method.toUpperCase()} ${quote(url)}`];
  for (const [k, v] of Object.entries(headers)) curlLines.push(`  -H ${quote(`${k}: ${v}`)}`);
  if (bodyText) curlLines.push(`  --data ${quote(bodyText)}`);
  console.log('\n── Equivalent curl command ──────────────────────────────────');
  console.log(curlLines.join(' \\\n'));

  console.log('\n── Sending request ──────────────────────────────────────────');
  const response = await request(args.method.toUpperCase(), url, headers, bodyText);
  if (response.transportError) {
    console.log(`✗ transport error: ${response.transportError}`);
    return true;
  }
  console.log(`HTTP ${response.status}`);
  for (const [k, v] of Object.entries(response.headers)) console.log(`${k}: ${v}`);
  console.log('');
  try {
    console.log(JSON.stringify(JSON.parse(response.body), null, 2));
  } catch {
    console.log(response.body);
  }
  console.log('');
  return false;
}

// Plain unauthenticated GET — no client cert, no signing. Used only by
// fetchTrustBundleMode below, which by design runs before any identity is
// loaded (MIS's discovery/bundle endpoints don't need one).
function simpleGet(url) {
  return new Promise((resolve) => {
    const u = new URL(url);
    const req = https.request(
      {
        method: 'GET',
        hostname: u.hostname,
        port: u.port || 443,
        path: u.pathname + u.search,
        headers: { Accept: 'application/json' },
        // rejectUnauthorized: false intentional here — simpleGet is only used
        // for the --fetch-trust-bundle setup command against MIS, where the MIS
        // HTTPS CA is not yet available to the caller.
        rejectUnauthorized: false,
        timeout: 15000,
      },
      (res) => {
        const chunks = [];
        res.on('data', (c) => chunks.push(c));
        res.on('end', () => resolve({ status: res.statusCode, body: Buffer.concat(chunks).toString('utf8') }));
      }
    );
    req.on('timeout', () => req.destroy(new Error('request timed out after 15s')));
    req.on('error', (err) => resolve({ status: 0, body: '', transportError: err.message }));
    req.end();
  });
}

// Base64 DER (as carried in a JWK's "x5c") -> PEM.
function derToPem(b64der) {
  const lines = b64der.match(/.{1,64}/g).join('\n');
  return `-----BEGIN CERTIFICATE-----\n${lines}\n-----END CERTIFICATE-----`;
}

// The MIAF "fetch the trust bundle once, at suite startup" step (Part 5.3
// Phase 2 / Part 7.1-B of conformance-flows-and-miaf-migration.md): discovery
// doc -> trust bundle -> write out the X.509 anchors as a CA PEM file that a
// later `svid-ca.pem` in a cert dir can just be a copy of. Deliberately a
// standalone one-shot command, not a scenario step — this is setup, run once,
// not repeated per test case.
async function fetchTrustBundleMode(argv) {
  const [misBaseUrl, outCaPath] = argv;
  if (!misBaseUrl || !outCaPath) {
    console.error('Usage: node run_wfm_scenarios.js --fetch-trust-bundle <mis-base-url> <output-ca.pem>');
    return false;
  }

  const discoveryUrl = `${misBaseUrl.replace(/\/+$/, '')}/.well-known/margo`;
  console.log(`Fetching discovery document: ${discoveryUrl}`);
  const discoveryResp = await simpleGet(discoveryUrl);
  if (discoveryResp.status !== 200) {
    console.error(`✗ discovery fetch failed: HTTP ${discoveryResp.status} ${discoveryResp.transportError || ''}`.trim());
    return false;
  }
  let discovery;
  try {
    discovery = JSON.parse(discoveryResp.body);
  } catch (err) {
    console.error(`✗ discovery document is not valid JSON: ${err.message}`);
    return false;
  }
  const { trustDomain, trustBundleUri } = discovery;
  if (!trustDomain || !trustBundleUri) {
    console.error(`✗ discovery document missing trustDomain/trustBundleUri: ${discoveryResp.body}`);
    return false;
  }
  if (!/^https:\/\//i.test(trustBundleUri)) {
    console.error(`✗ trustBundleUri MUST be https per MIAF spec, got: ${trustBundleUri}`);
    return false;
  }
  console.log(`  trustDomain:    ${trustDomain}`);
  console.log(`  trustBundleUri: ${trustBundleUri}`);

  console.log(`Fetching trust bundle: ${trustBundleUri}`);
  const bundleResp = await simpleGet(trustBundleUri);
  if (bundleResp.status !== 200) {
    console.error(`✗ trust bundle fetch failed: HTTP ${bundleResp.status} ${bundleResp.transportError || ''}`.trim());
    return false;
  }
  let bundle;
  try {
    bundle = JSON.parse(bundleResp.body);
  } catch (err) {
    console.error(`✗ trust bundle is not valid JSON: ${err.message}`);
    return false;
  }

  // Fail closed on zero anchors — required by the MIAF spec (Part 5.3, step 6).
  const anchors = (bundle.keys || []).filter(
    (k) => k.use === 'x509-svid' && Array.isArray(k.x5c) && k.x5c.length > 0
  );
  if (anchors.length === 0) {
    console.error('✗ trust bundle has zero x509-svid anchors — refusing to write a CA file (fail closed per MIAF spec)');
    return false;
  }

  const pem = anchors.map((k) => k.x5c.map(derToPem).join('\n')).join('\n');
  fs.writeFileSync(outCaPath, pem);
  console.log(`✓ wrote ${anchors.length} trust anchor(s) to ${outCaPath}`);
  console.log(`✓ trust domain: ${trustDomain}`);
  if (bundle.spiffe_sequence !== undefined) console.log(`  spiffe_sequence: ${bundle.spiffe_sequence}`);
  if (bundle.spiffe_refresh_hint !== undefined) console.log(`  spiffe_refresh_hint: ${bundle.spiffe_refresh_hint}`);
  return true;
}

// validate() is the runner's whole pass/fail rule engine: every single
// assertion in every scenario JSON file (an { field, operation, value } entry
// under a step's "validations" array) ends up as exactly one call here.
//
// Contract: return '' (empty string) when the check PASSES, or a short,
// human-readable failure message string when it FAILS. runStep() collects
// these messages and prints/reports whichever ones came back non-empty — so
// the return value doubles as both a boolean (falsy = pass) and the report
// text, which is why every branch below ends in a ternary rather than
// throwing or logging directly.
//
// `responseSource` is the parsed HTTP response for this step (body fields
// merged with `_headers`/`_body` for header/raw-body checks — see
// performHTTPStep/performOrasStep). `validation.field` is a dot-path into it
// (e.g. "bundle.digest" or "_headers.etag"), resolved via getField(); it may
// be absent for the "operation"-only semantic checks near the bottom of this
// switch (those inspect the whole response, not one field).
//
// Most `case` blocks are a single spec rule from the Margo Management
// Interface API (MI-xxx) or Application Registry (AR-xxx) requirements — the
// comment on each one names which requirement it's checking and why the
// check is shaped the way it is. Generic checks (exists/is_string/equals/...)
// are reusable building blocks any scenario step can combine; the named
// semantic ones (etag_is_body_digest, bundle_null_when_no_deployments, ...)
// encode a specific multi-field spec rule that a single field/value pair
// can't express on its own.
//
// To add a new kind of assertion: add a case here, then reference its
// operation name from any scenario JSON's "validations" array — no other
// code needs to change, since runStep() already loops over "validations" and
// calls this function generically for each entry.
// The exact bytes of a response body — digests are computed over these, never
// over a re-encoded string (a bundle archive is binary).
function exactBody(responseSource) {
  return responseSource._rawBody ?? Buffer.from(responseSource._body ?? '', 'utf8');
}

function validate(responseSource, validation) {
  // Field-less validations (the bundle_* semantic checks below) carry no `field`.
  const actual =
    validation.field == null ? undefined : getField(responseSource, validation.field);
  const expected = substitute(validation.value);

  switch (validation.operation) {
    case 'exists':
      return actual !== undefined && actual !== null ? '' : `${validation.field} is missing`;
    case 'is_string':
      return typeof actual === 'string' ? '' : `${validation.field} is not a string`;
    case 'is_number':
      return typeof actual === 'number' ? '' : `${validation.field} is not a number`;
    case 'is_array':
      return Array.isArray(actual) ? '' : `${validation.field} is not an array`;
    case 'is_object':
      return actual !== null && typeof actual === 'object' && !Array.isArray(actual)
        ? ''
        : `${validation.field} is not an object`;
    case 'gte':
      return Number(actual) >= Number(expected) ? '' : `${validation.field} expected >= ${expected}, got ${actual}`;
    case 'not_empty':
      return actual !== undefined && actual !== null && String(actual).length > 0
        ? ''
        : `${validation.field} is empty`;
    case 'equals':
      return actual === expected
        ? ''
        : `${validation.field} expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`;
    case 'contains':
      return String(actual ?? '').includes(String(expected))
        ? ''
        : `${validation.field} does not contain ${JSON.stringify(expected)}`;
    case 'array_length_gte':
      return Array.isArray(actual) && actual.length >= Number(expected)
        ? ''
        : `${validation.field} expected length >= ${expected}, got ${Array.isArray(actual) ? actual.length : typeof actual}`;
    case 'array_length_equals':
      return Array.isArray(actual) && actual.length === Number(expected)
        ? ''
        : `${validation.field} expected length === ${expected}, got ${Array.isArray(actual) ? actual.length : typeof actual}`;
    case 'one_of':
      return Array.isArray(expected) && expected.includes(actual)
        ? ''
        : `${validation.field} expected one of ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`;
    case 'matches_regex':
      // Uses the raw pattern, not the {context}-substituted `expected` — a
      // regex quantifier like {64} would otherwise be mistaken for a
      // context-variable placeholder by substitute() and silently stripped.
      return new RegExp(validation.value).test(String(actual ?? ''))
        ? ''
        : `${validation.field} value ${JSON.stringify(actual)} does not match pattern ${validation.value}`;
    case 'greater_than':
      return Number(actual) > Number(expected)
        ? ''
        : `${validation.field} expected > ${expected}, got ${actual}`;
    case 'not_contains':
      // Passes when the value is absent or does not include the substring.
      return !String(actual ?? '').includes(String(expected))
        ? ''
        : `${validation.field} must not contain ${JSON.stringify(expected)} (got ${JSON.stringify(actual)})`;
    case 'etag_is_body_digest': {
      // MI-015: the State Manifest ETag MUST be a strong validator computed as
      // sha256 of the exact serialized JSON response body.
      const rawEtag = getField(responseSource, '_headers.etag');
      if (rawEtag == null) return 'ETag header is missing';
      const bodyDigest = 'sha256:' + crypto.createHash('sha256').update(exactBody(responseSource)).digest('hex');
      const normalised = String(rawEtag).replace(/^W\//, '').replace(/^"|"$/g, '');
      return normalised === bodyDigest
        ? ''
        : `ETag ${JSON.stringify(rawEtag)} is not sha256 of the response body (expected ${bodyDigest}) (MI-015)`;
    }
    case 'body_sha256_equals': {
      // MI-025: the deployment/bundle digest advertised in the desired-state
      // manifest MUST be sha256 of the exact bytes served at the content-addressed
      // URL. `validation.value` is the expected digest (usually a {context} var
      // such as {deploymentDigest}); compared hex-to-hex, ignoring an optional
      // "sha256:" prefix on either side.
      const actualHex = crypto.createHash('sha256').update(exactBody(responseSource)).digest('hex');
      const expectedHex = String(expected ?? '').replace(/^sha256:/, '').toLowerCase();
      if (!expectedHex) return `${validation.field ?? 'digest'} expected value is empty`;
      return actualHex === expectedHex
        ? ''
        : `served body sha256 (${actualHex}) does not match the manifest digest (${expectedHex}) (MI-025)`;
    }
    case 'manifest_urls_are_content_addressed': {
      // rc.3 State Manifest: bundle.url is /api/v1/bundles/{bundle.digest} and
      // each deployments[].url is /api/v1/deployments/{deploymentId}/{digest}.
      const problems = [];
      const bundle = responseSource.bundle;
      if (bundle && bundle.url !== `/api/v1/bundles/${bundle.digest}`) {
        problems.push(`bundle.url is ${JSON.stringify(bundle.url)}, expected "/api/v1/bundles/${bundle.digest}"`);
      }
      (responseSource.deployments || []).forEach((ref, i) => {
        const wanted = `/api/v1/deployments/${ref.deploymentId}/${ref.digest}`;
        if (ref.url !== wanted) problems.push(`deployments[${i}].url is ${JSON.stringify(ref.url)}, expected "${wanted}"`);
      });
      return problems.join(' | ');
    }
    case 'has_application_description_layer': {
      // AR-002 (the one part every package MUST have, regardless of vendor):
      // a layer with the fixed Application Description media type MUST exist.
      // Ref-agnostic — doesn't need to know the vendor's file name.
      const layers = getField(responseSource, 'layers') || [];
      const found = layers.some((l) => l.mediaType === 'application/vnd.margo.app.description.v1+yaml');
      return found
        ? ''
        : 'no layer has mediaType "application/vnd.margo.app.description.v1+yaml" for the Application Description';
    }
    case 'layer_media_types_are_margo_specific': {
      // AR-004/011: every layer a package DOES ship MUST carry a Margo-specific
      // vendor media type, never a generic one (application/octet-stream etc.).
      // Ref-agnostic by design — doesn't assume which resource files a given
      // vendor's package includes or what they're named, only that whatever is
      // present is correctly typed.
      const layers = getField(responseSource, 'layers') || [];
      const generic = layers
        .filter((l) => !String(l.mediaType || '').startsWith('application/vnd.margo.app.'))
        .map((l) => `${(l.annotations && l.annotations['org.opencontainers.image.title']) || l.digest}: "${l.mediaType}"`);
      return generic.length === 0
        ? ''
        : `layer(s) with a non-Margo-specific mediaType: ${generic.join(', ')}`;
    }
    case 'bundle_null_iff_no_deployments':
    case 'bundle_null_when_no_deployments': {
      // MI-009: when there are zero deployments the `bundle` field MUST be
      // present with the value null (not omitted). JSON.parse keeps the key, so
      // `bundle === null` is true only for an explicit null; `undefined` (the
      // field absent) fails the check as it should.
      const deployments = getField(responseSource, 'deployments');
      const bundle = getField(responseSource, 'bundle');
      if (Array.isArray(deployments) && deployments.length === 0) {
        return bundle === null
          ? ''
          : 'bundle must be present and null when the deployments array is empty (MI-009)';
      }
      // With deployments there must be a bundle to fetch (an archive is never empty, MI-033).
      if (validation.operation === 'bundle_null_iff_no_deployments' && Array.isArray(deployments)) {
        return bundle && typeof bundle === 'object' && bundle.digest
          ? ''
          : `bundle must describe the archive when there are deployments (${deployments.length}), got ${JSON.stringify(bundle)}`;
      }
      return '';
    }
    case 'bundle_contains_exactly_deployments': {
      // MI-024: the bundle archive holds exactly the YAML files the manifest's
      // deployments[] refer to — compared by sha256 of each file in the tar.gz.
      const wanted = (Array.isArray(expected) ? expected : []).map((d) => (d && d.digest) || d).filter(Boolean).sort();
      if (!wanted.length) return 'no deployments[] from the manifest to compare with (value must be the saved deployments array)';
      let tar;
      try {
        tar = require('zlib').gunzipSync(exactBody(responseSource));
      } catch (e) {
        return `bundle is not a gzip archive: ${e.message}`;
      }
      const found = [];
      for (let off = 0; off + 512 <= tar.length;) {
        const header = tar.subarray(off, off + 512);
        if (header.every((b) => b === 0)) break;
        const size = parseInt(header.subarray(124, 136).toString('ascii').replace(/\0.*$/, '').trim() || '0', 8);
        const type = String.fromCharCode(header[156] || 48);
        if (type === '0' || type === '\0') {
          found.push('sha256:' + crypto.createHash('sha256').update(tar.subarray(off + 512, off + 512 + size)).digest('hex'));
        }
        off += 512 + Math.ceil(size / 512) * 512;
      }
      found.sort();
      return JSON.stringify(found) === JSON.stringify(wanted)
        ? ''
        : `bundle holds ${found.length} file(s) ${JSON.stringify(found.map((d) => d.slice(0, 19) + '…'))}, the manifest lists ${wanted.length} deployment(s) ${JSON.stringify(wanted.map((d) => d.slice(0, 19) + '…'))}`;
    }
    case 'bundle_media_type': {
      // MI-031: bundle.mediaType MUST be application/vnd.margo.bundle.v1+tar+gzip.
      // Skipped on a zero-deployment manifest, where bundle is null.
      const bundle = getField(responseSource, 'bundle');
      if (bundle == null) return '';
      return bundle.mediaType === 'application/vnd.margo.bundle.v1+tar+gzip'
        ? ''
        : `bundle.mediaType expected "application/vnd.margo.bundle.v1+tar+gzip", got ${JSON.stringify(bundle.mediaType)} (MI-031)`;
    }
    default:
      return `unsupported validation operation: ${validation.operation}`;
  }
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function parseBody(body) {
  if (!body) return {};
  try {
    return JSON.parse(body);
  } catch {
    return { _body: body };
  }
}

function injectCertificate(body, step) {
  if (!body || step.skip_certificate_injection) return body;
  if (body.certificate === './certs/device-cert.pem') {
    return { ...body, certificate: deviceCertificateBase64 };
  }
  return body;
}

// Returns the context keys that were newly set (so caller can display them).
function extractContext(responseSource, extractors) {
  const extracted = {};
  for (const [key, field] of Object.entries(extractors || {})) {
    const value = getField(responseSource, field);
    if (value !== undefined && value !== null) {
      context[key] = value;
      extracted[key] = value;
    }
  }
  return extracted;
}

function failureSummary(response, assertionFailures) {
  if (response.transportError) return response.transportError;
  if (assertionFailures.length > 0) return assertionFailures.join('; ');
  return '';
}

// ─────────────────────────────────────────────────────────────────────────────
// Console output helpers
// ─────────────────────────────────────────────────────────────────────────────

const COL = 72;
const THICK_LINE = '═'.repeat(COL);
const THIN_LINE  = '─'.repeat(COL);

const HTTP_STATUS_TEXT = {
  200: 'OK', 201: 'Created', 204: 'No Content', 304: 'Not Modified',
  400: 'Bad Request', 401: 'Unauthorized', 403: 'Forbidden',
  404: 'Not Found', 406: 'Not Acceptable', 409: 'Conflict',
  422: 'Unprocessable Entity', 500: 'Internal Server Error', 503: 'Service Unavailable',
};
function httpLabel(code) {
  return code > 0
    ? `${code} ${HTTP_STATUS_TEXT[code] || ''}`.trim()
    : 'NO RESPONSE (network error)';
}

function truncate(str, max) {
  const s = String(str ?? '');
  return s.length > max ? s.slice(0, max - 1) + '…' : s;
}

function printBanner() {
  const grp  = groupName    ? `Group: ${groupName}${groupVersion ? ` (v${groupVersion})` : ''}` : 'WFM Scenario Test';
  console.log('\n' + THICK_LINE);
  console.log(` Margo WFM Conformance Test Runner`);
  console.log(` ${grp}`);
  console.log(` WFM:   ${baseUrl}`);
  if (miafUrl !== baseUrl) console.log(` MIAF:  ${miafUrl}  (mtls:true steps)`);
  console.log(THICK_LINE);
}

function printScenarioHeader(scenario, index, total) {
  console.log('\n' + THIN_LINE);
  console.log(` SCENARIO ${index} of ${total}  ·  ${scenario.name}`);
  if (scenario.description) {
    // Word-wrap description at COL-2 chars
    const words = scenario.description.split(' ');
    let line = ' ';
    for (const w of words) {
      if (line.length + w.length + 1 > COL - 1) { console.log(line); line = ` ${w}`; }
      else { line += (line === ' ' ? '' : ' ') + w; }
    }
    if (line.trim()) console.log(line);
  }
  console.log(THIN_LINE);
}

function signingLabel(step, bodyText) {
  if (step.oras)              return '[oras / registry — no signing]';
  if (step.mtls)              return '[mTLS · X.509-SVID]';
  if (step.skip_signing)      return '[unsigned]';
  if (step.tamper_signature)  return '[bad-signature]';
  if (step.fresh_cert)        return '[fresh-cert · signed]';
  if (bodyText) return '[signed · content-digest]';
  return '[signed]';
}

function printStepResult(step, result, newContext, bodyText) {
  const pass = result.passed;
  const icon = pass ? '✓' : '✗';
  const tag  = pass ? 'PASS' : 'FAIL';
  const sig  = signingLabel(step, bodyText);

  console.log('');
  console.log(`  [${step.id}]  ${step.name}`);
  console.log(`   ▶  ${result.method.padEnd(6)} ${result.endpoint}  ${sig}`);
  if (result.crIds && result.crIds.length) console.log(`   ⚙  ${result.crIds.join(', ')}`);

  if (pass) {
    console.log(`   ${icon}  ${tag}  ${httpLabel(result.actual)}`);
    // Show any newly extracted context values (e.g., clientId from onboarding)
    for (const [key, val] of Object.entries(newContext)) {
      const display = truncate(String(val), 60);
      console.log(`        ↳ ${key} = "${display}"`);
    }
  } else {
    console.log(`   ${icon}  ${tag}  expected ${httpLabel(result.expected)}  ·  got ${httpLabel(result.actual)}`);
    const failures = result.reason.split('; ');
    for (const f of failures) {
      if (f.trim()) console.log(`        • ${f.trim()}`);
    }
  }
}

function printScenarioSummary(passed, total, skipped) {
  const failed = total - passed - skipped;
  const status = failed > 0 ? `✗ ${failed} failed` : skipped > 0 ? `⏭ ${skipped} not run` : '✓ all passed';
  console.log(`\n  Scenario result: ${passed}/${total} steps  ${status}`);
}

function printFinalSummary(allResults, scenarioResultsList, reportPath) {
  const totalPassed = allResults.filter((r) => r.passed).length;
  const totalSkipped = allResults.filter((r) => r.skipped).length;
  const totalFailed = allResults.length - totalPassed - totalSkipped;

  console.log('\n' + THICK_LINE);
  const grpLabel = groupName ? `Group: ${groupName}  ·  ` : '';
  console.log(` CONFORMANCE SUMMARY  ·  ${grpLabel}${allResults.length} tests`);
  console.log(` Claimed App Version: ${groupVersion || 'unknown'}  ·  CTT Margo Version: ${cttMargoVersion}`);
  if (groupVersion && groupVersion !== cttMargoVersion) {
    console.log(
      `\x1b[32m⚠ Version Mismatch: Claimed App Version (${groupVersion}) differs from CTT Margo Version (${cttMargoVersion})\x1b[0m`
    );
  }
  console.log(THICK_LINE);

  // Per-scenario table
  const nameW = Math.max(28, ...scenarioResultsList.map((s) => s.name.length));
  const header = ` ${'Scenario'.padEnd(nameW)}  ${'Steps'.padStart(5)}  ${'Passed'.padStart(6)}  ${'Failed'.padStart(6)}  ${'Not run'.padStart(7)}`;
  console.log('');
  console.log(header);
  console.log(' ' + THIN_LINE.slice(0, header.length - 1));
  for (const s of scenarioResultsList) {
    const fail = s.total - s.passed - s.skipped;
    console.log(
      ` ${s.name.padEnd(nameW)}  ${String(s.total).padStart(5)}  ${String(s.passed).padStart(6)}  ${String(fail).padStart(6)}  ${String(s.skipped).padStart(7)}`
    );
  }
  console.log(' ' + THIN_LINE.slice(0, header.length - 1));
  console.log(
    ` ${'TOTAL'.padEnd(nameW)}  ${String(allResults.length).padStart(5)}  ${String(totalPassed).padStart(6)}  ${String(totalFailed).padStart(6)}  ${String(totalSkipped).padStart(7)}`
  );

  const notRun = allResults.filter((r) => r.skipped);
  if (notRun.length > 0) {
    console.log('\n NOT RUN:');
    for (const r of notRun) {
      console.log(`\n  ⏭  ${r.step}  [${r.scenario}]  ${r.name}`);
      console.log(`     • ${r.reason}`);
    }
  }

  // List failed steps
  const failed = allResults.filter((r) => !r.passed && !r.skipped);
  if (failed.length > 0) {
    console.log('\n FAILED TESTS:');
    for (const r of failed) {
      console.log(`\n  ✗  ${r.step}  [${r.scenario}]  ${r.name}`);
      console.log(`     ${r.method} ${r.endpoint}`);
      console.log(`     Expected ${httpLabel(r.expected)}  ·  Got ${httpLabel(r.actual)}`);
      const parts = r.reason.split('; ');
      for (const p of parts) { if (p.trim()) console.log(`     • ${p.trim()}`); }
    }
  }

  console.log('');
  const relReport = path.relative(process.cwd(), reportPath);
  console.log(` Report: ${relReport}`);
  console.log('');
  const notRunNote = totalSkipped > 0 ? `  (${totalSkipped} not run)` : '';
  if (totalFailed === 0) {
    console.log(` ✅  ALL ${totalPassed} EXECUTED TESTS PASSED${notRunNote}`);
  } else {
    console.log(` ❌  ${totalFailed} of ${allResults.length} TESTS FAILED${notRunNote}`);
  }
  console.log(THICK_LINE + '\n');
}

// ─────────────────────────────────────────────────────────────────────────────
// Step execution
// ─────────────────────────────────────────────────────────────────────────────

// Sends a single HTTP request for a step (signing, Content-Digest, etc.) and
// returns the parsed response. Factored out of runStep so the polling loop
// below can re-issue the exact same request (with a fresh signature each
// time) on an interval.
async function performHTTPStep(step) {
  const method   = (step.method || 'GET').toUpperCase();
  const endpoint = substitute(step.endpoint || '');
  const url      = `${step.mtls ? miafUrl : baseUrl}${endpoint}`;
  const headers  = { ...(substitute(step.headers || {})) };
  const body =
    step.request_body === undefined
      ? undefined
      : injectCertificate(substitute(step.request_body), step);
  const bodyText = body === undefined ? '' : JSON.stringify(body);

  if (bodyText) headers['Content-Type'] = headers['Content-Type'] || 'application/json';

  // step.mtls: true → MIAF transport (mutual TLS with an X.509-SVID) instead of
  // RFC 9421 request signing. The old flow's signature/digest headers don't exist
  // in MIAF at all, so this branch skips them entirely rather than layering on top.
  if (step.mtls) {
    if (!miafIdentity) {
      throw new Error(
        `step "${step.id}" has mtls:true but no SVID cert/key found at ${svidCertPath} / ${svidKeyPath} — ` +
        `see wfm-supplier/fixtures/miaf/README.md`
      );
    }
    // mtls_wrong_server_ca: true → MIAF-002 negative test: present real client SVID
    // but verify the WFM's server cert against the wrong CA (device self-signed CA).
    // The WFM's SVID cannot chain to the device CA → TLS handshake fails → transport error.
    const ownIdentity = step.svid_source === 'previous' ? previousIdentity
      : step.svid_source === 'untrusted' ? untrustedIdentity()
      : miafIdentity;
    const identity = step.mtls_wrong_server_ca
      ? { cert: ownIdentity.cert, key: ownIdentity.key, ca: caCertificate }
      : ownIdentity;
    const response = await request(method, url, headers, bodyText, identity);
    const parsed = parseBody(response.body);
    const responseSource = { ...parsed, _headers: response.headers, _body: response.body, _rawBody: response.rawBody };
    return { method, endpoint, bodyText, response, responseSource };
  }

  // Prepare Content-Digest for body requests so the WFM doesn't reject due to missing
  // digest before it has a chance to check for the signature — unless the step is
  // specifically testing a missing/invalid Content-Digest, in which case it must be omitted.
  if (!step.skip_content_digest) prepareContentDigest(headers, bodyText);

  if (!step.skip_signing) signRequest(method, url, headers, bodyText);

  // "Signature verification failed" test: keep Signature-Input intact but flip one
  // character of the Signature value so the bytes no longer verify.
  if (step.tamper_signature && headers.Signature) {
    headers.Signature = headers.Signature.replace(/:([A-Za-z0-9+/=]+):/, (_m, b64) => {
      const chars = b64.split('');
      const i = Math.max(0, Math.floor(chars.length / 2));
      chars[i] = chars[i] === 'A' ? 'B' : 'A';
      return ':' + chars.join('') + ':';
    });
  }

  const response = await request(method, url, headers, bodyText);
  const parsed = parseBody(response.body);
  const responseSource = { ...parsed, _headers: response.headers, _body: response.body, _rawBody: response.rawBody };
  return { method, endpoint, bodyText, response, responseSource };
}

// Application Registry checks: `oras` (or a plain HTTP GET for the raw tags-list
// endpoint) instead of an HTTP call to the WFM. Shaped exactly like
// performHTTPStep's return value so the rest of runStep (poll/validations/
// extract_context/report) needs no changes at all.
//   step.oras = { cmd: 'manifest' | 'tags' | 'blobs' | 'tags_raw', ref? }
// `ref` defaults to the REGISTRY_REF environment variable (falling back to our
// own spec-conformant fixture package) so the same test-case file works for
// any vendor's registry without editing JSON, but also runs out-of-the-box.
const DEFAULT_REGISTRY_REF = 'harbor.machine:8443/library/margo-ctt-hello-world:1.0.0';
async function performOrasStep(step) {
  const ref = step.oras.ref ? substitute(step.oras.ref) : process.env.REGISTRY_REF || DEFAULT_REGISTRY_REF;
  const repo = ref.replace(/(:[^/]*)?$/, ''); // strip a trailing ":tag", keep any port's colon (comes before the last "/")
  let status = 200;
  let body = '';
  let transportError;

  try {
    if (step.oras.cmd === 'manifest') {
      body = execFileSync('oras', ['manifest', 'fetch', ref], { encoding: 'utf8' });
    } else if (step.oras.cmd === 'tags') {
      const out = execFileSync('oras', ['repo', 'tags', ref], { encoding: 'utf8' });
      body = JSON.stringify({ tags: out.split('\n').map((s) => s.trim()).filter(Boolean) });
    } else if (step.oras.cmd === 'blobs') {
      const manifest = JSON.parse(execFileSync('oras', ['manifest', 'fetch', ref], { encoding: 'utf8' }));
      const failedDigests = [];
      for (const layer of manifest.layers || []) {
        try {
          execFileSync('oras', ['blob', 'fetch', '--output', '/dev/null', `${repo}@${layer.digest}`]);
        } catch {
          failedDigests.push(layer.digest);
        }
      }
      body = JSON.stringify({ layerCount: (manifest.layers || []).length, failedDigests });
    }
  } catch (err) {
    transportError = (err.stderr && err.stderr.toString().trim()) || err.message;
  }

  if (step.oras.cmd === 'tags_raw') {
    // Raw OCI Distribution "Listing Tags" endpoint — AR-010 checks the exact
    // {name, tags[]} JSON shape, which `oras repo tags` re-formats away.
    const [host, ...rest] = repo.split('/');
    const response = await registryGet(host, `https://${host}/v2/${rest.join('/')}/tags/list`);
    const responseSource = { ...parseBody(response.body), _headers: response.headers, _body: response.body };
    return { method: 'GET', endpoint: ref, bodyText: '', response, responseSource };
  }

  const response = { status: transportError ? 0 : status, headers: {}, body, transportError };
  const responseSource = { ...parseBody(body), _headers: {}, _body: body };
  return { method: 'ORAS', endpoint: ref, bodyText: '', response, responseSource };
}

// GET against an OCI registry, following the standard token flow: a 401 with a
// "WWW-Authenticate: Bearer realm=...,service=...,scope=..." challenge means
// "fetch a token from realm, then retry". Uses the credentials `docker login` /
// `oras login` stored for this host when there are any, otherwise an anonymous
// token (enough for a public repository).
async function registryGet(host, url) {
  const first = await request('GET', url, {}, '');
  const challenge = first.headers['www-authenticate'] || '';
  if (first.status !== 401 || !/^Bearer /i.test(challenge)) return first;

  const params = Object.fromEntries([...challenge.matchAll(/(\w+)="([^"]*)"/g)].map((m) => [m[1], m[2]]));
  if (!params.realm) return first;
  const tokenUrl = new URL(params.realm);
  if (params.service) tokenUrl.searchParams.set('service', params.service);
  if (params.scope) tokenUrl.searchParams.set('scope', params.scope);

  const tokenHeaders = {};
  try {
    const dockerConfig = JSON.parse(fs.readFileSync(path.join(process.env.HOME || '', '.docker', 'config.json'), 'utf8'));
    const stored = dockerConfig.auths && dockerConfig.auths[host] && dockerConfig.auths[host].auth;
    if (stored) tokenHeaders.Authorization = `Basic ${stored}`;
  } catch (_) {
    // no stored credentials — anonymous token
  }

  const tokenResponse = await request('GET', tokenUrl.toString(), tokenHeaders, '');
  const token = (parseBody(tokenResponse.body) || {}).token || (parseBody(tokenResponse.body) || {}).access_token;
  if (!token) return first;
  return request('GET', url, { Authorization: `Bearer ${token}` }, '');
}

// Runs a list of validations against a response, returning an array of failure messages.
function runValidations(responseSource, validations) {
  const failures = [];
  for (const validation of validations || []) {
    const failure = validate(responseSource, validation);
    if (failure) failures.push(failure);
  }
  return failures;
}

// runStep() executes exactly one step of one scenario, end to end, and
// returns true/false for pass/fail. It's called once per step, in order,
// from the main scenario loop further down this file. At a high level it
// does five things, in this order:
//
//   1. (optional) swap in a "fresh_cert" identity for this one step only,
//      restored in the `finally` block no matter how the step ends —
//      used by negative tests that need an unrecognized signer.
//   2. send the actual request — either once, or repeatedly on an interval
//      if step.poll is set (waiting for some condition to become true,
//      e.g. an operator assigning a deployment via the WFM's own console
//      mid-test). This is delegated to performStep(), which is either
//      performHTTPStep (a normal signed/mTLS HTTP call) or performOrasStep
//      (an `oras` CLI call against an OCI registry) depending on whether
//      the step has an `oras` key — runStep() itself doesn't care which.
//   3. decide pass/fail on the HTTP status code, allowing for
//      step.accepted_statuses (a step can say "200 is what I really want,
//      but 404 is also fine here") in addition to step.expected_status.
//   4. if the status matched, run every entry in step.validations through
//      validate() and collect whatever failure messages come back.
//   5. if everything passed, pull values out of the response into the
//      shared `context` object (via step.extract_context) so that later
//      steps in the same scenario can reference them as {likeThis} in
//      their own endpoint/body/headers — this is how e.g. a GET step's
//      returned deploymentId gets threaded into a later POST step's URL.
//
// Whatever happened gets pushed onto the module-level `results` array (used
// for the final summary + HTML report) and printed immediately via
// printStepResult(), so failures are visible in the console output as soon
// as they happen rather than only in a report generated at the very end.
// Fills the two values a poll.instruct / poll.skip_reason text may refer to.
function fillInstruct(text) {
  return String(text)
    .replace(/\{cttClientSpiffeId\}/g, ownSpiffeId || "the CTT's client SPIFFE ID")
    .replace(/\{fixturesDir\}/g, path.resolve(__dirname, '..', 'utils', 'fixtures'));
}

async function runStep(scenario, step) {
  const blocker = step.not_run_reason || scenarioBlockers.get(scenario.id) || svidSourceBlocker(step);
  if (blocker) {
    const resultEntry = {
      scenario: scenario.id,
      scenarioName: scenario.name,
      step: step.id,
      name: step.name,
      crIds: step.crIds || [],
      method: (step.method || 'GET').toUpperCase(),
      endpoint: substitute(step.endpoint || ''),
      expected: step.expected_status,
      actual: '-',
      passed: false,
      skipped: true,
      reason: blocker,
    };
    results.push(resultEntry);
    console.log('');
    console.log(`  [${step.id}]  ${step.name}`);
    console.log(`   ⏭  NOT RUN  ${blocker}`);
    return 'skipped';
  }

  // fresh_cert: temporarily replace the global signing identity with a brand-new,
  // never-onboarded certificate so the WFM sees an unregistered signer.
  let savedCertState = null;
  if (step.fresh_cert) {
    const { execSync } = require('child_process');
    const tmpKey  = path.join(certDir, '.temp-fresh.key');
    const tmpCert = path.join(certDir, '.temp-fresh.pem');
    const tmpId   = `fresh-${Date.now()}`;
    try {
      execSync(`openssl ecparam -name prime256v1 -genkey -noout -out "${tmpKey}"`, { stdio: 'ignore' });
      execSync(
        `openssl req -new -x509 -days 1 -key "${tmpKey}" -out "${tmpCert}" -subj "/CN=${tmpId}"`,
        { stdio: 'ignore' }
      );
      savedCertState = { privateKey, deviceCertificate, deviceCertificateBase64, keyid };
      privateKey              = fs.readFileSync(tmpKey, 'utf8');
      deviceCertificate       = fs.readFileSync(tmpCert, 'utf8');
      deviceCertificateBase64 = Buffer.from(deviceCertificate).toString('base64');
      keyid                   = computeKeyId(privateKey);
    } catch (_) {
      // If temp cert generation fails, fall back to running without fresh-cert
    }
  }

  try {
    let method, endpoint, bodyText, response, responseSource;
    let pollTimedOut = false;
    let pollAttempts = 0;
    // A step with `oras: {...}` instead of `method`/`endpoint` checks the
    // Application Registry via the `oras` CLI (or a plain HTTP GET) rather than
    // the WFM's HTTP API — see performOrasStep(). Everything else (poll,
    // validations, extract_context, reporting) is unchanged.
    const performStep = step.oras ? performOrasStep : performHTTPStep;

    // step.poll = { interval_seconds, timeout_seconds, until: [validations] }
    // Re-issues this step's request on an interval until every validation in
    // `until` passes (e.g. "wait until the desired-state manifest lists >= 2
    // deployments", or "wait until it's back down to exactly 1") or the
    // timeout elapses — mirrors a real device-agent's state-seeking loop, and
    // lets a scenario wait on an operator making a change via the WFM's own
    // console mid-test (e.g. assigning or removing an app).
    //   poll.instruct   lines printed once while waiting: what the user has to
    //                   do in the WFM. {cttClientSpiffeId} and {fixturesDir}
    //                   are filled in.
    //   poll.on_timeout "skip_scenario": the wait is for a precondition the
    //                   user sets up, not for the WFM's own behaviour — on
    //                   timeout this step and the rest of the scenario are
    //                   NOT RUN (poll.skip_reason), not FAIL.
    if (step.poll) {
      const intervalSeconds = step.poll.interval_seconds ?? 5;
      const timeoutSeconds  = step.poll.timeout_seconds ?? 120;
      const deadline        = Date.now() + timeoutSeconds * 1000;
      const untilValidations = step.poll.until || [];

      for (;;) {
        pollAttempts++;
        ({ method, endpoint, bodyText, response, responseSource } = await performStep(step));

        const primaryMatchNow = response.status === step.expected_status;
        const pollFailures = primaryMatchNow
          ? runValidations(responseSource, untilValidations)
          : [`expected HTTP ${step.expected_status}, got ${response.status}`];

        if (pollFailures.length === 0) break;

        if (Date.now() >= deadline) {
          pollTimedOut = true;
          break;
        }

        if (pollAttempts === 1 && Array.isArray(step.poll.instruct)) {
          console.log('');
          console.log(`  [${step.id}]  ${step.name}`);
          for (const line of step.poll.instruct) console.log(`    👉 ${fillInstruct(line)}`);
          console.log('');
        }
        const secondsLeft = Math.max(0, Math.round((deadline - Date.now()) / 1000));
        console.log(
          `    ⏳ [poll attempt ${pollAttempts}] not ready yet (${pollFailures.join('; ')}) — retrying in ${intervalSeconds}s (waiting up to ${secondsLeft}s more)...`
        );
        await sleep(intervalSeconds * 1000);
      }

      if (pollTimedOut && step.poll.on_timeout === 'skip_scenario') {
        const reason = fillInstruct(
          step.poll.skip_reason || `precondition not met within ${timeoutSeconds}s — this scenario was not judged`
        );
        scenarioBlockers.set(scenario.id, reason);
        results.push({
          scenario: scenario.id,
          scenarioName: scenario.name,
          step: step.id,
          name: step.name,
          crIds: step.crIds || [],
          method,
          endpoint,
          expected: step.expected_status,
          actual: '-',
          passed: false,
          skipped: true,
          reason,
        });
        console.log('');
        console.log(`  [${step.id}]  ${step.name}`);
        console.log(`   ⏭  NOT RUN  ${reason}`);
        return 'skipped';
      }
    } else {
      ({ method, endpoint, bodyText, response, responseSource } = await performStep(step));
    }

    const assertionFailures = [];

    // expect_transport_error: true — a TLS rejection IS the passing outcome (or
    // an explicit HTTP rejection listed in accepted_statuses). Not being able to
    // reach the server at all proves nothing about how it handles TLS.
    if (step.expect_transport_error) {
      if (!response.transportError) {
        if (!(step.accepted_statuses || []).includes(response.status)) {
          assertionFailures.push(`expected the connection to be rejected but got HTTP ${response.status}`);
        }
      } else if (/ECONNREFUSED|ENOTFOUND|EAI_AGAIN|EHOSTUNREACH|ENETUNREACH|timed out/i.test(response.transportError)) {
        assertionFailures.push(`could not reach the WFM (${response.transportError}) — the TLS behaviour was not tested`);
      }
    } else {
      // A step passes if the actual status matches expected OR any of the accepted alternatives.
      const acceptedStatuses = step.accepted_statuses || [];
      const primaryMatch     = response.status === step.expected_status;
      const alternativeMatch = !primaryMatch && acceptedStatuses.includes(response.status);
      const statusMatch      = primaryMatch || alternativeMatch;
      if (!statusMatch) {
        assertionFailures.push(`expected HTTP ${step.expected_status}, got ${response.status}`);
      }

      // Run field validations only when the PRIMARY expected status is matched.
      // When we got an accepted alternative (e.g. 404 instead of 200 for a bundle step),
      // the response body belongs to a different content type — validating it against the
      // success schema would produce false negatives, so we skip it.
      if (primaryMatch) {
        assertionFailures.push(...runValidations(responseSource, step.validations));
      }

      if (pollTimedOut) {
        assertionFailures.push(
          `timed out after ${step.poll.timeout_seconds ?? 120}s waiting for poll.until condition (${pollAttempts} attempt(s))`
        );
      }
    }

    // Context is taken from any response with the expected status, even when one
    // of its validations failed: the IDs it carries are still real, and later
    // steps should test the WFM with them instead of failing on placeholders.
    let newContext = {};
    if (!step.expect_transport_error && !pollTimedOut && response.status === step.expected_status) {
      newContext = extractContext(responseSource, step.extract_context);
    }

    const passed = assertionFailures.length === 0 && (step.expect_transport_error || !response.transportError);
    const resultEntry = {
      scenario: scenario.id,
      scenarioName: scenario.name,
      step: step.id,
      name: step.name,
      crIds: step.crIds || [],
      method,
      endpoint,
      expected: step.expected_status,
      actual: response.status,
      passed,
      reason: failureSummary(response, assertionFailures),
    };
    results.push(resultEntry);

    printStepResult(step, resultEntry, newContext, bodyText);

    return passed;
  } finally {
    // Restore the original signing identity after fresh_cert steps
    if (savedCertState) {
      ({ privateKey, deviceCertificate, deviceCertificateBase64, keyid } = savedCertState);
      try { fs.unlinkSync(path.join(certDir, '.temp-fresh.key')); } catch (_) {}
      try { fs.unlinkSync(path.join(certDir, '.temp-fresh.pem')); } catch (_) {}
    }
  }
}

// ─────────────────────────────────────────────────────────────────────────────
// HTML report (unchanged logic, updated to use scenarioName)
// ─────────────────────────────────────────────────────────────────────────────

function htmlEscape(value) {
  return String(value ?? '')
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;');
}

// writeReport() builds one self-contained HTML file (inline CSS, no external
// assets) from `results` (one entry per step) and `scenarioResults`.
// The report template is shared with the device-supplier runner
// (generateHTMLReport in ctt-runner/device-supplier/scripts/run_tests.go):
// same title, header fields, sections, columns, wording and CSS. Change both
// together. The CLI turns this HTML into the signed PDF (ctt-runner/reports/scripts/pdf_ops.sh).
//
// Sections: header (persona, group, versions, system under test, run time),
// result line, Requirements Coverage, Scenario Summary, Step Details.
// A requirement is FAIL when any step that cites it failed, PASS when at
// least one such step passed and none failed, NOT RUN when none of them ran.
const REPORT_CSS = `
    @page { size: A4 landscape; margin: 12mm 12mm 20mm 12mm; } /* bottom: room for the signature stamp */
    body { font-family: Arial, sans-serif; margin: 24px; color: #1f2933; }
    h1 { font-size: 22px; margin-bottom: 4px; }
    h2 { font-size: 16px; margin: 24px 0 8px; }
    .meta { font-size: 13px; color: #555; margin-bottom: 18px; }
    .summary { margin-bottom: 10px; font-size: 15px; font-weight: bold; }
    .summary.all-pass { color: #166534; }
    .summary.has-fail { color: #b91c1c; }
    table { border-collapse: collapse; width: 100%; font-size: 13px; margin-bottom: 24px; }
    th, td { border: 1px solid #d7dde5; padding: 7px 9px; text-align: left; vertical-align: top; }
    th { background: #eef2f7; }
    td.wrap { overflow-wrap: anywhere; }
    /* Step Details: fixed column widths so all ten columns fit the page width in the PDF. */
    table.steps { table-layout: fixed; font-size: 11px; }
    table.steps th, table.steps td { padding: 5px 6px; }
    table.steps td { overflow-wrap: anywhere; }
    td.id, td.pass, td.fail, td.skip { white-space: nowrap; }
    td.pass { color: #166534; font-weight: 700; }
    td.fail, tr.fail td.reason { color: #b91c1c; font-weight: 700; }
    td.skip, tr.skip td.reason { color: #6b7280; font-weight: 700; }
    .version-warning { margin-bottom: 14px; padding: 10px 14px; border-radius: 4px; background: #dcfce7; color: #166534; border: 1px solid #86efac; font-size: 13px; font-weight: bold; }
  `;

const NO_REQUIREMENTS_NOTE = 'The tests of this group carry no requirement IDs.';

function writeReport() {
  const passed = results.filter((r) => r.passed).length;
  const skipped = results.filter((r) => r.skipped).length;
  const failed = results.length - passed - skipped;
  const persona = 'WFM Supplier';
  const grpLabel = htmlEscape(groupName || 'unknown');
  const state = (r) => (r.passed ? 'pass' : r.skipped ? 'skip' : 'fail');
  const word = { pass: 'PASS', fail: 'FAIL', skip: 'NOT RUN' };

  // Requirement → steps that cite it, in first-seen order.
  const reqs = new Map();
  for (const r of results) {
    for (const c of r.crIds || []) {
      if (!reqs.has(c)) reqs.set(c, []);
      reqs.get(c).push(r);
    }
  }
  const reqState = (steps) =>
    steps.some((r) => state(r) === 'fail') ? 'fail' : steps.some((r) => r.passed) ? 'pass' : 'skip';
  const reqIds = [...reqs.keys()].sort();
  const reqCount = { pass: 0, fail: 0, skip: 0 };
  const reqRows = reqIds
    .map((id) => {
      const s = reqState(reqs.get(id));
      reqCount[s]++;
      return `
    <tr class="${s}">
      <td class="id">${htmlEscape(id)}</td>
      <td class="${s}">${word[s]}</td>
      <td class="wrap">${htmlEscape(reqs.get(id).map((r) => r.step).join(', '))}</td>
    </tr>`;
    })
    .join('\n');

  const scenarioRows = scenarioResults
    .map((s) => {
      const f = s.total - s.passed - s.skipped;
      return `
    <tr class="${f > 0 ? 'fail' : s.skipped > 0 ? 'skip' : 'pass'}">
      <td>${htmlEscape(s.name)}</td>
      <td>${s.total}</td>
      <td>${s.passed}</td>
      <td${f > 0 ? ' class="fail"' : ''}>${f}</td>
      <td>${s.skipped}</td>
    </tr>`;
    })
    .join('\n');

  const rows = results
    .map(
      (r) => `
    <tr class="${state(r)}">
      <td>${htmlEscape(r.scenarioName || r.scenario)}</td>
      <td>${htmlEscape(r.step)}</td>
      <td>${htmlEscape(r.name)}</td>
      <td class="wrap">${htmlEscape((r.crIds || []).join(', '))}</td>
      <td>${htmlEscape(r.method)}</td>
      <td class="wrap">${htmlEscape(r.endpoint)}</td>
      <td>${htmlEscape(r.expected)}</td>
      <td>${htmlEscape(r.actual)}</td>
      <td class="${state(r)}">${word[state(r)]}</td>
      <td class="reason">${htmlEscape(r.reason)}</td>
    </tr>`
    )
    .join('\n');

  const html = `<!doctype html>
<html>
<head>
  <meta charset="utf-8">
  <title>Margo Conformance Report — ${persona} — ${grpLabel}</title>
  <style>${REPORT_CSS}</style>
</head>
<body>
  <h1>Margo Conformance Report</h1>
  <div class="meta">
    Persona: <strong>${persona}</strong> &nbsp;|&nbsp;
    Group: <strong>${grpLabel}</strong> &nbsp;|&nbsp;
    Claimed App Version: <strong>${htmlEscape(groupVersion || 'unknown')}</strong> &nbsp;|&nbsp;
    CTT Margo Version: <strong>${htmlEscape(cttMargoVersion)}</strong> &nbsp;|&nbsp;
    System under test: <strong>${htmlEscape(`WFM ${miafUrl}`)}</strong> &nbsp;|&nbsp;
    Run: <strong>${new Date().toISOString().replace(/\.\d+Z$/, 'Z')}</strong>
  </div>
  ${
    groupVersion && groupVersion !== cttMargoVersion
      ? `<div class="version-warning">⚠ Version Mismatch: Claimed App Version (${htmlEscape(groupVersion)}) differs from CTT Margo Version (${htmlEscape(cttMargoVersion)})</div>`
      : ''
  }
  <div class="summary ${failed === 0 ? 'all-pass' : 'has-fail'}">${failed === 0 ? '✅' : '❌'} ${passed} passed, ${failed} failed, ${skipped} not run, ${results.length} total</div>

  <h2>Requirements Coverage</h2>
  ${
    reqIds.length === 0
      ? `<div class="meta">${NO_REQUIREMENTS_NOTE}</div>`
      : `<div class="meta">${reqCount.pass} of ${reqIds.length} requirements passing &nbsp;|&nbsp; ${reqCount.fail} failing &nbsp;|&nbsp; ${reqCount.skip} not run</div>
  <table>
    <thead><tr><th>Requirement ID</th><th>Result</th><th>Checked by steps</th></tr></thead>
    <tbody>${reqRows}</tbody>
  </table>`
  }

  <h2>Scenario Summary</h2>
  <table>
    <thead><tr><th>Scenario</th><th>Total</th><th>Passed</th><th>Failed</th><th>Not run</th></tr></thead>
    <tbody>${scenarioRows}</tbody>
  </table>

  <h2>Step Details</h2>
  <table class="steps">
    <colgroup>
      <col style="width:10%"><col style="width:11%"><col style="width:14%"><col style="width:13%"><col style="width:6%">
      <col style="width:11.5%"><col style="width:6.5%"><col style="width:5.5%"><col style="width:7%"><col style="width:15.5%">
    </colgroup>
    <thead><tr>
      <th>Scenario</th><th>Step</th><th>Name</th><th>Requirement IDs</th>
      <th>Method</th><th>Endpoint</th><th>Expected</th><th>Actual</th><th>Result</th><th>Reason</th>
    </tr></thead>
    <tbody>${rows}</tbody>
  </table>
</body>
</html>
`;

  fs.writeFileSync(reportFile, html);
}

// ─────────────────────────────────────────────────────────────────────────────
// Main
// ─────────────────────────────────────────────────────────────────────────────

(async () => {
  printBanner();

  const total = scenarios.length;

  for (let i = 0; i < scenarios.length; i++) {
    const scenario = scenarios[i];
    regenerateCertificate();
    context = { ...(scenario.initial_context || {}) };

    printScenarioHeader(scenario, i + 1, total);

    let scenarioPassed = 0;
    let scenarioSkipped = 0;
    for (const step of scenario.steps || []) {
      const outcome = await runStep(scenario, step);
      if (outcome === 'skipped') scenarioSkipped++;
      else if (outcome) scenarioPassed++;
    }

    const stepCount = (scenario.steps || []).length;
    scenarioResults.push({ name: scenario.name, passed: scenarioPassed, skipped: scenarioSkipped, total: stepCount });
    printScenarioSummary(scenarioPassed, stepCount, scenarioSkipped);
  }

  writeReport();
  printFinalSummary(results, scenarioResults, reportFile);

  const failed = results.filter((r) => !r.passed && !r.skipped).length;
  process.exit(failed > 0 ? 1 : 0);
})();
