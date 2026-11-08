// Webhook scenario: three receivers (the SDK helper, and two hand-written ones that check the
// signature the way n8n does) are registered as webhooks, then every kind of change is made
// through the public API and the deliveries each receiver accepted are counted exactly.
// Waiting for deliveries uses the native deliveries field, never the database.
import { randomBytes } from "node:crypto";
import { LinearClient } from "@linear/sdk";
import { createRunner, assertEqual, StepFailure } from "../lib/check.mjs";
import { gql, gqlOk } from "../lib/api.mjs";
import { mask, scrub } from "../lib/mask.mjs";
import { startReceiver, waitSettled } from "../lib/receivers.mjs";

const SECRET_SHAPE = /^yal_wh_[0-9A-Za-z]{40}$/;
// A text for a step message: scrubbed, first line only.
const describeError = (e) => scrub(`${e?.constructor?.name}: ${String(e?.message).split("\n")[0]}`);

// Fails the step with a clear text when an earlier step did not produce its value.
function need(value, name, step) {
  if (value === undefined || value === null) throw new StepFailure(`${name} is missing: step "${step}" did not complete`);
  return value;
}
const must = (cond, msg) => {
  if (!cond) throw new StepFailure(msg);
};
// Secrets never go through assertEqual (it prints both values): compare, then assert the boolean.
const isShape = (secret) => typeof secret === "string" && SECRET_SHAPE.test(secret);

// "<receiver>:<type>:<action>" lists for the receivers a change reaches. Labels reach every
// webhook; an issue or a milestone reaches the webhook of its project, and A (all projects).
const ALL = (type, action) => [`A:${type}:${action}`, `B:${type}:${action}`, `C:${type}:${action}`];
const ALP = (type, action, n = 1) => Array.from({ length: n }, () => [`A:${type}:${action}`, `C:${type}:${action}`]).flat();
const BET = (type, action, n = 1) => Array.from({ length: n }, () => [`A:${type}:${action}`, `B:${type}:${action}`]).flat();

const tally = (list) => {
  const out = {};
  for (const k of list) out[k] = (out[k] ?? 0) + 1;
  return out;
};
const format = (counts) =>
  Object.entries(counts)
    .sort()
    .map(([k, n]) => `${k} x${n}`)
    .join(", ") || "none";

