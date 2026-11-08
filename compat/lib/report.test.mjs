import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

import { classify, buildReport, render, exitCode, writeReport } from "./report.mjs";
import { mask, resetMasks } from "./mask.mjs";

const okStatic = (extra = {}) => ({
  result: "OK",
  documents: { extracted: 53, valid: 53 },
  diff: null,
  added: [],
  removed: [],
  retyped: [],
  errors: [],
  ...extra,
});

const stage = (name, steps) => ({ name, steps, error: null });
const pass = (name) => ({ name, ok: true, message: "" });
const fail = (name, message) => ({ name, ok: false, message });

function tmpDir(t) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "compat-report-test-"));
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
  return dir;
}

test("classify: all steps pass and no diff is OK", () => {
  const stages = [stage("bootstrap", [pass("a"), pass("b")])];
  assert.equal(classify({ staticResult: okStatic(), stages }), "OK");
  assert.equal(classify({ staticResult: okStatic({ result: "INFO" }), stages }), "INFO");
});

test("classify: a failed step with 'Cannot query field' is DRIFT, any other failed step is BREAK", () => {
  const additive = [stage("sdk", [pass("a"), fail("b", 'Cannot query field "newThing" on type "Team".')])];
  assert.equal(classify({ staticResult: okStatic(), stages: additive }), "DRIFT");
  const unknownArg = [stage("sdk", [fail("b", 'Unknown argument "extra" on field "Query.teams".')])];
  assert.equal(classify({ staticResult: okStatic(), stages: unknownArg }), "DRIFT");
  const other = [stage("sdk", [fail("b", "expected 3, got 2")])];
  assert.equal(classify({ staticResult: okStatic(), stages: other }), "BREAK");
  // One non-additive failure next to an additive one is a BREAK.
  const mixed = [stage("sdk", [fail("a", 'Cannot query field "x" on type "T".'), fail("b", "boom")])];
  assert.equal(classify({ staticResult: okStatic(), stages: mixed }), "BREAK");
  // Static findings count too.
  const passing = [stage("sdk", [pass("a")])];
  assert.equal(classify({ staticResult: okStatic({ result: "DRIFT" }), stages: passing }), "DRIFT");
  assert.equal(classify({ staticResult: okStatic({ result: "BREAK" }), stages: passing }), "BREAK");
  assert.equal(classify({ staticResult: okStatic({ result: "DRIFT" }), stages: other }), "BREAK");
});

test("classify: a stage with 0 steps is TOOLING, and TOOLING from static wins", () => {
  assert.equal(classify({ staticResult: okStatic(), stages: [stage("sdk", [])] }), "TOOLING");
  assert.equal(classify({ staticResult: okStatic(), stages: [stage("a", [pass("x")]), stage("b", [])] }), "TOOLING");
  // No stage at all is never green either.
  assert.equal(classify({ staticResult: okStatic(), stages: [] }), "TOOLING");
  // A run error (server did not start, bootstrap failed) is TOOLING.
  assert.equal(classify({ staticResult: okStatic(), stages: [stage("a", [pass("x")])], errors: ["server not ready"] }), "TOOLING");
  const broken = [stage("sdk", [fail("a", "boom")])];
  assert.equal(classify({ staticResult: okStatic({ result: "TOOLING" }), stages: broken }), "TOOLING");
  assert.equal(classify({ staticResult: okStatic({ result: "TOOLING" }), stages: [stage("a", [pass("x")])] }), "TOOLING");
});

