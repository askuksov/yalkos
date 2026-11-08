import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

import { decide, issueBody, issueTitle, findOpen, manageIssue, main } from "./issue.mjs";
import { buildReport } from "./report.mjs";
import { mask, resetMasks } from "./mask.mjs";

const EXPECTED = "101.2.0";
const RUN_URL = "https://github.com/example/repo/actions/runs/42";
// Internal labels that must not appear in public texts.
const PUBLIC_TEXT_LABELS = /\bM\d+\b|\bpla[n]\b|Task \d|spec\s§|ADR[-]/;

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
const stages = [{ name: "sdk", steps: [{ name: "a", ok: true, message: "" }], error: null }];

function reportOf(result, version = "101.2.0") {
  const staticResult =
    result === "DRIFT"
      ? okStatic({ result: "DRIFT", added: ["IssueQuery: Issue.newThing"] })
      : result === "BREAK"
        ? okStatic({ result: "BREAK", errors: ["a document is broken"] })
        : result === "TOOLING"
          ? okStatic({ result: "TOOLING", documents: { extracted: 0, valid: 0 }, errors: ["0 documents"] })
          : okStatic({ result });
  return buildReport({ candidate: version, baseline: "101.1.0", staticResult, stages });
}

// A gh stand-in: records every call and answers the issue search with `open`.
function fakeGh(open = []) {
  const calls = [];
  const gh = (args, options = {}) => {
    calls.push({ args, input: options.input ?? null });
    // Issues without an author are the bot's own, as the workflow creates them.
    if (args[0] === "issue" && args[1] === "list") return JSON.stringify(open.map((i) => ({ author: { login: "app/github-actions" }, ...i })));
    return "";
  };
  return { gh, calls };
}

const mutations = (calls) => calls.filter((c) => !(c.args[0] === "issue" && c.args[1] === "list"));

test("TestIssueDecisions", () => {
  const version = "101.2.0";
  const same = (result) => ({ number: 7, title: `[compat] @linear/sdk ${version}: ${result}` });
  const title = (result) => `[compat] @linear/sdk ${version}: ${result}`;

  // No open issue: only a bad result creates one.
  for (const result of ["OK", "INFO"]) {
    assert.deepEqual(decide({ result, version, open: null }), { action: "none", title: title(result) });
  }
  for (const result of ["DRIFT", "BREAK", "TOOLING"]) {
    assert.deepEqual(decide({ result, version, open: null }), { action: "create", title: title(result) });
  }
  // An open issue: a good result closes it.
  for (const result of ["OK", "INFO"]) {
    assert.deepEqual(decide({ result, version, open: same("DRIFT") }), { action: "close", title: title(result) });
  }
  // A bad result with the same title changes nothing, a different one is a comment.
  for (const result of ["DRIFT", "BREAK", "TOOLING"]) {
    assert.deepEqual(decide({ result, version, open: same(result) }), { action: "none", title: title(result) });
    const other = result === "BREAK" ? "DRIFT" : "BREAK";
    assert.deepEqual(decide({ result, version, open: same(other) }), { action: "comment", title: title(result) });
  }
  assert.throws(() => decide({ result: "WAT", version, open: null }), /unknown result/);
});

test("create uses the title [compat] @linear/sdk <version>: <RESULT> and no labels", async () => {
  const { gh, calls } = fakeGh([]);
  const outcome = await manageIssue({ report: reportOf("DRIFT"), runUrl: RUN_URL, gh, expectedVersion: EXPECTED });
  assert.equal(outcome.action, "create");
  const made = mutations(calls);
  assert.equal(made.length, 1);
  const { args } = made[0];
  assert.deepEqual(args.slice(0, 2), ["issue", "create"]);
  assert.equal(args[args.indexOf("--title") + 1], "[compat] @linear/sdk 101.2.0: DRIFT");
  assert.equal(args[args.indexOf("--body-file") + 1], "-");
  assert.ok(!args.some((a) => /label/i.test(a)), "no label flags");
  assert.equal(issueTitle("101.2.0", "DRIFT"), "[compat] @linear/sdk 101.2.0: DRIFT");
  assert.ok(!PUBLIC_TEXT_LABELS.test(args[args.indexOf("--title") + 1]));
});

