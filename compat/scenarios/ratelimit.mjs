// Runs against the instance restarted with a low API rate limit, on the same
// database and with the same API key: calls are made until the SDK throws.
import { LinearClient, RatelimitedLinearError } from "@linear/sdk";
import { createRunner, assertEqual, StepFailure } from "../lib/check.mjs";

const LIMIT = 5;
// The sixth call must be refused; the loop stops by itself well after that.
const MAX_CALLS = 6;
const GIVE_UP_AFTER = 20;

export async function run(ctx) {
  const runner = createRunner("ratelimit");
  const apiUrl = ctx.baseUrl + "/graphql";
  const client = new LinearClient({ apiKey: ctx.apiKey, apiUrl });
  let refusal = null;

  await runner.step("calls are refused with RatelimitedLinearError", async () => {
    let calls = 0;
    while (refusal === null && calls < GIVE_UP_AFTER) {
      calls++;
      try {
        await client.viewer;
      } catch (e) {
        refusal = e;
      }
    }
    if (refusal === null) throw new StepFailure(`${calls} calls were all accepted`);
    if (!(refusal instanceof RatelimitedLinearError)) {
      throw new StepFailure(`expected RatelimitedLinearError, got ${refusal?.constructor?.name}: ${String(refusal?.message).split("\n")[0]}`);
    }
    if (calls > MAX_CALLS) throw new StepFailure(`the first refusal came on call ${calls}, expected at most ${MAX_CALLS}`);
    if (typeof refusal.retryAfter !== "number" || !(refusal.retryAfter > 0)) {
      throw new StepFailure(`retryAfter is not a positive number: ${JSON.stringify(refusal.retryAfter)}`);
    }
  });

  await runner.step("limit and remaining headers", async () => {
    if (refusal === null) throw new StepFailure('no refusal to read: step "calls are refused with RatelimitedLinearError" did not complete');
    let limit = refusal.requestsLimit;
    let remaining = refusal.requestsRemaining;
    if (limit === undefined || remaining === undefined) {
      // The SDK did not expose the headers: read them from one raw response.
      const res = await fetch(apiUrl, {
        method: "POST",
        headers: { "Content-Type": "application/json", Authorization: ctx.apiKey },
        body: JSON.stringify({ query: "query { viewer { id } }" }),
      });
      await res.arrayBuffer();
      limit = Number(res.headers.get("x-ratelimit-requests-limit"));
      remaining = Number(res.headers.get("x-ratelimit-requests-remaining"));
    }
    assertEqual(limit, LIMIT, "x-ratelimit-requests-limit");
    assertEqual(remaining, 0, "x-ratelimit-requests-remaining");
  });

  return runner;
}
