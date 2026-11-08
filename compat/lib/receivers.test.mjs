import { test } from "node:test";
import assert from "node:assert/strict";
import http from "node:http";
import crypto from "node:crypto";
import { startReceiver, waitSettled } from "./receivers.mjs";

// Deliveries are built here with a local signer: lower-case hex HMAC-SHA256 of the raw body.
const SECRET = "test-secret-0123456789abcdef";
const OTHER = "other-secret-fedcba9876543210";
const sign = (secret, raw) => crypto.createHmac("sha256", secret).update(raw).digest("hex");

function body(ts, extra = {}) {
  return JSON.stringify({
    action: "update",
    type: "Issue",
    createdAt: new Date(ts).toISOString(),
    data: { id: "i1", title: "t" },
    updatedFrom: { updatedAt: new Date(ts - 1000).toISOString(), title: "old" },
    organizationId: "o1",
    webhookId: "w1",
    webhookTimestamp: ts,
    ...extra,
  });
}

async function deliver(rx, { raw, signature, ts, secret = SECRET, headers = {} }) {
  const res = await fetch(rx.url, {
    method: "POST",
    headers: {
      "content-type": "application/json; charset=utf-8",
      "linear-signature": signature ?? sign(secret, raw),
      "linear-timestamp": String(ts),
      ...headers,
    },
    body: raw,
  });
  return { status: res.status, text: await res.text() };
}

async function withReceiver(kind, fn) {
  const rx = await startReceiver({ kind, secret: SECRET });
  try {
    await fn(rx);
  } finally {
    await rx.close();
  }
}

test("receiver B accepts a delivery signed with the right secret and a fresh timestamp", async () => {
  for (const kind of ["B", "C"]) {
    await withReceiver(kind, async (rx) => {
      const ts = Date.now();
      const res = await deliver(rx, { raw: body(ts), ts });
      assert.equal(res.status, 200);
      assert.deepEqual(rx.rejections(), []);
      const [e, ...rest] = rx.events();
      assert.equal(rest.length, 0);
      assert.equal(e.rx, kind);
      assert.equal(e.type, "Issue");
      assert.equal(e.action, "update");
      assert.deepEqual(e.updatedFromKeys, ["title", "updatedAt"]);
    });
  }
});

test("receiver B rejects: wrong secret, uppercase hex, timestamp 61 s old, header differing from body", async () => {
  await withReceiver("B", async (rx) => {
    const ts = Date.now();
    const raw = body(ts);
    const cases = [
      ["signature", await deliver(rx, { raw, ts, secret: OTHER })],
      ["signature-case", await deliver(rx, { raw, ts, signature: sign(SECRET, raw).toUpperCase() })],
      ["timestamp-window", await deliver(rx, { raw: body(ts - 61_000), ts })],
      ["timestamp-header-mismatch", await deliver(rx, { raw, ts: ts + 1 })],
    ];
    for (const [reason, res] of cases) assert.equal(res.status, 401, reason);
    assert.deepEqual(
      rx.rejections().map((r) => [r.status, r.reason]),
      cases.map(([reason]) => [401, reason]),
    );
    assert.deepEqual(rx.events(), []);
    // A rejection text names the reason only.
    const recorded = JSON.stringify(rx.rejections());
    assert.equal(recorded.includes(SECRET), false);
    assert.equal(recorded.includes(OTHER), false);
    assert.equal(recorded.includes(sign(SECRET, raw)), false);
  });
});

test("receiver A (SDK handler) accepts the same delivery and answers 400 for the wrong secret", async () => {
  await withReceiver("A", async (rx) => {
    const ts = Date.now();
    const raw = body(ts);
    const ok = await deliver(rx, { raw, ts });
    assert.equal(ok.status, 200);
    const accepted = rx.events().filter((e) => e.handler !== "*");
    assert.equal(accepted.length, 1);
    assert.equal(accepted[0].rx, "A");
    assert.equal(accepted[0].type, "Issue");
    assert.equal(accepted[0].action, "update");
    assert.deepEqual(accepted[0].updatedFromKeys, ["title", "updatedAt"]);
    assert.deepEqual(rx.rejections(), []);

    const bad = await deliver(rx, { raw, ts, secret: OTHER });
    assert.equal(bad.status, 400);
    const [r] = rx.rejections();
    assert.equal(r.rx, "A");
    assert.equal(r.status, 400);
    const recorded = JSON.stringify(rx.rejections());
    assert.equal(recorded.includes(SECRET), false);
    assert.equal(recorded.includes(OTHER), false);
    assert.equal(recorded.includes(sign(OTHER, raw)), false);
    // The refused delivery added no event.
    assert.equal(rx.events().filter((e) => e.handler !== "*").length, 1);
  });
});

test("receiver A runs only the * handler for a type it has no handler for", async () => {
  await withReceiver("A", async (rx) => {
    const ts = Date.now();
    const raw = body(ts, { type: "WebhookTest", action: "test" });
    const res = await deliver(rx, { raw, ts });
    assert.equal(res.status, 200);
    assert.deepEqual(rx.events().map((e) => [e.handler, e.type, e.action]), [["*", "WebhookTest", "test"]]);
  });
});