test("the search looks for the version in titles of open issues", async () => {
  const { gh, calls } = fakeGh([]);
  await manageIssue({ report: reportOf("OK"), runUrl: RUN_URL, gh, expectedVersion: EXPECTED });
  assert.deepEqual(calls[0].args, [
    "issue",
    "list",
    "--state",
    "open",
    "--search",
    '"[compat] @linear/sdk 101.2.0" in:title author:app/github-actions',
    "--json",
    "number,title,author",
    "--limit",
    "100",
  ]);
});

test("a report with result OK and no open issue calls gh nothing", async () => {
  const { gh, calls } = fakeGh([]);
  const outcome = await manageIssue({ report: reportOf("OK"), runUrl: RUN_URL, gh, expectedVersion: EXPECTED });
  assert.equal(outcome.action, "none");
  assert.equal(mutations(calls).length, 0);
});

test("the version match is exact: 101.1.0 does not match 101.1.01 or 101.1.0-beta", async () => {
  const lookalikes = [
    { number: 1, title: "[compat] @linear/sdk 101.1.01: DRIFT" },
    { number: 2, title: "[compat] @linear/sdk 101.1.0-beta.1: DRIFT" },
    { number: 3, title: "[compat] @linear/sdk 101.1.0 is mentioned here" },
    { number: 4, title: "unrelated [compat] @linear/sdk 101.1.0: DRIFT" },
  ];
  const { gh } = fakeGh(lookalikes);
  assert.equal(findOpen("101.1.0", gh), null);
  const hit = { number: 9, title: "[compat] @linear/sdk 101.1.0: BREAK" };
  const second = fakeGh([...lookalikes, hit]);
  assert.deepEqual(findOpen("101.1.0", second.gh), hit);

  // A lookalike open issue does not stop a new one for the real version.
  const third = fakeGh(lookalikes);
  const outcome = await manageIssue({ report: reportOf("DRIFT", "101.1.0"), runUrl: RUN_URL, gh: third.gh, expectedVersion: "101.1.0" });
  assert.equal(outcome.action, "create");
});

test("a changed result adds a comment and an unchanged one does nothing", async () => {
  const open = [{ number: 12, title: "[compat] @linear/sdk 101.2.0: DRIFT" }];

  const changed = fakeGh(open);
  const outcome = await manageIssue({ report: reportOf("BREAK"), runUrl: RUN_URL, gh: changed.gh , expectedVersion: EXPECTED });
  assert.equal(outcome.action, "comment");
  const [call] = mutations(changed.calls);
  assert.deepEqual(call.args, ["issue", "comment", "12", "--body-file", "-"]);
  assert.match(call.input, /RESULT: BREAK/);
  assert.ok(call.input.includes(RUN_URL));

  const same = fakeGh(open);
  const quiet = await manageIssue({ report: reportOf("DRIFT"), runUrl: RUN_URL, gh: same.gh , expectedVersion: EXPECTED });
  assert.equal(quiet.action, "none");
  assert.equal(mutations(same.calls).length, 0);
});

test("a changed result comments and renames the open issue, so the next run finds nothing to do", async () => {
  const open = [{ number: 12, title: "[compat] @linear/sdk 101.2.0: DRIFT" }];
  const first = fakeGh(open);
  const outcome = await manageIssue({ report: reportOf("BREAK"), runUrl: RUN_URL, gh: first.gh, expectedVersion: EXPECTED });
  assert.equal(outcome.action, "comment");
  const made = mutations(first.calls);
  assert.equal(made.length, 2);
  assert.deepEqual(made[0].args, ["issue", "comment", "12", "--body-file", "-"]);
  assert.match(made[0].input, /RESULT: BREAK/);
  assert.deepEqual(made[1].args, ["issue", "edit", "12", "--title", "[compat] @linear/sdk 101.2.0: BREAK"]);

  // The next daily run sees the renamed issue and stays quiet.
  const renamed = fakeGh([{ number: 12, title: "[compat] @linear/sdk 101.2.0: BREAK" }]);
  const next = await manageIssue({ report: reportOf("BREAK"), runUrl: RUN_URL, gh: renamed.gh, expectedVersion: EXPECTED });
  assert.equal(next.action, "none");
  assert.equal(mutations(renamed.calls).length, 0);

  // Positive control: an unchanged title is quiet too.
  const same = fakeGh(open);
  assert.equal((await manageIssue({ report: reportOf("DRIFT"), runUrl: RUN_URL, gh: same.gh, expectedVersion: EXPECTED })).action, "none");
  assert.equal(mutations(same.calls).length, 0);
});

