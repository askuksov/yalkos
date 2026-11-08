// The stages of a compatibility run, in order. A stage is
//   { name, env, restart, run(ctx) }
// run(ctx) returns the step runner of the stage (see lib/check.mjs). A restart
// stage gets the instance restarted with its env before it runs.
// ctx = { baseUrl, apiKey, ownerCookie, owner, member, projects, milestone, instance, sdkVersion }
import { run as bootstrap } from "./bootstrap.mjs";

export const STAGES = [{ name: "bootstrap", env: {}, restart: false, run: bootstrap }];