test("render shows one line per step and the summary table shape of the spec", () => {
  resetMasks();
  const staticResult = okStatic({
    result: "DRIFT",
    documents: { extracted: 53, valid: 51 },
    diff: { changed: [{ op: "teams" }, { op: "issueLabels" }], orderOnly: 0 },
    added: ["teams: nodes.exampleField", "teams: nodes.other", "issueLabels: nodes.groupType"],
    removed: [],
    retyped: [],
    errors: ['teams.graphql: Cannot query field "exampleField" on type "Team".'],
  });
  const stages = [
    stage("bootstrap", [pass("viewer"), pass("altered key")]),
    stage("sdk", [fail("lazy getter", 'Cannot query field "x" on type "T".')]),
  ];
  const report = buildReport({ candidate: "101.2.0", baseline: "101.1.0", staticResult, stages });
  assert.equal(report.result, "DRIFT");
  const out = render(report);

  const stepLines = out.console.split("\n").filter((l) => /^(PASS|FAIL) /.test(l));
  assert.deepEqual(stepLines, [
    "PASS bootstrap/viewer",
    "PASS bootstrap/altered key",
    'FAIL sdk/lazy getter: Cannot query field "x" on type "T".',
  ]);

  const lines = out.markdown.split("\n");
  assert.equal(lines[0], "@linear/sdk candidate 101.2.0 vs baseline 101.1.0");
  assert.equal(lines[1], "documents extracted: 53/53   static validation: 51/53 valid   steps: 2/3 passed");
  assert.equal(lines[2], "semantic diff: 2 documents changed, +field paths 3, +args 0, +vars 0, -fields 0, retyped 0");
  assert.equal(lines[3], "RESULT: DRIFT (additive)");
  assert.ok(lines.includes("| operation | change |"));
  assert.ok(lines.includes("| teams | +nodes.exampleField +nodes.other |"));
  assert.ok(lines.includes("| issueLabels | +nodes.groupType |"));
  // The console carries the same header lines.
  assert.ok(out.console.includes("RESULT: DRIFT (additive)"));

  // A pinned run has no baseline: the diff line says so.
  const pinned = render(buildReport({ candidate: "101.1.0", baseline: null, staticResult: okStatic(), stages: [stage("a", [pass("x")])] }));
  assert.equal(pinned.markdown.split("\n")[0], "@linear/sdk 101.1.0 (pinned)");
  assert.match(pinned.markdown, /semantic diff: not run/);
  assert.match(pinned.markdown, /RESULT: OK/);
});

test("the report JSON has exactly the documented keys and contains no registered secret", (t) => {
  resetMasks();
  const secret = "s3cr3t-Value-XYZ-123";
  const prevSummary = process.env.GITHUB_STEP_SUMMARY;
  const summaryFile = path.join(tmpDir(t), "step-summary.md");
  process.env.GITHUB_STEP_SUMMARY = summaryFile;
  t.after(() => {
    if (prevSummary === undefined) delete process.env.GITHUB_STEP_SUMMARY;
    else process.env.GITHUB_STEP_SUMMARY = prevSummary;
    resetMasks();
  });
  mask(secret);

  const staticResult = okStatic({
    result: "BREAK",
    added: [`teams: nodes.${secret}`],
    errors: [`validation said ${secret}`],
  });
  const stages = [stage("sdk", [pass("ok step"), fail("bad step", `request carried ${secret} in a header`)])];
  stages[0].error = `stage output ${secret}`;
  const report = buildReport({ candidate: "101.2.0", baseline: "101.1.0", staticResult, stages, errors: [`run error ${secret}`] });

  assert.deepEqual(Object.keys(report).sort(), [
    "added", "baseline", "candidate", "documents", "errors", "removed", "result", "retyped", "steps",
  ]);
  assert.deepEqual(Object.keys(report.documents).sort(), ["extracted", "valid"]);
  assert.deepEqual(Object.keys(report.steps[0]).sort(), ["message", "name", "ok", "stage"]);

  const dir = path.join(tmpDir(t), "out");
  const written = writeReport(dir, report);
  assert.equal(written.jsonPath, path.join(dir, "compat-report.json"));
  const rendered = render(report);
  for (const [label, text] of [
    ["report object", JSON.stringify(report)],
    ["json file", fs.readFileSync(written.jsonPath, "utf8")],
    ["summary file", fs.readFileSync(written.markdownPath, "utf8")],
    ["step summary", fs.readFileSync(summaryFile, "utf8")],
    ["console", rendered.console],
    ["markdown", rendered.markdown],
    ["json text", rendered.json],
  ]) {
    assert.ok(!text.includes(secret), `${label} contains a registered secret`);
  }
  assert.ok(fs.readFileSync(summaryFile, "utf8").includes(`RESULT: ${report.result}`));
  // The file on disk parses back to the same keys.
  assert.deepEqual(Object.keys(JSON.parse(fs.readFileSync(written.jsonPath, "utf8"))).sort(), Object.keys(report).sort());
});

test("exit code: pinned DRIFT exits 1 (the pin must never drift); latest DRIFT exits 0; BREAK and TOOLING exit 1 in both modes", () => {
  for (const r of ["OK", "INFO"]) {
    assert.equal(exitCode(r, "pinned"), 0, `${r} pinned`);
    assert.equal(exitCode(r, "latest"), 0, `${r} latest`);
  }
  assert.equal(exitCode("DRIFT", "pinned"), 1);
  assert.equal(exitCode("DRIFT", "latest"), 0);
  for (const r of ["BREAK", "TOOLING"]) {
    assert.equal(exitCode(r, "pinned"), 1, `${r} pinned`);
    assert.equal(exitCode(r, "latest"), 1, `${r} latest`);
  }
  assert.throws(() => exitCode("MAYBE", "pinned"));
});