export async function run(ctx) {
  const runner = createRunner("webhooks");
  const { alp, bet } = ctx.projects;
  const client = new LinearClient({ apiKey: ctx.apiKey, apiUrl: ctx.baseUrl + "/graphql" });

  // Changes go through the API key; reads of webhook data use the owner session.
  const nativeCall = (args) => gql({ baseUrl: ctx.baseUrl, ...args });
  const nat = (query, variables) => gqlOk({ baseUrl: ctx.baseUrl, key: ctx.apiKey, query, variables });
  const natRead = (query, variables) => gqlOk({ baseUrl: ctx.baseUrl, cookie: ctx.ownerCookie, query, variables });
  const lin = (query, variables) => gql({ baseUrl: ctx.baseUrl, path: "/graphql", key: ctx.apiKey, query, variables });

  const step = (name, fn) =>
    runner.step(name, async () => {
      try {
        await fn();
      } catch (e) {
        throw new StepFailure(e instanceof StepFailure ? scrub(e.message) : describeError(e));
      }
    });

  // Receivers: started with a placeholder secret, given the real one once the webhook exists.
  const rx = {};
  const placeholder = () => mask(randomBytes(16).toString("hex"));
  for (const kind of ["A", "B", "C"]) rx[kind] = await startReceiver({ kind, secret: placeholder() });
  const receivers = Object.values(rx);
  const webhookIds = {};
  const secrets = {};

  const settle = () => waitSettled({ ctx, webhookIds, receivers });
  const accepted = (rcv) => rcv.events().filter((e) => e.handler !== "*");

  // Runs change(), waits for the deliveries to settle and compares the multiset of
  // "<receiver>:<type>:<action>" (the * handler duplicates left out) with expected. Any
  // rejection a receiver recorded since the change started fails the step.
  function expectEvents(name, change, expected, extra) {
    return step(name, async () => {
      const marks = receivers.map((r) => [r.events().length, r.rejections().length]);
      let changeError = null;
      try {
        await change();
      } catch (e) {
        changeError = e;
      }
      await settle();
      const events = [];
      const rejections = [];
      receivers.forEach((r, i) => {
        events.push(...r.events().slice(marks[i][0]).filter((e) => e.handler !== "*"));
        rejections.push(...r.rejections().slice(marks[i][1]));
      });
      if (changeError) throw new StepFailure(`the change failed: ${describeError(changeError)}`);
      const got = tally(events.map((e) => `${e.rx}:${e.type}:${e.action}`));
      const want = tally(expected);
      console.log(`  ${events.length} events counted: ${format(got)}`);
      const problems = [];
      if (JSON.stringify(Object.entries(got).sort()) !== JSON.stringify(Object.entries(want).sort())) {
        problems.push(`events differ: expected ${format(want)}, got ${format(got)}`);
      }
      if (rejections.length > 0) {
        const refused = format(tally(rejections.map((r) => `${r.rx} answered ${r.status} (${r.reason})`)));
        problems.push(`receivers rejected ${rejections.length} deliveries: ${refused}`);
      }
      must(problems.length === 0, problems.join("; "));
      if (extra) extra(events);
    });
  }

  // The sorted updatedFrom keys of the events one receiver (A by default) accepted.
  const keys = (events, who = "A") => events.filter((e) => e.rx === who).map((e) => (e.updatedFromKeys ?? []).join(","));
  const allKeys = (events, expectedKeys, what) =>
    must(
      events.length > 0 && keys(events).every((k) => k === expectedKeys),
      `${what}: expected the updatedFrom keys ${expectedKeys}, got ${JSON.stringify(keys(events))}`,
    );
  const includeKeys = (events, wanted, what, type) => {
    const mine = events.filter((e) => e.rx === "A" && (!type || e.type === type));
    must(mine.length > 0 && mine.every((e) => wanted.every((k) => (e.updatedFromKeys ?? []).includes(k))), `${what}: expected updatedFrom keys including ${wanted.join(", ")}, got ${JSON.stringify(keys(mine))}`);
  };

  // State the steps hand to each other.
  const s = {};

  try {
    // ---------------------------------------------------------------- W1 registration
    await step("W1 A: createWebhook through the SDK for all projects, secret generated", async () => {
      const payload = await client.createWebhook({
        url: rx.A.url,
        allPublicTeams: true,
        resourceTypes: ["Issue", "Comment", "IssueLabel", "Project"],
      });
      assertEqual(payload.success, true, "success");
      const webhook = await payload.webhook;
      webhookIds.A = need(webhook?.id, "webhook id", "A");
      secrets.A = mask(webhook.secret);
      rx.A.setSecret(secrets.A);
      assertEqual(isShape(secrets.A), true, "secret has the generated shape (yal_wh_ + 40 base62)");
      assertEqual(webhook.allPublicTeams, true, "allPublicTeams");
      assertEqual(webhook.team ?? null, null, "team");
      assertEqual(webhook.enabled, true, "enabled");
      assertEqual(webhook.resourceTypes.length, 4, "resource types");
    });

    await step("W1 B: native createWebhook with its own secret, scoped to one project", async () => {
      const own = mask(`compat-b-${randomBytes(12).toString("hex")}`);
      const data = await nat(
        "mutation($i:CreateWebhookInput!){createWebhook(input:$i){id secret project{key}}}",
        { i: { url: rx.B.url, name: "B second project", projectId: bet.id, resources: ["ISSUE", "COMMENT", "LABEL", "MILESTONE"], secret: own } },
      );
      webhookIds.B = need(data.createWebhook?.id, "webhook id", "B");
      secrets.B = own;
      rx.B.setSecret(own);
      assertEqual(data.createWebhook.secret === own, true, "secret returned verbatim");
      assertEqual(data.createWebhook.project?.key, bet.key, "project key");
    });

    await step("W1 C: webhookCreate the way n8n sends it (teamId, no secret), secret read back", async () => {
      const query =
        "mutation webhookCreate($url: String!, $teamId: String!, $resources: [String!]!) { webhookCreate(input: { url: $url teamId: $teamId resourceTypes: $resources }) { success webhook { id enabled } } }";
      const res = await lin(query, { url: rx.C.url, teamId: alp.id, resources: ["Issue", "Comment", "IssueLabel", "Project"] });
      const created = res.json?.data?.webhookCreate;
      must(created?.success === true, `webhookCreate failed: ${scrub(res.json?.errors?.[0]?.message ?? `HTTP ${res.status}`)}`);
      assertEqual(created.webhook.enabled, true, "enabled");
      webhookIds.C = created.webhook.id;
      const back = await lin("query($id:String!){webhook(id:$id){secret team{id} allPublicTeams label}}", { id: webhookIds.C });
      const w = back.json?.data?.webhook;
      must(w, "webhook(id) returned nothing");
      secrets.C = mask(w.secret);
      rx.C.setSecret(secrets.C);
      assertEqual(isShape(secrets.C), true, "generated secret shape");
      assertEqual(w.team?.id, alp.id, "team id");
      assertEqual(w.allPublicTeams, false, "allPublicTeams");
    });

    await step("W1 the checkExists query of n8n lists the three webhooks", async () => {
      const res = await lin("query { webhooks { nodes { id url enabled team { id name } } } }");
      must(!res.json?.errors, `query failed: ${scrub(res.json?.errors?.[0]?.message ?? "")}`);
      const nodes = res.json?.data?.webhooks?.nodes ?? [];
      assertEqual(nodes.length, 3, "webhook nodes");
      const c = nodes.find((n) => n.id === need(webhookIds.C, "webhook C", "W1 C"));
      need(c, "webhook C in the list", "W1 checkExists");
      assertEqual(c.url, rx.C.url, "url of C");
      assertEqual(c.team?.id, alp.id, "team of C");
      assertEqual(c.enabled, true, "enabled of C");
    });

    // ------------------------------------------------------------------- W2 change stream
    const stateOf = async (project, type) => {
      const data = await natRead("query($p:String!){project(id:$p){states{id type}}}", { p: project.id });
      return need(data.project.states.find((x) => x.type === type), `a ${type} state of ${project.key}`, "states").id;
    };

    await expectEvents(
      "W2 label create: IssueLabel create to A, B, C",
      async () => {
        s.firstLabel = (await nat('mutation{createLabel(input:{name:"compat-bug",color:"#ef4444"}){id}}')).createLabel.id;
      },
      ALL("IssueLabel", "create"),
    );
    await expectEvents(
      "W2 label update: IssueLabel update to A, B, C, keys color,updatedAt",
      async () => {
        await nat('mutation($id:ID!){updateLabel(id:$id,input:{color:"#22c55e"}){id}}', { id: need(s.firstLabel, "label", "W2 label create") });
      },
      ALL("IssueLabel", "update"),
      (ev) => allKeys(ev, "color,updatedAt", "label update"),
    );

    await expectEvents(
      "W2 milestone create: Project create to A, C",
      async () => {
        s.firstMilestone = (await nat('mutation($p:ID!){createMilestone(projectId:$p,input:{name:"Alpha"}){id}}', { p: alp.id })).createMilestone.id;
      },
      ALP("Project", "create"),
    );

    await expectEvents(
      "W2 issue create with labels, assignee, milestone: Issue create to A, C",
      async () => {
        const payload = await client.createIssue({
          teamId: alp.id,
          title: "Webhook first",
          labelIds: [need(s.firstLabel, "label", "W2 label create")],
          assigneeId: ctx.owner.id,
          projectId: need(s.firstMilestone, "milestone", "W2 milestone create"),
          priority: 2,
        });
        s.issue = (await payload.issue).id;
      },
      ALP("Issue", "create"),
    );

    await expectEvents(
      "W2 issue state update: Issue update to A, C, keys stateId,updatedAt",
      async () => {
        const started = await stateOf(alp, "STARTED");
        await nat("mutation($id:ID!,$s:ID){updateIssue(id:$id,input:{stateId:$s}){id}}", { id: need(s.issue, "issue", "W2 issue create"), s: started });
      },
      ALP("Issue", "update"),
      (ev) => allKeys(ev, "stateId,updatedAt", "state update"),
    );

    await expectEvents(
      "W2 issue title, priority, assignee cleared: one Issue update per receiver",
      async () => {
        await nat('mutation($id:ID!){updateIssue(id:$id,input:{title:"Webhook first!",priority:1,assigneeId:null}){id}}', { id: need(s.issue, "issue", "W2 issue create") });
      },
      ALP("Issue", "update"),
      (ev) => allKeys(ev, "assigneeId,priority,title,updatedAt", "title, priority, assignee update"),
    );

    await expectEvents(
      "W2 issue labels changed through the SDK: Issue update with labelIds",
      async () => {
        await client.updateIssue(need(s.issue, "issue", "W2 issue create"), { labelIds: [] });
      },
      ALP("Issue", "update"),
      (ev) => includeKeys(ev, ["labelIds"], "labels update"),
    );

    await expectEvents(
      "W2 no-op issue update: 0 events",
      async () => {
        await nat('mutation($id:ID!){updateIssue(id:$id,input:{title:"Webhook first!"}){id}}', { id: need(s.issue, "issue", "W2 issue create") });
      },
      [],
    );

    await expectEvents(
      "W2 sub-issue create: Issue create to A, C",
      async () => {
        s.child = (
          await nat("mutation($p:ID!,$par:ID){createIssue(input:{projectId:$p,parentId:$par,title:\"Webhook child\"}){id}}", {
            p: alp.id,
            par: need(s.issue, "issue", "W2 issue create"),
          })
        ).createIssue.id;
      },
      ALP("Issue", "create"),
    );

    await expectEvents(
      "W2 comment create: Comment create to A, C",
      async () => {
        s.comment = (await nat('mutation($i:ID!){createComment(issueId:$i,body:"hello"){id}}', { i: need(s.issue, "issue", "W2 issue create") })).createComment.id;
      },
      ALP("Comment", "create"),
    );
    await expectEvents(
      "W2 comment edit: Comment update to A, C, keys body,editedAt,updatedAt",
      async () => {
        await nat('mutation($i:ID!){updateComment(id:$i,body:"hello 2"){id}}', { i: need(s.comment, "comment", "W2 comment create") });
      },
      ALP("Comment", "update"),
      (ev) => allKeys(ev, "body,editedAt,updatedAt", "comment edit"),
    );
    await expectEvents(
      "W2 comment delete: Comment remove to A, C",
      async () => {
        await nat("mutation($i:ID!){deleteComment(id:$i)}", { i: need(s.comment, "comment", "W2 comment create") });
      },
      ALP("Comment", "remove"),
    );

    await expectEvents(
      "W2 issue create in the second project: Issue create to A, B",
      async () => {
        await nat("mutation($p:ID!){createIssue(input:{projectId:$p,title:\"Second project issue\"}){id}}", { p: bet.id });
      },
      BET("Issue", "create"),
    );

    await expectEvents(
      "W2 move with a child: two Issue updates, each to A, B, C (both scopes), keys teamId,number,identifier",
      async () => {
        await nat("mutation($id:ID!,$p:ID!){moveIssue(id:$id,projectId:$p){id}}", { id: need(s.issue, "issue", "W2 issue create"), p: bet.id });
      },
      [...ALL("Issue", "update"), ...ALL("Issue", "update")],
      (ev) => includeKeys(ev, ["identifier", "number", "teamId", "updatedAt"], "move"),
    );

    await expectEvents(
      "W2 delete of the moved parent: Issue remove and Issue update (parentId) to A, B",
      async () => {
        await nat("mutation($id:ID!){deleteIssue(id:$id)}", { id: need(s.issue, "issue", "W2 issue create") });
      },
      [...BET("Issue", "remove"), ...BET("Issue", "update")],
      (ev) => {
        const update = ev.find((e) => e.rx === "A" && e.action === "update");
        must(update && (update.updatedFromKeys ?? []).includes("parentId"), "the child update has no parentId key");
      },
    );

    await expectEvents(
      "W2 label create for the cascade: IssueLabel create to A, B, C",
      async () => {
        s.secondLabel = (await nat('mutation{createLabel(input:{name:"compat-cascade",color:"#3b82f6"}){id}}')).createLabel.id;
      },
      ALL("IssueLabel", "create"),
    );
    await expectEvents(
      "W2 two labeled issues, one per project: Issue create to A, C and to A, B",
      async () => {
        const l2 = need(s.secondLabel, "label", "W2 label create for the cascade");
        await client.createIssue({ teamId: alp.id, title: "Cascade first", labelIds: [l2] });
        await client.createIssue({ teamId: bet.id, title: "Cascade second", labelIds: [l2] });
      },
      [...ALP("Issue", "create"), ...BET("Issue", "create")],
    );
    await expectEvents(
      "W2 label delete cascade: Issue update per project and IssueLabel remove to A, B, C",
      async () => {
        await nat("mutation($id:ID!){deleteLabel(id:$id)}", { id: need(s.secondLabel, "label", "W2 label create for the cascade") });
      },
      [...ALP("Issue", "update"), ...BET("Issue", "update"), ...ALL("IssueLabel", "remove")],
      (ev) => includeKeys(ev, ["labelIds"], "label delete cascade", "Issue"),
    );

    await expectEvents(
      "W2 second milestone create: Project create to A, C",
      async () => {
        s.secondMilestone = (await nat('mutation($p:ID!){createMilestone(projectId:$p,input:{name:"Beta"}){id}}', { p: alp.id })).createMilestone.id;
      },
      ALP("Project", "create"),
    );
    await expectEvents(
      "W2 issue in the second milestone: Issue create to A, C only",
      async () => {
        await client.createIssue({ teamId: alp.id, title: "In the second milestone", projectId: need(s.secondMilestone, "milestone", "W2 second milestone create") });
      },
      ALP("Issue", "create"),
    );
    await expectEvents(
      "W2 milestone status change: Project update to A, C with statusId",
      async () => {
        await nat("mutation($id:ID!){updateMilestone(id:$id,input:{status:DONE}){id}}", { id: need(s.secondMilestone, "milestone", "W2 second milestone create") });
      },
      ALP("Project", "update"),
      (ev) => includeKeys(ev, ["statusId"], "milestone status"),
    );
    await expectEvents(
      "W2 milestone reorder: Project update per moved milestone (2) to A, C with sortOrder",
      async () => {
        const m1 = need(s.firstMilestone, "milestone", "W2 milestone create");
        const m2 = need(s.secondMilestone, "milestone", "W2 second milestone create");
        const data = await natRead("query($p:String!){project(id:$p){milestones{id}}}", { p: alp.id });
        const order = data.project.milestones.map((m) => m.id);
        const [i, j] = [order.indexOf(m1), order.indexOf(m2)];
        must(i >= 0 && j >= 0, "the new milestones are not listed");
        [order[i], order[j]] = [order[j], order[i]];
        await nat("mutation($p:ID!,$ids:[ID!]!){reorderMilestones(projectId:$p,milestoneIds:$ids){id}}", { p: alp.id, ids: order });
      },
      ALP("Project", "update", 2),
      (ev) => includeKeys(ev, ["sortOrder"], "milestone reorder"),
    );
    await expectEvents(
      "W2 milestone delete cascade: Issue update (projectId) and Project remove to A, C",
      async () => {
        await nat("mutation($id:ID!){deleteMilestone(id:$id)}", { id: need(s.secondMilestone, "milestone", "W2 second milestone create") });
      },
      [...ALP("Issue", "update"), ...ALP("Project", "remove")],
      (ev) => includeKeys(ev, ["projectId"], "milestone delete cascade", "Issue"),
    );

    await step("W2 every receiver got at least one accepted delivery", async () => {
      const counts = Object.fromEntries(Object.entries(rx).map(([name, r]) => [name, accepted(r).length]));
      console.log(`  accepted deliveries: ${Object.entries(counts).map(([k, n]) => `${k} ${n}`).join(", ")}`);
      for (const [name, n] of Object.entries(counts)) must(n >= 1, `receiver ${name} got ${n} deliveries`);
      must(rx.A.rejections().length + rx.B.rejections().length + rx.C.rejections().length === 0, "a receiver recorded a rejection during the change stream");
    });

    // --------------------------------------------------------------------- W3 test delivery
    const testWebhook = async (id) => (await nat("mutation($id:ID!){testWebhook(id:$id){id resource action status attempts responseStatus error}}", { id })).testWebhook;
    const listDeliveries = async (id) => (await natRead("query($id:ID!){webhook(id:$id){deliveries(first:50){resource status}}}", { id })).webhook.deliveries;

    await step("W3 Send test to A: delivered, only the * handler runs (WebhookTest)", async () => {
      const mark = rx.A.events().length;
      const t = await testWebhook(need(webhookIds.A, "webhook A", "W1 A"));
      await settle();
      assertEqual(t.status, "SUCCEEDED", "status");
      assertEqual(t.responseStatus, 200, "response status");
      assertEqual(t.resource, "test", "resource");
      assertEqual(t.action, "test", "action");
      const seen = rx.A.events().slice(mark);
      must(seen.length === 1 && seen[0].handler === "*" && seen[0].type === "WebhookTest" && seen[0].action === "test", `A ran ${JSON.stringify(seen.map((e) => [e.handler, e.type, e.action]))}, expected only the * handler for WebhookTest`);
    });

    await step("W3 Send test to B: accepted by the hand-written check", async () => {
      const mark = rx.B.events().length;
      const t = await testWebhook(need(webhookIds.B, "webhook B", "W1 B"));
      await settle();
      assertEqual(t.status, "SUCCEEDED", "status");
      assertEqual(t.responseStatus, 200, "response status");
      const seen = rx.B.events().slice(mark);
      must(seen.length === 1 && seen[0].type === "WebhookTest", `B accepted ${seen.length} deliveries, expected one WebhookTest`);
    });

    await step("W3 test deliveries are listed with resource test", async () => {
      for (const name of ["A", "B"]) {
        const tests = (await listDeliveries(need(webhookIds[name], `webhook ${name}`, "W1"))).filter((d) => d.resource === "test");
        assertEqual(tests.length, 1, `test deliveries of ${name}`);
        assertEqual(tests[0].status, "SUCCEEDED", `status of the test delivery of ${name}`);
      }
    });

    await step("W3 a disabled webhook can still be tested", async () => {
      const id = need(webhookIds.B, "webhook B", "W1 B");
      const off = await nat("mutation($id:ID!){updateWebhook(id:$id,input:{enabled:false}){enabled}}", { id });
      assertEqual(off.updateWebhook.enabled, false, "enabled after disabling");
      const t = await testWebhook(id);
      assertEqual(t.status, "SUCCEEDED", "test delivery status");
    });
    await expectEvents(
      "W3 a disabled webhook gets no change events: a change in B's project reaches A only",
      async () => {
        await nat("mutation($p:ID!){createIssue(input:{projectId:$p,title:\"While B is disabled\"}){id}}", { p: bet.id });
      },
      ["A:Issue:create"],
    );
    await step("W3 the disabled webhook is enabled again", async () => {
      const on = await nat("mutation($id:ID!){updateWebhook(id:$id,input:{enabled:true}){enabled}}", { id: need(webhookIds.B, "webhook B", "W1 B") });
      assertEqual(on.updateWebhook.enabled, true, "enabled after enabling");
    });

    // ------------------------------------------------------------------ W4 secret rotation
    await step("W4 rotate natively: new secret, the receiver still on the old one answers 400", async () => {
      const id = need(webhookIds.A, "webhook A", "W1 A");
      const old = secrets.A;
      const fresh = mask((await nat("mutation($id:ID!){rotateWebhookSecret(id:$id){secret}}", { id })).rotateWebhookSecret.secret);
      secrets.A = fresh;
      assertEqual(isShape(fresh), true, "rotated secret shape");
      assertEqual(fresh !== old, true, "rotated secret differs from the old one");
      const marks = rx.A.rejections().length;
      const t = await testWebhook(id);
      assertEqual(t.status, "FAILED", "test status with the stale receiver");
      assertEqual(t.responseStatus, 400, "response status of the stale receiver");
      const refused = rx.A.rejections().slice(marks);
      assertEqual(refused.length, 1, "rejections recorded by A");
      console.log(`  A refused with ${refused[0].status}: ${refused[0].reason}`);
    });
    await expectEvents(
      "W4 receiver A with the new secret accepts a change",
      async () => {
        rx.A.setSecret(need(secrets.A, "rotated secret", "W4 rotate natively"));
        await nat("mutation($p:ID!){createIssue(input:{projectId:$p,title:\"After rotation\"}){id}}", { p: alp.id });
      },
      ALP("Issue", "create"),
    );
    await step("W4 rotate through /graphql: the receiver still on the old secret answers 401, the updated one accepts", async () => {
      const id = need(webhookIds.B, "webhook B", "W1 B");
      const old = secrets.B;
      const res = await lin("mutation($id:String!){webhookRotateSecret(id:$id){success secret lastSyncId}}", { id });
      const payload = res.json?.data?.webhookRotateSecret;
      must(payload?.success === true, `webhookRotateSecret failed: ${scrub(res.json?.errors?.[0]?.message ?? `HTTP ${res.status}`)}`);
      secrets.B = mask(payload.secret);
      assertEqual(isShape(secrets.B), true, "rotated secret shape");
      assertEqual(secrets.B !== old, true, "rotated secret differs from the old one");
      assertEqual(payload.lastSyncId > 0, true, "lastSyncId is positive");
      const marks = rx.B.rejections().length;
      const stale = await testWebhook(id);
      assertEqual(stale.status, "FAILED", "test status with the stale receiver");
      assertEqual(stale.responseStatus, 401, "response status of the stale receiver");
      const refused = rx.B.rejections().slice(marks);
      assertEqual(refused.length, 1, "rejections recorded by B");
      console.log(`  B refused with ${refused[0].status}: ${refused[0].reason}`);
      rx.B.setSecret(secrets.B);
      const fresh = await testWebhook(id);
      assertEqual(fresh.status, "SUCCEEDED", "test status with the updated receiver");
      assertEqual(fresh.responseStatus, 200, "response status of the updated receiver");
    });
    await step("W4 the read-back secret equals the rotated one", async () => {
      const native = await natRead("query($id:ID!){webhook(id:$id){secret}}", { id: need(webhookIds.A, "webhook A", "W1 A") });
      assertEqual(native.webhook.secret === secrets.A, true, "native read-back of A equals the rotated secret");
      const back = await lin("query($id:String!){webhook(id:$id){secret}}", { id: need(webhookIds.B, "webhook B", "W1 B") });
      assertEqual(back.json?.data?.webhook?.secret === secrets.B, true, "/graphql read-back of B equals the rotated secret");
    });

    // ------------------------------------------------------------------- W5 destinations
    const refusedReason = (res) => {
      const e = res.json?.errors?.[0];
      return e ? { message: String(e.message), field: e.extensions?.field, reason: e.extensions?.reason } : null;
    };
    await step("W5 http://169.254.169.254/ is refused on native create", async () => {
      const res = await nativeCall({
        key: ctx.apiKey,
        query: "mutation($i:CreateWebhookInput!){createWebhook(input:$i){id}}",
        variables: { i: { url: "http://169.254.169.254/latest/meta-data/", resources: ["ISSUE"] } },
      });
      const e = need(refusedReason(res), "a refusal", "native create");
      assertEqual(e.field, "url", "field");
      assertEqual(e.reason, "not_allowed", "reason");
    });
    await step("W5 http://169.254.169.254/ is refused on /graphql create", async () => {
      const res = await lin("mutation($i:WebhookCreateInput!){webhookCreate(input:$i){success}}", {
        i: { url: "http://169.254.169.254/", resourceTypes: ["Issue"], allPublicTeams: true },
      });
      const e = need(refusedReason(res), "a refusal", "/graphql create");
      assertEqual(/not allowed|invalid url/i.test(e.message), true, `the refusal names the address (${e.message})`);
    });
    await step("W5 an update to http://[fe80::1]/ is refused and the webhook keeps its address", async () => {
      const id = need(webhookIds.B, "webhook B", "W1 B");
      const res = await nativeCall({
        key: ctx.apiKey,
        query: "mutation($id:ID!,$i:UpdateWebhookInput!){updateWebhook(id:$id,input:$i){id}}",
        variables: { id, i: { url: "http://[fe80::1]/" } },
      });
      const e = need(refusedReason(res), "a refusal", "native update");
      assertEqual(e.reason, "not_allowed", "reason");
      const back = await natRead("query($id:ID!){webhook(id:$id){url}}", { id });
      assertEqual(back.webhook.url, rx.B.url, "url after the refused update");
    });
    await step("W5 the refused requests created no webhook", async () => {
      const data = await natRead("{ webhooks { id } }");
      assertEqual(data.webhooks.length, 3, "webhooks");
    });
  } finally {
    await Promise.allSettled(receivers.map((r) => r.close()));
  }

  return runner;
}
