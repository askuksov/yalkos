import { randomBytes } from "node:crypto";
import { gql, gqlOk, assertNoErrors } from "./api.mjs";
import { mask, scrub } from "./mask.mjs";

const SESSION_COOKIE = "yalkos_session";

// Runs one bootstrap call; a failure is rethrown as `<mutation>: <scrubbed text>`.
async function named(mutation, fn) {
  try {
    return await fn();
  } catch (err) {
    throw new Error(`${mutation}: ${scrub(err instanceof Error ? err.message : String(err))}`);
  }
}

// The session token from the Set-Cookie headers of a response, or null.
function sessionToken(headers) {
  for (const line of headers.getSetCookie()) {
    const pair = line.split(";")[0];
    if (pair.startsWith(`${SESSION_COOKIE}=`)) return pair.slice(SESSION_COOKIE.length + 1);
  }
  return null;
}

const randomPassword = () => mask(randomBytes(18).toString("base64url"));

// Creates the owner, an API key, a second (MEMBER) user, two projects and a
// milestone on a fresh instance. ownerCookie is the session token (not the full
// header). Secrets are masked before first use and scrubbed from every error.
export async function bootstrap({ baseUrl, code, password }) {
  mask(code);
  const ownerPassword = mask(password ?? randomPassword());
  const memberPassword = randomPassword();

  const setup = await named("setup", async () => {
    const res = await gql({
      baseUrl,
      query: "mutation($i:SetupInput!){setup(input:$i){id name email role}}",
      variables: {
        i: {
          code,
          workspaceName: "CI Workspace",
          name: "CI Owner",
          email: "ci-owner@example.invalid",
          password: ownerPassword,
        },
      },
    });
    const token = sessionToken(res.headers);
    if (token) mask(token);
    const data = assertNoErrors(res);
    if (!token) throw new Error(`no ${SESSION_COOKIE} cookie in the response`);
    return { owner: data.setup, ownerCookie: token };
  });
  const { owner, ownerCookie } = setup;

  const apiKey = await named("createApiKey", async () => {
    const data = await gqlOk({
      baseUrl,
      cookie: ownerCookie,
      query: 'mutation{createApiKey(name:"compat"){key apiKey{id displayPrefix}}}',
    });
    return mask(data.createApiKey.key);
  });

  const inviteToken = await named("createInvite", async () => {
    const data = await gqlOk({
      baseUrl,
      cookie: ownerCookie,
      query: "mutation{createInvite(role:MEMBER){url invite{id}}}",
    });
    const token = new URL(data.createInvite.url).pathname.split("/").pop();
    return mask(token);
  });

  const member = await named("acceptInvite", async () => {
    const res = await gql({
      baseUrl,
      query: "mutation($i:AcceptInviteInput!){acceptInvite(input:$i){id name email role}}",
      variables: {
        i: { token: inviteToken, name: "CI Member", email: "ci-member@example.invalid", password: memberPassword },
      },
    });
    const token = sessionToken(res.headers);
    if (token) mask(token);
    return assertNoErrors(res).acceptInvite;
  });

  const createProject = (key, name) =>
    named("createProject", async () => {
      const data = await gqlOk({
        baseUrl,
        key: apiKey,
        query: "mutation($i:CreateProjectInput!){createProject(input:$i){id key name}}",
        variables: { i: { key, name } },
      });
      return data.createProject;
    });
  const alp = await createProject("ALP", "Alpha");
  const bet = await createProject("BET", "Beta");

  const milestone = await named("createMilestone", async () => {
    const data = await gqlOk({
      baseUrl,
      key: apiKey,
      query: "mutation($p:ID!,$i:CreateMilestoneInput!){createMilestone(projectId:$p,input:$i){id name}}",
      variables: { p: alp.id, i: { name: "Release" } },
    });
    return data.createMilestone;
  });

  return { ownerCookie, apiKey, owner, member, projects: { alp, bet }, milestone };
}
