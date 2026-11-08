// Checks that the bootstrap produced a working API key: it identifies the
// owner, and the same key altered by one character is rejected.
import { createRunner, assertEqual, StepFailure } from "../lib/check.mjs";
import { gql, gqlOk } from "../lib/api.mjs";

const VIEWER = "query { viewer { id } }";

// The same key with its last character replaced by a different one.
function alter(key) {
  const last = key.slice(-1);
  return key.slice(0, -1) + (last === "a" ? "b" : "a");
}

export async function run(ctx) {
  const runner = createRunner("bootstrap");

  await runner.step("viewer returns the owner", async () => {
    const data = await gqlOk({ baseUrl: ctx.baseUrl, path: "/graphql", key: ctx.apiKey, query: VIEWER });
    assertEqual(data.viewer?.id, ctx.owner.id, "viewer id");
  });

  await runner.step("an altered key is rejected", async () => {
    const res = await gql({ baseUrl: ctx.baseUrl, path: "/graphql", key: alter(ctx.apiKey), query: VIEWER });
    if (res.json?.data?.viewer?.id) throw new StepFailure("the altered key was accepted");
    const rejected = res.status === 401 || (Array.isArray(res.json?.errors) && res.json.errors.length > 0);
    if (!rejected) throw new StepFailure(`expected a rejection, got HTTP ${res.status}`);
  });

  return runner;
}
