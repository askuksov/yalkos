// Offline probe of the SDK webhook helper: thirteen variants of a Yalkos-shaped delivery are
// fed to LinearWebhookClient(secret).createHandler() through a Request, and the status the
// handler answers is compared with the expected one. No server is involved. It pins what the
// installed SDK accepts, so a change in the helper's rules shows up even before Yalkos is.
import crypto from "node:crypto";
import { LinearWebhookClient } from "@linear/sdk/webhooks";
import { createRunner, assertEqual, StepFailure } from "../lib/check.mjs";
import { mask, scrub } from "../lib/mask.mjs";

// A delivery body with the fields Yalkos sends.
function body(ts) {
  return JSON.stringify({
    action: "update",
    type: "Issue",
    createdAt: new Date(ts).toISOString(),
    actor: { id: "u1", type: "user", name: "CI Owner", email: "owner@example.invalid", url: "https://receiver.example/profiles/owner" },
    data: { id: "i1", title: "t" },
    updatedFrom: { updatedAt: new Date(ts - 1000).toISOString(), title: "old" },
    url: "https://receiver.example/issue/ALP-1/t",
    organizationId: "o1",
    webhookId: "w1",
    webhookTimestamp: ts,
  });
}

// The expected status of each variant. 200 for what the helper accepts, 400 for a refused
// POST, 405 for a method other than POST. The first variant is the positive control.
// build(now, sign) returns { method, raw, headers }.
export const VARIANTS = [
  {
    name: "valid: lower-case hex HMAC of the raw body, numeric millisecond timestamp",
    status: 200,
    build: (now, sign) => {
      const raw = body(now);
      return { raw, headers: { "linear-signature": sign(raw), "linear-timestamp": String(now), "linear-event": "Issue", "linear-delivery": crypto.randomUUID() } };
    },
  },
  {
    name: "upper-case hex signature",
    status: 400,
    build: (now, sign) => {
      const raw = body(now);
      return { raw, headers: { "linear-signature": sign(raw).toUpperCase() } };
    },
  },
  {
    name: "sha256= prefix on the signature",
    status: 400,
    build: (now, sign) => {
      const raw = body(now);
      return { raw, headers: { "linear-signature": `sha256=${sign(raw)}` } };
    },
  },
  { name: "missing signature header", status: 400, build: (now) => ({ raw: body(now), headers: {} }) },
  {
    name: "GET instead of POST",
    status: 405,
    build: (now, sign) => {
      const raw = body(now);
      return { method: "GET", raw, headers: { "linear-signature": sign(raw) } };
    },
  },
  {
    name: "timestamp in seconds",
    status: 400,
    build: (now, sign) => {
      const raw = body(Math.floor(now / 1000));
      return { raw, headers: { "linear-signature": sign(raw) } };
    },
  },
  {
    name: "timestamp as a JSON string",
    status: 400,
    build: (now, sign) => {
      const raw = body(now).replace(`"webhookTimestamp":${now}`, `"webhookTimestamp":"${now}"`);
      return { raw, headers: { "linear-signature": sign(raw) } };
    },
  },
  {
    name: "timestamp 61 s old with a fresh Linear-Timestamp header",
    status: 400,
    build: (now, sign) => {
      const raw = body(now - 61_000);
      return { raw, headers: { "linear-signature": sign(raw), "linear-timestamp": String(now) } };
    },
  },
  {
    name: "timestamp 59 s in the future",
    status: 200,
    build: (now, sign) => {
      const raw = body(now + 59_000);
      return { raw, headers: { "linear-signature": sign(raw) } };
    },
  },
  {
    name: "no timestamp",
    status: 400,
    build: (now, sign) => {
      const raw = JSON.stringify({ ...JSON.parse(body(now)), webhookTimestamp: undefined });
      return { raw, headers: { "linear-signature": sign(raw) } };
    },
  },
  {
    name: "byte-order mark before the JSON",
    status: 400,
    build: (now, sign) => {
      const raw = String.fromCharCode(0xfeff) + body(now);
      return { raw, headers: { "linear-signature": sign(raw) } };
    },
  },
  {
    name: "pretty-printed body, signed as sent",
    status: 200,
    build: (now, sign) => {
      const raw = JSON.stringify(JSON.parse(body(now)), null, 2);
      return { raw, headers: { "linear-signature": sign(raw) } };
    },
  },
  {
    name: "text/plain content type",
    status: 200,
    build: (now, sign) => {
      const raw = body(now);
      return { raw, headers: { "linear-signature": sign(raw), "content-type": "text/plain" } };
    },
  },
];

export async function run() {
  const runner = createRunner("handler-probe");
  const secret = mask(`probe-${crypto.randomBytes(16).toString("hex")}`);
  const sign = (raw) => crypto.createHmac("sha256", secret).update(Buffer.from(raw, "utf8")).digest("hex");

  for (const variant of VARIANTS) {
    await runner.step(`${variant.status} ${variant.name}`, async () => {
      // A fresh handler per variant: what one variant triggers cannot leak into the next.
      const handler = new LinearWebhookClient(secret).createHandler();
      let invoked = 0;
      handler.on("Issue", () => {
        invoked++;
      });
      const { method = "POST", raw, headers } = variant.build(Date.now(), sign);
      const request = new Request("https://receiver.example/hook", {
        method,
        body: method === "POST" ? raw : undefined,
        headers: { "content-type": "application/json; charset=utf-8", ...headers },
      });
      const response = await handler(request);
      const text = scrub(await response.text()).split("\n")[0].slice(0, 120);
      if (response.status !== variant.status) {
        throw new StepFailure(`status: expected ${variant.status}, got ${response.status} (${text})`);
      }
      // Only an accepted delivery reaches the Issue handler.
      assertEqual(invoked, variant.status === 200 ? 1 : 0, "Issue handler invocations");
    });
  }
  return runner;
}
