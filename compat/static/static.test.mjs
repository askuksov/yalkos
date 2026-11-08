import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

import { extractDocuments } from "./extract-documents.mjs";
import { semanticDiff } from "./semantic-diff.mjs";
import { validateDocuments } from "./validate.mjs";
import { classifyMessages, parseGoTestJson, runStatic } from "../lib/static.mjs";

const OPS = JSON.parse(fs.readFileSync(new URL("./operations.json", import.meta.url), "utf8"));

// --- fixtures: tiny synthetic bundle texts, never SDK text ---

function tmpDir(t) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "compat-static-test-"));
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
  return dir;
}

function bundleText(entries) {
  return entries
    .map(({ op, doc, marker = true }) =>
      `const ${op}Document = ${marker ? "/* @__PURE__ */ " : ""}new TypedDocumentString(\`${doc}\`, {});\n`)
    .join("");
}

function writeBundle(root, entries) {
  const dist = path.join(root, "node_modules", "@linear", "sdk", "dist");
  fs.mkdirSync(dist, { recursive: true });
  fs.writeFileSync(path.join(dist, "index.mjs"), bundleText(entries));
  return path.join(root, "node_modules");
}

function writeSchema(repoRoot, schema) {
  const dir = path.join(repoRoot, "api", "graphql", "linear");
  fs.mkdirSync(dir, { recursive: true });
  fs.writeFileSync(path.join(dir, "schema.graphqls"), schema);
}

// All 53 operations as `query <op> { a }`, with optional per-operation overrides.
function allEntries(overrides = {}) {
  return OPS.map((op) => ({ op, doc: overrides[op] ?? `query ${op} { a }` }));
}

// Lines of `go test -json` for the given passing and failing subtests.
function goLines({ pass = OPS, fail = [] } = {}) {
  const ev = (Action, name, extra = {}) =>
    JSON.stringify({ Action, Package: "p", Test: `TestSDKDocuments/${name}`, ...extra });
  return [
    ...pass.map((n) => ev("pass", n)),
    ...fail.flatMap((n) => [ev("output", n, { Output: `    ${n}: boom\n` }), ev("fail", n)]),
    JSON.stringify({ Action: fail.length ? "fail" : "pass", Package: "p" }),
  ];
}

const goStub = (spec) => async () => ({ lines: goLines(spec), exitCode: spec?.fail?.length ? 1 : 0 });
const goMustNotRun = async () => {
  throw new Error("go step must not run");
};

// --- extractor ---

test("TestExtractTolerantMarker", (t) => {
  const root = tmpDir(t);
  const dist = path.join(root, "index.mjs");
  fs.writeFileSync(
    dist,
    bundleText([
      { op: "viewer", doc: "query viewer { a }", marker: true },
      { op: "users", doc: "query users { b }", marker: false },
    ]),
  );
  const outDir = path.join(root, "out");
  const res = extractDocuments({ sdkDistFile: dist, outDir });
  assert.equal(res.extracted, 2);
  assert.equal(res.missing.length, OPS.length - 2);
  assert.ok(!res.missing.includes("viewer") && !res.missing.includes("users"));
  assert.deepEqual(fs.readdirSync(outDir).sort(), ["users.graphql", "viewer.graphql"]);
  assert.equal(res.all.users.doc, "query users { b }");
  assert.equal(res.all.viewer.kind, "query");
});

test("TestExtractWritesOnlyTheListedOperations", (t) => {
  const root = tmpDir(t);
  const dist = path.join(root, "index.mjs");
  fs.writeFileSync(
    dist,
    bundleText([
      { op: "viewer", doc: "query viewer { a }" },
      { op: "somethingElse", doc: "mutation somethingElse { z }" },
    ]),
  );
  const outDir = path.join(root, "out");
  const res = extractDocuments({ sdkDistFile: dist, outDir });
  assert.equal(res.extracted, 1);
  assert.deepEqual(fs.readdirSync(outDir), ["viewer.graphql"]);
  assert.equal(res.all.somethingElse.kind, "mutation");
});

test("TestInterpolatedDocumentThrows", (t) => {
  const root = tmpDir(t);
  const dist = path.join(root, "index.mjs");
  fs.writeFileSync(dist, bundleText([{ op: "viewer", doc: "query viewer { a ${frag} }" }]));
  assert.throws(
    () => extractDocuments({ sdkDistFile: dist, outDir: path.join(root, "out") }),
    /viewer: interpolated document/,
  );
});