test("a forged report never renames or comments on an open issue", async () => {
  const open = [{ number: 12, title: "[compat] @linear/sdk 101.2.0: DRIFT" }];
  for (const report of [{ ...reportOf("BREAK"), candidate: "--title x" }, { ...reportOf("BREAK"), result: "FINE" }, reportOf("BREAK", "999.0.0")]) {
    const { gh, calls } = fakeGh(open);
    await assert.rejects(manageIssue({ report, runUrl: RUN_URL, gh, expectedVersion: EXPECTED }), /invalid|expected/);
    assert.equal(calls.length, 0);
  }
});

test("a passing result closes the open issue with a comment", async () => {
  const { gh, calls } = fakeGh([{ number: 5, title: "[compat] @linear/sdk 101.2.0: BREAK" }]);
  const outcome = await manageIssue({ report: reportOf("OK"), runUrl: RUN_URL, gh, expectedVersion: EXPECTED });
  assert.equal(outcome.action, "close");
  const [call] = mutations(calls);
  assert.deepEqual(call.args.slice(0, 3), ["issue", "close", "5"]);
  const comment = call.args[call.args.indexOf("--comment") + 1];
  assert.match(comment, /now passes/i);
  assert.ok(comment.includes(RUN_URL));
  assert.ok(!PUBLIC_TEXT_LABELS.test(comment));
});

test("the issue body contains the summary table and the run link, no registered secret", async (t) => {
  t.after(resetMasks);
  const secret = "yal_api_testsecretvalue123";
  mask(secret);
  const report = buildReport({
    candidate: "101.2.0",
    baseline: "101.1.0",
    staticResult: okStatic({
      result: "DRIFT",
      added: ["IssueQuery: Issue.newThing", "IssueQuery: Issue.other"],
      errors: [`a failure mentioning ${secret}`],
    }),
    stages,
  });
  const { gh, calls } = fakeGh([]);
  await manageIssue({ report, runUrl: RUN_URL, gh, expectedVersion: EXPECTED });
  const body = mutations(calls)[0].input;
  assert.match(body, /\| operation \| change \|/);
  assert.match(body, /\| IssueQuery \| \+Issue\.newThing \+Issue\.other \|/);
  assert.match(body, /RESULT: DRIFT \(additive\)/);
  assert.ok(body.includes(RUN_URL));
  assert.ok(!body.includes(secret));
  assert.ok(!PUBLIC_TEXT_LABELS.test(body));
});

const MAIN_OPTS = (gh) => ({ gh, log: () => {} });
const args = (file, version = EXPECTED) => [file, RUN_URL, "--expected-version", version];

test("a report without a candidate uses the expected version", async () => {
  const report = buildReport({
    candidate: null,
    baseline: "101.1.0",
    staticResult: okStatic({ result: "TOOLING", documents: { extracted: 0, valid: 0 }, errors: ["cannot install"] }),
    stages: [],
  });
  const { gh, calls } = fakeGh([]);
  const outcome = await manageIssue({ report, runUrl: RUN_URL, gh, expectedVersion: "102.0.0-beta.1" });
  assert.equal(outcome.action, "create");
  const made = mutations(calls)[0].args;
  assert.equal(made[made.indexOf("--title") + 1], "[compat] @linear/sdk 102.0.0-beta.1: TOOLING");
});

test("a report for another version than the expected one is refused without gh", async () => {
  const { gh, calls } = fakeGh([]);
  await assert.rejects(
    manageIssue({ report: reportOf("BREAK", "101.3.0"), runUrl: RUN_URL, gh, expectedVersion: EXPECTED }),
    /expected 101\.2\.0/,
  );
  assert.equal(calls.length, 0);
  // The same version proceeds.
  const outcome = await manageIssue({ report: reportOf("BREAK", EXPECTED), runUrl: RUN_URL, gh, expectedVersion: EXPECTED });
  assert.equal(outcome.action, "create");
});

test("an invalid expected version is refused without gh", async () => {
  for (const expectedVersion of ["latest", "1.2", "101.2.0; rm", "--title x", " ", "", undefined, null]) {
    const { gh, calls } = fakeGh([]);
    await assert.rejects(manageIssue({ report: reportOf("DRIFT"), runUrl: RUN_URL, gh, expectedVersion }), /invalid expected/, String(expectedVersion));
    assert.equal(calls.length, 0);
  }
});