test("setMode 503 answers 503 and records no event", async () => {
  for (const kind of ["A", "B"]) {
    await withReceiver(kind, async (rx) => {
      const ts = Date.now();
      const raw = body(ts);
      rx.setMode("503");
      const res = await deliver(rx, { raw, ts });
      assert.equal(res.status, 503);
      assert.deepEqual(rx.events(), []);
      assert.deepEqual(rx.rejections(), []);
      assert.equal(rx.requests(), 1);
      rx.setMode("ok");
      assert.equal((await deliver(rx, { raw: body(ts), ts })).status, 200);
      assert.equal(rx.events().filter((e) => e.handler !== "*").length, 1);
    });
  }
});

test("setSecret changes the secret used for the next request", async () => {
  for (const kind of ["A", "B"]) {
    await withReceiver(kind, async (rx) => {
      const ts = Date.now();
      const raw = body(ts);
      rx.setSecret(OTHER);
      assert.equal((await deliver(rx, { raw, ts })).status, kind === "A" ? 400 : 401);
      assert.equal((await deliver(rx, { raw, ts, secret: OTHER })).status, 200);
    });
  }
});

test("a receiver with 0 deliveries reports an empty events list (the scenario fails on it)", async () => {
  for (const kind of ["A", "B", "C"]) {
    await withReceiver(kind, async (rx) => {
      assert.deepEqual(rx.events(), []);
      assert.deepEqual(rx.rejections(), []);
      assert.equal(rx.events().length >= 1, false);
    });
  }
});

// A stand-in for the native API that answers the deliveries query from a script.
async function fakeNative(statusesFor) {
  const seen = [];
  const server = http.createServer((req, res) => {
    const chunks = [];
    req.on("data", (c) => chunks.push(c));
    req.on("end", () => {
      const { variables } = JSON.parse(Buffer.concat(chunks).toString());
      seen.push({ id: variables.id, cookie: req.headers.cookie });
      res.setHeader("content-type", "application/json");
      res.end(JSON.stringify({ data: { webhook: { deliveries: statusesFor(variables.id, seen.length) } } }));
    });
  });
  await new Promise((r) => server.listen(0, "127.0.0.1", r));
  return {
    ctx: { baseUrl: `http://127.0.0.1:${server.address().port}`, ownerCookie: "cookie-value" },
    seen,
    close: () => new Promise((r) => (server.close(() => r()), server.closeAllConnections())),
  };
}

const fast = { pollMs: 20, quietMs: 40, maxMs: 400 };

test("waitSettled returns once PENDING deliveries are done, reading with the owner cookie", async () => {
  const api = await fakeNative((id, n) => [{ status: n < 3 ? "PENDING" : "SUCCEEDED", nextAttemptAt: null }]);
  try {
    await waitSettled({ ctx: api.ctx, webhookIds: { A: "wa" }, receivers: [], ...fast });
    assert.ok(api.seen.length >= 3);
    assert.equal(api.seen[0].cookie, "yalkos_session=cookie-value");
  } finally {
    await api.close();
  }
});

test("waitSettled treats a RETRYING delivery due in the future as settled", async () => {
  const later = new Date(Date.now() + 60_000).toISOString();
  const api = await fakeNative(() => [{ status: "RETRYING", nextAttemptAt: later }, { status: "SUCCEEDED", nextAttemptAt: null }]);
  try {
    await waitSettled({ ctx: api.ctx, webhookIds: { A: "wa" }, receivers: [], ...fast });
  } finally {
    await api.close();
  }
});

test("waitSettled times out on a due RETRYING delivery and names the webhook and the statuses", async () => {
  const past = new Date(Date.now() - 1000).toISOString();
  const api = await fakeNative(() => [{ status: "RETRYING", nextAttemptAt: past }, { status: "PENDING", nextAttemptAt: null }]);
  try {
    await assert.rejects(
      waitSettled({ ctx: api.ctx, webhookIds: { B: "wb" }, receivers: [], ...fast }),
      (err) => /did not settle/.test(err.message) && /B: /.test(err.message) && /RETRYING x1/.test(err.message) && /PENDING x1/.test(err.message),
    );
  } finally {
    await api.close();
  }
});

test("waitSettled waits for the receivers to be quiet", async () => {
  const api = await fakeNative(() => [{ status: "SUCCEEDED", nextAttemptAt: null }]);
  const rx = await startReceiver({ kind: "B", secret: SECRET });
  try {
    const t0 = Date.now();
    const poke = setInterval(() => {
      const ts = Date.now();
      deliver(rx, { raw: body(ts), ts }).catch(() => {});
    }, 25);
    setTimeout(() => clearInterval(poke), 200);
    await waitSettled({ ctx: api.ctx, webhookIds: { B: "wb" }, receivers: [rx], pollMs: 20, quietMs: 100, maxMs: 2000 });
    assert.ok(Date.now() - t0 >= 250, `returned after ${Date.now() - t0} ms, before the traffic stopped and 100 ms passed`);
  } finally {
    await rx.close();
    await api.close();
  }
});

test("waitSettled refuses to run without any webhook id", async () => {
  for (const webhookIds of [{}, []]) {
    await assert.rejects(waitSettled({ ctx: {}, webhookIds, receivers: [], ...fast }), /at least one webhook id/);
  }
});
