// Extracts the GraphQL documents an installed @linear/sdk bundle sends, one
// <operation>.graphql file per supported operation. Never committed; the
// output directory is a temporary one owned by the caller.
import fs from "node:fs";
import path from "node:path";

export const OPERATIONS = JSON.parse(fs.readFileSync(new URL("./operations.json", import.meta.url), "utf8"));

// The marker /* @__PURE__ */ is absent in old bundles, so it is optional.
const DOCUMENT = /const (\w+)Document = (?:\/\* @__PURE__ \*\/ )?new TypedDocumentString\(`([\s\S]*?)`/g;

// Returns { extracted, missing, all }. `all` holds every query, mutation and
// subscription of the bundle. When outDir is given, the supported operations
// found are written there. A supported document with `${` throws.
export function extractDocuments({ sdkDistFile, outDir }) {
  const text = fs.readFileSync(sdkDistFile, "utf8");
  const all = {};
  for (const m of text.matchAll(DOCUMENT)) {
    const head = /^\s*(query|mutation|subscription)\s+(\w+)/.exec(m[2]);
    if (head) all[head[2]] = { kind: head[1], doc: m[2].trim() };
  }
  if (outDir) fs.mkdirSync(outDir, { recursive: true });
  const missing = [];
  let extracted = 0;
  for (const name of OPERATIONS) {
    const found = all[name];
    if (!found) {
      missing.push(name);
      continue;
    }
    if (found.doc.includes("${")) throw new Error(`${name}: interpolated document`);
    if (outDir) fs.writeFileSync(path.join(outDir, `${name}.graphql`), found.doc + "\n");
    extracted++;
  }
  return { extracted, missing, all };
}