// --- semantic diff ---

const entry = (doc) => ({ kind: "query", doc });

test("TestSemanticDiffIgnoresOrder", () => {
  const a = { viewer: entry("query viewer { id name email }") };
  const b = { viewer: entry("query viewer { email id name }") };
  const d = semanticDiff(a, b);
  assert.deepEqual(d.changed, []);
  assert.equal(d.orderOnly, 1);
});

test("TestSemanticDiffAddedRemovedRetyped", () => {
  const a = {
    issues: entry("query issues($a: String, $gone: Int) { issues(first: 1) { nodes { id old } } }"),
  };
  const b = {
    issues: entry("query issues($a: String!, $fresh: Int) { issues(first: 1) { nodes { id fresh } } }"),
  };
  const d = semanticDiff(a, b);
  assert.equal(d.changed.length, 1);
  const c = d.changed[0];
  assert.equal(c.op, "issues");
  assert.deepEqual(c.addedFields, ["issues.nodes.fresh"]);
  assert.deepEqual(c.removedFields, ["issues.nodes.old"]);
  assert.deepEqual(c.addedVars, ["$fresh: Int"]);
  assert.deepEqual(c.removedVars, ["$gone: Int"]);
  assert.deepEqual(c.retypedVars, [{ variable: "$a", from: "String", to: "String!" }]);
});

test("TestSemanticDiffArgumentRemovedIsOneEntry", () => {
  const a = { issues: entry("query issues { issues(first: 1, after: \"x\") { nodes { id } } }") };
  const b = { issues: entry("query issues { issues(first: 1) { nodes { id } } }") };
  const d = semanticDiff(a, b);
  assert.equal(d.changed.length, 1);
  assert.deepEqual(d.changed[0].removedFields, ["issues(after)"]);
  assert.deepEqual(d.changed[0].addedFields, []);
});

test("TestSemanticDiffFollowsFragmentsAndIgnoresUnlistedOperations", () => {
  const a = { viewer: entry("query viewer { ...F } fragment F on User { id }"), notListed: entry("query notListed { x }") };
  const b = { viewer: entry("query viewer { ...F } fragment F on User { id name }"), notListed: entry("query notListed { y }") };
  const d = semanticDiff(a, b);
  assert.equal(d.changed.length, 1);
  assert.deepEqual(d.changed[0].addedFields, ["name"]);
});

// --- classification ---

test("TestClassifyAdditive", () => {
  const add = 'Cannot query field "x" on type "Issue".';
  const other = 'Variable "$a" of type "A" used in position expecting type "B".';
  assert.equal(classifyMessages([add]), "additive");
  assert.equal(classifyMessages(['Unknown argument "x" on field "Query.issues".', 'Unknown type "Foo".']), "additive");
  assert.equal(classifyMessages([other]), "other");
  assert.equal(classifyMessages([add, other]), "other");
  assert.equal(classifyMessages([]), "other");
});

// --- go test -json ---

test("TestGoJsonZeroTestsIsTooling", () => {
  const lines = [
    JSON.stringify({ Action: "start", Package: "p" }),
    JSON.stringify({ Action: "output", Package: "p", Output: "testing: warning: no tests to run\n" }),
    JSON.stringify({ Action: "pass", Package: "p" }),
  ];
  const r = parseGoTestJson(lines);
  assert.equal(r.passed, 0);
  assert.equal(r.failed, 0);
});

test("TestGoJsonCountsSubtests", () => {
  const r = parseGoTestJson(goLines({ pass: ["viewer", "users"], fail: ["issues"] }));
  assert.equal(r.passed, 2);
  assert.equal(r.failed, 1);
  assert.equal(r.failures[0].test, "TestSDKDocuments/issues");
  assert.match(r.failures[0].output, /boom/);
});

test("TestGoJsonPackageFailureWithoutSubtestsIsAFailure", () => {
  const r = parseGoTestJson([
    JSON.stringify({ Action: "output", Package: "p", Output: "build failed\n" }),
    JSON.stringify({ Action: "fail", Package: "p" }),
    "not json at all",
  ]);
  assert.equal(r.passed, 0);
  assert.equal(r.failed, 1);
});