test("main reads the report file and fails on a missing or broken one", async (t) => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "compat-issue-test-"));
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
  const file = path.join(dir, "compat-report.json");
  fs.writeFileSync(file, JSON.stringify(reportOf("OK")));
  const { gh, calls } = fakeGh([]);
  const logs = [];
  assert.equal(await main(args(file), { gh, log: (m) => logs.push(m) }), 0);
  assert.equal(mutations(calls).length, 0);
  assert.ok(logs.some((l) => l.includes("none")));

  assert.equal(await main(args(path.join(dir, "missing.json")), MAIN_OPTS(gh)), 1);
  fs.writeFileSync(file, "{ not json");
  assert.equal(await main(args(file), MAIN_OPTS(gh)), 1);
  // The expected version is required.
  assert.equal(await main([file, RUN_URL], MAIN_OPTS(gh)), 1);
  assert.equal(await main([file], MAIN_OPTS(gh)), 1);
  assert.equal(await main([file, RUN_URL, "--expected-version"], MAIN_OPTS(gh)), 1);
  assert.equal(await main([file, RUN_URL, "--other", EXPECTED], MAIN_OPTS(gh)), 1);
});

test("an invalid report never reaches gh", async () => {
  const bad = [
    { ...reportOf("DRIFT"), candidate: "1.0.0; rm" },
    { ...reportOf("DRIFT"), candidate: "--title x" },
    { ...reportOf("DRIFT"), candidate: "latest" },
    { ...reportOf("DRIFT"), baseline: "$(id)" },
    { ...reportOf("DRIFT"), result: "FINE" },
    { ...reportOf("DRIFT"), result: undefined },
    { ...reportOf("DRIFT"), errors: "not a list" },
    { ...reportOf("DRIFT"), documents: { extracted: "53", valid: 1 } },
    { ...reportOf("DRIFT"), steps: [null] },
    null,
  ];
  for (const report of bad) {
    const { gh, calls } = fakeGh([]);
    await assert.rejects(manageIssue({ report, runUrl: RUN_URL, gh, expectedVersion: EXPECTED }), /invalid|expected/, JSON.stringify(report)?.slice(0, 80));
    assert.equal(calls.length, 0);
  }
  const { gh, calls } = fakeGh([]);
  await assert.rejects(manageIssue({ report: reportOf("DRIFT"), runUrl: "javascript:alert(1)", gh, expectedVersion: EXPECTED }), /invalid run URL/);
  assert.equal(calls.length, 0);
});

test("main exits non-zero and calls gh nowhere for an untrusted report", async (t) => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "compat-issue-test-"));
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
  const file = path.join(dir, "compat-report.json");
  const { gh, calls } = fakeGh([]);
  fs.writeFileSync(file, JSON.stringify({ ...reportOf("BREAK"), candidate: "--title x" }));
  assert.equal(await main(args(file), MAIN_OPTS(gh)), 1);
  fs.writeFileSync(file, JSON.stringify(reportOf("BREAK", "999.0.0")));
  assert.equal(await main(args(file), MAIN_OPTS(gh)), 1);
  fs.writeFileSync(file, JSON.stringify(reportOf("BREAK")));
  assert.equal(await main(args(file, "1.2"), MAIN_OPTS(gh)), 1);
  assert.equal(await main(args(file, "latest"), MAIN_OPTS(gh)), 1);
  assert.equal(calls.length, 0);
  assert.equal(await main(args(file), MAIN_OPTS(gh)), 0);
  assert.equal(mutations(calls).length, 1);
});

