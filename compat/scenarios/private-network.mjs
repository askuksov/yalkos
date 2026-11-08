// Runs against the instance restarted with WEBHOOK_ALLOW_PRIVATE=false, on the same
// database: addresses on private networks are refused when a webhook is created, a public
// address is accepted (the positive control), and a webhook that already points at the
// loopback address cannot be tested.
import { createRunner, assertEqual, StepFailure } from "../lib/check.mjs";
import { gql, gqlOk } from "../lib/api.mjs";
import { scrub } from "../lib/mask.mjs";

const PRIVATE_URLS = ["http://127.0.0.1:1/", "http://10.1.2.3/", "http://192.168.1.5/", "http://[::1]/"];
// A public address that is never dialled: the webhook is created disabled.
const PUBLIC_URL = "http://93.184.216.34/";

export async function run(ctx) {
  const runner = createRunner("private-network");
  const create = (url, extra = {}) =>
    gql({
      baseUrl: ctx.baseUrl,
      key: ctx.apiKey,
      query: "mutation($i:CreateWebhookInput!){createWebhook(input:$i){id}}",
      variables: { i: { url, resources: ["ISSUE"], ...extra } },
    });

  for (const url of PRIVATE_URLS) {
    await runner.step(`create for ${url} is refused with reason not_allowed`, async () => {
      const res = await create(url);
      const id = res.json?.data?.createWebhook?.id;
      if (id) {
        // Do not leave it behind when the refusal is missing.
        await gql({ baseUrl: ctx.baseUrl, key: ctx.apiKey, query: "mutation($id:ID!){deleteWebhook(id:$id)}", variables: { id } });
        throw new StepFailure(`the webhook for ${url} was created`);
      }
      const e = res.json?.errors?.[0];
      if (!e) throw new StepFailure(`no error for ${url} (HTTP ${res.status})`);
      assertEqual(e.extensions?.field, "url", "field");
      assertEqual(e.extensions?.reason, "not_allowed", `reason (${scrub(e.message)})`);
    });
  }

  await runner.step(`control: a public address is accepted (created disabled, then deleted)`, async () => {
    const res = await create(PUBLIC_URL, { enabled: false });
    const id = res.json?.data?.createWebhook?.id;
    if (!id) throw new StepFailure(`the public address was refused: ${scrub(res.json?.errors?.[0]?.message ?? `HTTP ${res.status}`)}`);
    const deleted = await gqlOk({ baseUrl: ctx.baseUrl, key: ctx.apiKey, query: "mutation($id:ID!){deleteWebhook(id:$id)}", variables: { id } });
    assertEqual(deleted.deleteWebhook, true, "deleted");
  });

  await runner.step("Send test to a webhook that points at 127.0.0.1 fails with address not allowed", async () => {
    const list = await gqlOk({ baseUrl: ctx.baseUrl, cookie: ctx.ownerCookie, query: "{ webhooks { id url } }" });
    const loopback = list.webhooks.find((w) => /^http:\/\/127\.0\.0\.1[:/]/.test(w.url));
    if (!loopback) throw new StepFailure("no webhook points at 127.0.0.1: the webhooks stage has to run first");
    const data = await gqlOk({
      baseUrl: ctx.baseUrl,
      key: ctx.apiKey,
      query: "mutation($id:ID!){testWebhook(id:$id){status error responseStatus}}",
      variables: { id: loopback.id },
    });
    const t = data.testWebhook;
    assertEqual(t.status, "FAILED", "status");
    assertEqual(/^address not allowed: 127\.0\.0\.1/.test(String(t.error)), true, `error (${scrub(String(t.error))})`);
    assertEqual(t.responseStatus, null, "response status");
  });

  return runner;
}
