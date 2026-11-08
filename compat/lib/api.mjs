import { scrub } from "./mask.mjs";

// POSTs one GraphQL request. key is sent as `Authorization: <key>`, cookie is the
// session token sent as `Cookie: yalkos_session=<token>`. Returns the status,
// the response headers and the parsed JSON body (null when it is not JSON).
export async function gql({ baseUrl, path = "/api/graphql", query, variables, key, cookie }) {
  const headers = { "Content-Type": "application/json" };
  if (key) headers.Authorization = key;
  if (cookie) headers.Cookie = `yalkos_session=${cookie}`;
  const res = await fetch(baseUrl + path, {
    method: "POST",
    headers,
    body: JSON.stringify({ query, variables }),
  });
  const text = await res.text();
  let json = null;
  try {
    json = JSON.parse(text);
  } catch {
    json = null;
  }
  return { status: res.status, headers: res.headers, json };
}

// Throws when the response carries GraphQL errors or no data; the message lists
// every error message with its extensions.code. Returns json.data.
export function assertNoErrors(res) {
  if (Array.isArray(res.json?.errors) && res.json.errors.length > 0) {
    const parts = res.json.errors.map((e) => {
      const code = e.extensions?.code;
      return code ? `${e.message} [${code}]` : String(e.message);
    });
    throw new Error(scrub(parts.join("; ")));
  }
  if (res.status !== 200 || !res.json || res.json.data == null) {
    throw new Error(scrub(`HTTP ${res.status} without data`));
  }
  return res.json.data;
}

export async function gqlOk(args) {
  return assertNoErrors(await gql(args));
}