// --- graphql-js validation ---

test("TestValidateFindsBogusField", (t) => {
  const root = tmpDir(t);
  const schemaDir = path.join(root, "schema");
  const docsDir = path.join(root, "docs");
  fs.mkdirSync(schemaDir);
  fs.mkdirSync(docsDir);
  fs.writeFileSync(path.join(schemaDir, "a.graphqls"), "type Query { a: String }\n");
  fs.writeFileSync(path.join(schemaDir, "b.graphqls"), "extend type Query { c: String }\n");
  fs.writeFileSync(path.join(docsDir, "bogus.graphql"), "query bogus { a bogusField }\n");
  fs.writeFileSync(path.join(docsDir, "control.graphql"), "query control { a c }\n");
  const res = validateDocuments({ schemaDir, docsDir });
  const byFile = Object.fromEntries(res.map((r) => [r.file, r.messages]));
  assert.deepEqual(byFile["control.graphql"], []);
  assert.equal(byFile["bogus.graphql"].length, 1);
  assert.match(byFile["bogus.graphql"][0], /^Cannot query field "bogusField"/);
});

// --- runStatic ---

test("TestZeroDocumentsIsTooling", async (t) => {
  const root = tmpDir(t);
  const nodeModules = writeBundle(root, []);
  const dist = path.join(nodeModules, "@linear", "sdk", "dist", "index.mjs");
  const ex = extractDocuments({ sdkDistFile: dist, outDir: path.join(root, "out") });
  assert.equal(ex.extracted, 0);
  assert.equal(ex.missing.length, 53);

  const res = await runStatic({ nodeModules, repoRoot: root, runGo: goMustNotRun });
  assert.equal(res.result, "TOOLING");
  assert.equal(res.documents.extracted, 0);
  assert.ok(res.errors.some((e) => e.includes("viewer")));
});

test("TestMissingBundleIsTooling", async (t) => {
  const root = tmpDir(t);
  const res = await runStatic({ nodeModules: path.join(root, "node_modules"), repoRoot: root, runGo: goMustNotRun });
  assert.equal(res.result, "TOOLING");
  assert.ok(res.errors.some((e) => e.includes("SDK bundle not found")));
});

test("TestRunStaticOk", async (t) => {
  const root = tmpDir(t);
  writeSchema(root, "type Query { a: String }\n");
  const nodeModules = writeBundle(root, allEntries());
  const res = await runStatic({ nodeModules, repoRoot: root, runGo: goStub() });
  assert.equal(res.result, "OK");
  assert.deepEqual(res.documents, { extracted: 53, valid: 53 });
  assert.deepEqual(res.errors, []);
});

test("TestRunStaticFiftyTwoGoPassesIsTooling", async (t) => {
  const root = tmpDir(t);
  writeSchema(root, "type Query { a: String }\n");
  const nodeModules = writeBundle(root, allEntries());
  const res = await runStatic({ nodeModules, repoRoot: root, runGo: goStub({ pass: OPS.slice(1) }) });
  assert.equal(res.result, "TOOLING");
});

test("TestRunStaticAdditiveIsDrift", async (t) => {
  const root = tmpDir(t);
  writeSchema(root, "type Query { a: String }\n");
  const nodeModules = writeBundle(root, allEntries({ viewer: "query viewer { a bogusField }" }));
  const res = await runStatic({
    nodeModules,
    repoRoot: root,
    runGo: goStub({ pass: OPS.filter((o) => o !== "viewer"), fail: ["viewer"] }),
  });
  assert.equal(res.result, "DRIFT");
  assert.equal(res.documents.valid, 52);
  assert.match(res.errors.join("\n"), /Cannot query field "bogusField"/);
});

