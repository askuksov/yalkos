// The stages of a compatibility run, in order. A stage is
//   { name, env, restart, run(ctx) }
// run(ctx) returns the step runner of the stage (see lib/check.mjs). A restart
// stage gets the instance restarted with its env before it runs.
// ctx = { baseUrl, apiKey, ownerCookie, owner, member, projects, milestone, instance, sdkVersion }
// The handler probe needs no instance. Stages that restart the instance come last; the
// rate limit stage is the final one.
import { run as bootstrap } from "./bootstrap.mjs";
import { run as sdk } from "./sdk.mjs";
import { run as webhooks } from "./webhooks.mjs";
import { run as handlerProbe } from "./handler-probe.mjs";
import { run as privateNetwork } from "./private-network.mjs";
import { run as ratelimit } from "./ratelimit.mjs";

export const STAGES = [
  { name: "bootstrap", env: {}, restart: false, run: bootstrap },
  { name: "sdk", env: {}, restart: false, run: sdk },
  { name: "webhooks", env: {}, restart: false, run: webhooks },
  { name: "handler-probe", env: {}, restart: false, run: handlerProbe },
  { name: "private-network", env: { WEBHOOK_ALLOW_PRIVATE: "false" }, restart: true, run: privateNetwork },
  { name: "ratelimit", env: { API_RATE_LIMIT: "5" }, restart: true, run: ratelimit },
];