test("the whole summary sits in one code fence that report texts cannot close", async () => {
  const errors = Array.from({ length: 30 }, (_, i) => `error ${i} @everyone ` + "x".repeat(5000));
  const evil = "](https://evil.example) @someone ![x](y) <img src=x> ``` ```` ";
  const report = buildReport({
    candidate: "101.2.0",
    baseline: "101.1.0",
    staticResult: okStatic({ result: "BREAK", errors, added: [`IssueQuery: ${evil}`], removed: [`IssueQuery: ${evil}`] }),
    stages: [
      { name: `sdk ${evil}`, steps: [{ name: `a ${evil}`, ok: false, message: `boom @team ${evil}` }], error: null },
    ],
  });
  const { gh, calls } = fakeGh([]);
  await manageIssue({ report, runUrl: RUN_URL, gh, expectedVersion: EXPECTED });
  const body = mutations(calls)[0].input;
  // At most 20 errors, each cut at 2000 characters, and the rest only counted.
  assert.equal((body.match(/error \d+ /g) ?? []).length, 20);
  assert.ok(!/x{2001}/.test(body));
  assert.match(body, /\.\.\. 10 more errors in the report/);
  // Exactly one fence, and nothing but the heading and the run link outside it.
  const parts = body.split(/^```\n/m);
  assert.equal(parts.length, 3, "opening and closing fence only");
  const [before, inside, after] = [parts[0], parts[1], parts[2]];
  assert.ok(inside.includes("@everyone") && inside.includes("boom @team") && inside.includes("evil.example"));
  assert.ok(!inside.includes("`"), "no backtick survives inside the fence");
  assert.equal(before, "Daily check of `@linear/sdk` 101.2.0: BREAK\n\n");
  assert.equal(after, `\nRun: ${RUN_URL}\n`);
});

test("the fence is longer than any backtick run it holds", () => {
  // The texts are cleaned of backticks first; the fence still guards on its own.
  const clean = {
    candidate: "101.2.0",
    baseline: null,
    result: "DRIFT",
    documents: { extracted: 53, valid: 53 },
    added: [],
    removed: [],
    retyped: [],
    steps: [],
    errors: [],
  };
  assert.match(issueBody(clean, "101.2.0", RUN_URL), /^Daily check of `@linear\/sdk` 101\.2\.0: DRIFT\n\n```\n/);
  const nasty = { ...clean, errors: ["```` and `````"] };
  const fenced = issueBody(nasty, "101.2.0", RUN_URL);
  assert.match(fenced, /^`{6}\n/m, "a fence of 6 around a run of 5");
  assert.equal(fenced.split(/^`{6}$/m).length, 3);
});

// A gh stand-in that returns the given list as is (no default author).
function rawGh(list) {
  const calls = [];
  const gh = (args, options = {}) => {
    calls.push({ args, input: options.input ?? null });
    return args[0] === "issue" && args[1] === "list" ? JSON.stringify(list) : "";
  };
  return { gh, calls };
}

test("only an issue opened by the workflow's bot is found, updated or closed", async () => {
  const title = "[compat] @linear/sdk 101.2.0: DRIFT";
  const foreign = [
    { number: 3, title, author: { login: "someone" } },
    { number: 4, title, author: { login: "github-actions" } },
    { number: 5, title },
    { number: 6, title, author: null },
  ];
  assert.equal(findOpen("101.2.0", rawGh(foreign).gh), null);

  // A same-titled issue by another author is ignored, so a new one is created.
  const a = rawGh(foreign);
  assert.equal((await manageIssue({ report: reportOf("DRIFT"), runUrl: RUN_URL, gh: a.gh, expectedVersion: EXPECTED })).action, "create");
  // A passing run does not close a foreign issue either.
  const b = rawGh(foreign);
  assert.equal((await manageIssue({ report: reportOf("OK"), runUrl: RUN_URL, gh: b.gh, expectedVersion: EXPECTED })).action, "none");
  assert.equal(mutations(b.calls).length, 0);

  // The bot's own issue is found, and no duplicate is created.
  const mine = { number: 9, title, author: { login: "app/github-actions" } };
  assert.deepEqual(findOpen("101.2.0", rawGh([...foreign, mine]).gh), { number: 9, title });
  const c = rawGh([...foreign, mine]);
  assert.equal((await manageIssue({ report: reportOf("DRIFT"), runUrl: RUN_URL, gh: c.gh, expectedVersion: EXPECTED })).action, "none");
  assert.equal(mutations(c.calls).length, 0);
});

test("an oversized summary is cut below the GitHub limit, inside the one fence, with a notice", () => {
  const clean = {
    candidate: "101.2.0",
    baseline: null,
    result: "BREAK",
    documents: { extracted: 53, valid: 53 },
    added: [],
    removed: [],
    retyped: [],
    steps: Array.from({ length: 500 }, (_, i) => ({ stage: "sdk", name: `step ${i}`, ok: false, message: "m".repeat(2000) })),
    errors: [],
  };
  const body = issueBody(clean, "101.2.0", RUN_URL);
  assert.ok(body.length < 65000, `body has ${body.length} characters`);
  const parts = body.split(/^```\n/m);
  assert.equal(parts.length, 3, "opening and closing fence only");
  assert.ok(parts[1].trimEnd().endsWith("... truncated; see the uploaded report"));
  assert.ok(!parts[1].includes("`"));
  assert.equal(parts[2], `\nRun: ${RUN_URL}\n`);

  // A small summary is left whole.
  const small = issueBody({ ...clean, steps: [] }, "101.2.0", RUN_URL);
  assert.ok(!small.includes("truncated"));
});
