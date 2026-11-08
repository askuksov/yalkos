// Webhook receivers for the compatibility checks, run in the runner process.
//
//   A  the SDK helper: LinearWebhookClient(secret).createHandler(), adapted to node:http
//   B  hand-written, n8n style: lower-case hex HMAC-SHA256 of the raw body compared as a
//      string, numeric webhookTimestamp within 60 s, Linear-Timestamp header equal to it
//   C  a second hand-written receiver with the same check as B
//
// A receiver keeps what it accepted (events) and what it refused (rejections) in memory.
// Rejection texts name the reason only: never the secret and never a signature.
import http from "node:http";
import crypto from "node:crypto";
import { LinearWebhookClient } from "@linear/sdk/webhooks";
import { gql } from "./api.mjs";
import { scrub } from "./mask.mjs";

const HANDLED_TYPES = ["Issue", "Comment", "IssueLabel", "Project", "*"];
const WINDOW_MS = 60_000;

const hmacHex = (secret, raw) => crypto.createHmac("sha256", secret).update(raw).digest("hex");
const keysOf = (o) => (o && typeof o === "object" ? Object.keys(o).sort() : null);

// Removes the secret and the signatures from a text that may be recorded.
function redact(text, hide) {
  let out = String(text);
  for (const h of hide) if (typeof h === "string" && h.length >= 8) out = out.split(h).join("***");
  return out;
}

// The check of receivers B and C. Returns the reason of a refusal, or null.
function checkSigned(req, raw, secret) {
  if (req.method !== "POST") return { reason: "method" };
  const actual = req.headers["linear-signature"];
  const expected = hmacHex(secret, raw);
  if (typeof actual !== "string" || actual.length !== expected.length) return { reason: "signature" };
  if (!crypto.timingSafeEqual(Buffer.from(expected), Buffer.from(actual))) {
    return { reason: actual.toLowerCase() === expected ? "signature-case" : "signature" };
  }
  let body;
  try {
    body = JSON.parse(raw.toString("utf8"));
  } catch {
    return { reason: "json" };
  }
  const t = body?.webhookTimestamp;
  if (typeof t !== "number") return { reason: "timestamp-missing" };
  if (Math.abs(Date.now() - t) > WINDOW_MS) return { reason: "timestamp-window" };
  if (String(t) !== req.headers["linear-timestamp"]) return { reason: "timestamp-header-mismatch" };
  return { body };
}

const readBody = (req) =>
  new Promise((resolve, reject) => {
    const chunks = [];
    req.on("data", (c) => chunks.push(c));
    req.on("end", () => resolve(Buffer.concat(chunks)));
    req.on("error", reject);
  });

// A node request as a fetch Request (the form the SDK handler takes).
function toRequest(req, raw, port) {
  const headers = new Headers();
  for (const [k, v] of Object.entries(req.headers)) {
    if (["host", "connection", "content-length", "transfer-encoding"].includes(k)) continue;
    headers.set(k, Array.isArray(v) ? v.join(", ") : v);
  }
  const hasBody = req.method !== "GET" && req.method !== "HEAD";
  return new Request(`http://127.0.0.1:${port}${req.url}`, { method: req.method, headers, body: hasBody ? raw : undefined });
}