test("TestRunStaticDifferentFailedDocumentsIsBreak", async (t) => {
  const root = tmpDir(t);
  writeSchema(root, "type Query { a: String }\n");
  // graphql-js rejects users with an additive error.
  const nodeModules = writeBundle(root, allEntries({ users: "query users { a bogusField }" }));
  const res = await runStatic({
    nodeModules,
    repoRoot: root,
    runGo: goStub({ pass: OPS.filter((o) => o !== "users"), fail: ["users"] }),
  });
  assert.equal(res.result, "DRIFT"); // positive control: both fail the same document
  // Go failed viewer (accepted by graphql-js) while graphql-js rejects users (Go passed it).
  const mixed = await runStatic({
    nodeModules,
    repoRoot: root,
    runGo: goStub({ pass: OPS.filter((o) => o !== "viewer"), fail: ["viewer"] }),
  });
  assert.equal(mixed.result, "BREAK");
  assert.match(mixed.errors.join("\n"), /viewer: go test failed it, graphql-js accepts it/);
  assert.match(mixed.errors.join("\n"), /users: graphql-js rejects it, go test passed it/);
});

test("TestRunStaticTypeMismatchIsBreak", async (t) => {
  const root = tmpDir(t);
  writeSchema(root, "type Query { a: String n: Int f(x: String): String }\n");
  const nodeModules = writeBundle(root, allEntries({ viewer: "query viewer($x: Int) { f(x: $x) }" }));
  const res = await runStatic({
    nodeModules,
    repoRoot: root,
    runGo: goStub({ pass: OPS.filter((o) => o !== "viewer"), fail: ["viewer"] }),
  });
  assert.equal(res.result, "BREAK");
  assert.match(res.errors.join("\n"), /Variable "\$x" of type "Int" used in position expecting type "String"/);
});

test("TestRunStaticGoAndGraphqlDisagreeIsBreak", async (t) => {
  const root = tmpDir(t);
  writeSchema(root, "type Query { a: String }\n");
  const nodeModules = writeBundle(root, allEntries());
  // Go fails a document graphql-js accepts.
  const goFails = await runStatic({
    nodeModules,
    repoRoot: root,
    runGo: goStub({ pass: OPS.filter((o) => o !== "viewer"), fail: ["viewer"] }),
  });
  assert.equal(goFails.result, "BREAK");
  // Go passes a document graphql-js rejects.
  const bad = writeBundle(path.join(root, "bad"), allEntries({ viewer: "query viewer { a bogusField }" }));
  const goPasses = await runStatic({ nodeModules: bad, repoRoot: root, runGo: goStub() });
  assert.equal(goPasses.result, "BREAK");
});

test("TestRunStaticBaselineRemovedIsInfoAddedIsOk", async (t) => {
  const root = tmpDir(t);
  writeSchema(root, "type Query { a: String b: String }\n");
  const wide = writeBundle(path.join(root, "wide"), allEntries({ viewer: "query viewer { a b }" }));
  const narrow = writeBundle(path.join(root, "narrow"), allEntries());

  const info = await runStatic({ nodeModules: narrow, baselineNodeModules: wide, repoRoot: root, runGo: goStub() });
  assert.equal(info.result, "INFO");
  assert.deepEqual(info.removed, ["viewer: b"]);
  assert.deepEqual(info.added, []);

  const ok = await runStatic({ nodeModules: wide, baselineNodeModules: narrow, repoRoot: root, runGo: goStub() });
  assert.equal(ok.result, "OK");
  assert.deepEqual(ok.added, ["viewer: b"]);

  const argSchema = "type Query { a(after: String): String }\n";
  writeSchema(root, argSchema);
  const withArg = writeBundle(path.join(root, "witharg"), allEntries({ viewer: "query viewer { a(after: \"x\") }" }));
  const noArg = writeBundle(path.join(root, "noarg"), allEntries({ viewer: "query viewer { a }" }));
  const argRemoved = await runStatic({ nodeModules: noArg, baselineNodeModules: withArg, repoRoot: root, runGo: goStub() });
  assert.equal(argRemoved.result, "INFO");
  assert.deepEqual(argRemoved.removed, ["viewer: a(after)"]);
  assert.deepEqual(argRemoved.added, []);
  const argAdded = await runStatic({ nodeModules: withArg, baselineNodeModules: noArg, repoRoot: root, runGo: goStub() });
  assert.equal(argAdded.result, "OK");
  assert.deepEqual(argAdded.added, ["viewer: a(after)"]);
  assert.deepEqual(argAdded.removed, []);

  const same = await runStatic({ nodeModules: narrow, baselineNodeModules: narrow, repoRoot: root, runGo: goStub() });
  assert.equal(same.result, "OK");
  assert.deepEqual(same.diff.changed, []);
});