// Starts a receiver on 127.0.0.1 with a free port.
// -> { url, port, events(), rejections(), requests(), setSecret(s), setMode("ok"|"503"), close() }
export async function startReceiver({ kind, secret }) {
  if (!["A", "B", "C"].includes(kind)) throw new Error(`unknown receiver kind: ${kind}`);
  let current = secret;
  let mode = "ok";
  let requests = 0;
  const events = [];
  const rejections = [];
  let port = 0;

  let sdkCache = { secret: null, handler: null };
  const sdkHandler = () => {
    if (sdkCache.secret === current) return sdkCache.handler;
    const handler = new LinearWebhookClient(current).createHandler();
    for (const type of HANDLED_TYPES) {
      handler.on(type, (e) => {
        events.push({
          rx: kind,
          handler: type,
          type: e.type,
          action: e.action,
          updatedFromKeys: keysOf(e.updatedFrom),
          dataKeys: keysOf(e.data),
          dataId: e.data?.id ?? null,
          webhookId: e.webhookId ?? null,
        });
      });
    }
    sdkCache = { secret: current, handler };
    return handler;
  };

  async function serveSdk(req, res, raw) {
    const signature = req.headers["linear-signature"];
    const response = await sdkHandler()(toRequest(req, raw, port));
    const text = await response.text();
    if (response.status >= 400) {
      const reason = redact(text.split("\n")[0].slice(0, 120), [current, signature]);
      rejections.push({ rx: kind, status: response.status, reason });
    }
    res.statusCode = response.status;
    res.end(text);
  }

  function serveSigned(req, res, raw) {
    const verdict = checkSigned(req, raw, current);
    if (verdict.reason) {
      rejections.push({ rx: kind, status: 401, reason: verdict.reason });
      res.statusCode = 401;
      res.end("invalid");
      return;
    }
    const body = verdict.body;
    events.push({
      rx: kind,
      handler: "check",
      type: body.type,
      action: body.action,
      updatedFromKeys: keysOf(body.updatedFrom),
      dataKeys: keysOf(body.data),
      dataId: body.data?.id ?? null,
      webhookId: body.webhookId ?? null,
    });
    res.statusCode = 200;
    res.end("OK");
  }

  const server = http.createServer(async (req, res) => {
    requests++;
    try {
      const raw = await readBody(req);
      if (mode === "503") {
        res.statusCode = 503;
        res.end("unavailable");
        return;
      }
      if (kind === "A") await serveSdk(req, res, raw);
      else serveSigned(req, res, raw);
    } catch (err) {
      rejections.push({ rx: kind, status: 500, reason: redact(`receiver error: ${err?.message}`, [current]) });
      res.statusCode = 500;
      res.end("error");
    }
  });
  await new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", resolve);
  });
  port = server.address().port;

  return {
    url: `http://127.0.0.1:${port}/hook`,
    port,
    events: () => events.slice(),
    rejections: () => rejections.slice(),
    requests: () => requests,
    setSecret(s) {
      current = s;
    },
    setMode(m) {
      if (m !== "ok" && m !== "503") throw new Error(`unknown receiver mode: ${m}`);
      mode = m;
    },
    close() {
      return new Promise((resolve) => {
        server.close(() => resolve());
        server.closeAllConnections();
      });
    },
  };
}

const DELIVERIES = "query($id:ID!){webhook(id:$id){deliveries(first:50){status nextAttemptAt}}}";
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// Reads the deliveries of one webhook through the native API with the owner session.
async function readDeliveries(ctx, id) {
  const res = await gql({ baseUrl: ctx.baseUrl, cookie: ctx.ownerCookie, query: DELIVERIES, variables: { id } });
  const list = res.json?.data?.webhook?.deliveries;
  if (!Array.isArray(list)) {
    const why = res.json?.errors?.[0]?.message ?? `HTTP ${res.status}`;
    throw new Error(scrub(`cannot read the deliveries: ${why}`));
  }
  return list;
}

const countOf = (receivers) => receivers.reduce((n, r) => n + r.requests(), 0);

// Waits until no delivery of the webhooks is PENDING or RETRYING and already due (a retry
// scheduled for later counts as settled), then until the receivers saw no request for
// quietMs. webhookIds is an object { name: id } (names appear in the error) or a list of ids.
export async function waitSettled({ ctx, webhookIds, receivers, maxMs = 15_000, pollMs = 500, quietMs = 500 }) {
  const entries = Array.isArray(webhookIds) ? webhookIds.map((id) => [id, id]) : Object.entries(webhookIds);
  // Nothing to look at would "settle" at once and hide a missing registration.
  if (entries.length === 0) throw new Error("waitSettled needs at least one webhook id");
  const started = Date.now();
  let seen = countOf(receivers);
  let lastChange = Date.now();
  let last = {};
  for (;;) {
    const now = Date.now();
    const open = [];
    last = {};
    for (const [name, id] of entries) {
      const list = await readDeliveries(ctx, id);
      const counts = {};
      for (const d of list) {
        const due = d.status === "RETRYING" && !(d.nextAttemptAt && Date.parse(d.nextAttemptAt) > now);
        const label = d.status === "RETRYING" && !due ? "RETRYING (later)" : d.status;
        counts[label] = (counts[label] ?? 0) + 1;
        if (d.status === "PENDING" || due) open.push(name);
      }
      last[name] = counts;
    }
    const total = countOf(receivers);
    if (total !== seen) {
      seen = total;
      lastChange = Date.now();
    }
    if (open.length === 0 && Date.now() - lastChange >= quietMs) return;
    if (Date.now() - started >= maxMs) {
      const text = Object.entries(last)
        .map(([name, c]) => `${name}: ${Object.entries(c).map(([s, n]) => `${s} x${n}`).join(", ") || "no deliveries"}`)
        .join("; ");
      throw new Error(`deliveries did not settle in ${maxMs} ms (${open.length ? `open: ${[...new Set(open)].join(", ")}; ` : "receivers kept receiving; "}${text})`);
    }
    await sleep(pollMs);
  }
}
