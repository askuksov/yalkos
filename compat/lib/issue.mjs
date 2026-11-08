#!/usr/bin/env node
// Opens, updates or closes the issue for one @linear/sdk version after a
// scheduled run.
//
//   node issue.mjs <compat-report.json> <run-url> --expected-version <version>
//
// There is one issue per SDK version, titled "[compat] @linear/sdk <version>:
// <RESULT>". A result that needs attention opens it, a changed result adds a
// comment and renames it, and a passing result closes it. The gh CLI does the
// work (in CI it uses the job's token); texts go in through stdin, never through
// a shell.
import fs from "node:fs";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";

import { render } from "./report.mjs";
import { scrub } from "./mask.mjs";
import { isSemver } from "./version.mjs";

const PASSING = new Set(["OK", "INFO"]);
const ATTENTION = new Set(["DRIFT", "BREAK", "TOOLING"]);
const PREFIX = "[compat] @linear/sdk";

// The report comes from a run of code we do not control, so it is data to be
// checked, never trusted: every field is validated and every text bounded.
const MAX_ERRORS = 20;
const MAX_TEXT = 2000;
const MAX_ENTRIES = 200;
const MAX_ENTRY = 300;
const MAX_STEPS = 500;

export const issueTitle = (version, result) => `${PREFIX} ${version}: ${result}`;

// Decides what to do with the open issue of a version (null when there is none).
// Returns { action: "create" | "comment" | "close" | "none", title }, where
// title is the title the issue has for this result.
export function decide({ result, version, open }) {
  const title = issueTitle(version, result);
  if (PASSING.has(result)) return { action: open ? "close" : "none", title };
  if (!ATTENTION.has(result)) throw new Error(`unknown result class: ${result}`);
  if (!open) return { action: "create", title };
  return { action: open.title === title ? "none" : "comment", title };
}

// Runs gh with an argument array (no shell). Returns stdout.
export function runGh(args, { input } = {}) {
  const r = spawnSync("gh", args, { input, encoding: "utf8", stdio: ["pipe", "pipe", "pipe"] });
  if (r.error) throw new Error(`cannot run gh: ${r.error.message}`);
  if (r.status !== 0) throw new Error(scrub(`gh ${args.slice(0, 2).join(" ")} exited with ${r.status}: ${r.stderr.trim()}`));
  return r.stdout;
}

// The author the workflow's own token shows up as (gh prints the bot like this).
export const BOT_LOGIN = "app/github-actions";

// Finds the open issue of a version, among the issues the workflow itself
// opened: an issue of someone else with the same title is not ours to update or
// close. The search is fuzzy, so the result is filtered again: only
// "[compat] @linear/sdk <version>:" counts, and 101.1.0 never matches 101.1.01
// or 101.1.0-beta.
export function findOpen(version, gh = runGh) {
  const out = gh([
    "issue",
    "list",
    "--state",
    "open",
    "--search",
    `"${PREFIX} ${version}" in:title author:${BOT_LOGIN}`,
    "--json",
    "number,title,author",
    "--limit",
    "100",
  ]);
  const issues = JSON.parse(out || "[]");
  const exact = `${PREFIX} ${version}:`;
  const found = issues.find((i) => i?.author?.login === BOT_LOGIN && typeof i.title === "string" && i.title.startsWith(exact));
  return found ? { number: found.number, title: found.title } : null;
}

// A bounded text without backticks (so it cannot end a code block early).
const clip = (text, max) => {
  const t = String(text).replace(/`/g, "'");
  return t.length > max ? `${t.slice(0, max)}...` : t;
};

function list(value, what, max) {
  if (!Array.isArray(value) || value.some((x) => typeof x !== "string")) throw new Error(`invalid report: ${what} must be a list of texts`);
  return value.slice(0, max);
}

// Returns a clean copy of the report, or throws. Nothing outside the copy is
// ever written into an issue. The version of the issue is the expected one,
// which comes from the workflow, never from the report: a report that names
// another candidate is refused.
export function validateReport(report, expectedVersion) {
  if (!isSemver(expectedVersion)) throw new Error("invalid expected SDK version");
  if (!report || typeof report !== "object") throw new Error("invalid report: not an object");
  if (!ATTENTION.has(report.result) && !PASSING.has(report.result)) throw new Error("invalid report: unknown result class");
  const candidate = report.candidate ?? null;
  if (candidate !== null && !isSemver(candidate)) throw new Error("invalid report: candidate is not a plain version");
  if (candidate !== null && candidate !== expectedVersion) {
    throw new Error(`the report is for @linear/sdk ${candidate}, expected ${expectedVersion}`);
  }
  const baseline = report.baseline ?? null;
  if (baseline !== null && !isSemver(baseline)) throw new Error("invalid report: baseline is not a plain version");
  const docs = report.documents ?? {};
  for (const n of [docs.extracted, docs.valid]) {
    if (!Number.isInteger(n) || n < 0) throw new Error("invalid report: document counts must be integers");
  }
  if (!Array.isArray(report.steps) || report.steps.length > MAX_STEPS) throw new Error("invalid report: steps must be a list");
  const steps = report.steps.map((st) => {
    if (!st || typeof st !== "object" || typeof st.ok !== "boolean") throw new Error("invalid report: a step is malformed");
    return { stage: clip(st.stage, MAX_ENTRY), name: clip(st.name, MAX_ENTRY), ok: st.ok, message: clip(st.message ?? "", MAX_TEXT) };
  });
  const errors = list(report.errors ?? [], "errors", Infinity);
  const shownErrors = errors.slice(0, MAX_ERRORS).map((e) => clip(e, MAX_TEXT));
  if (errors.length > MAX_ERRORS) shownErrors.push(`... ${errors.length - MAX_ERRORS} more errors in the report`);
  return {
    version: expectedVersion,
    clean: {
      candidate,
      baseline,
      result: report.result,
      documents: { extracted: docs.extracted, valid: docs.valid },
      added: list(report.added ?? [], "added", MAX_ENTRIES).map((e) => clip(e, MAX_ENTRY)),
      removed: list(report.removed ?? [], "removed", MAX_ENTRIES).map((e) => clip(e, MAX_ENTRY)),
      retyped: list(report.retyped ?? [], "retyped", MAX_ENTRIES).map((e) => clip(e, MAX_ENTRY)),
      steps,
      errors: shownErrors,
    },
  };
}

// A fenced block that the text cannot close early.
function fence(text) {
  const longest = Math.max(0, ...(text.match(/`+/g) ?? []).map((r) => r.length));
  const mark = "`".repeat(Math.max(3, longest + 1));
  return `${mark}\n${text}\n${mark}`;
}

// The issue text: a fixed heading, the whole summary inside one code block (so
// nothing taken from the report can link, mention or render), and the run link.
export function issueBody(clean, version, runUrl) {
  const heading = `Daily check of \`@linear/sdk\` ${version}: ${clean.result}`;
  const summary = scrub(render(clean).markdown.trimEnd());
  return `${heading}\n\n${fence(summary)}\n\nRun: ${runUrl}\n`;
}

// Applies the decision for a report. expectedVersion is the version the
// workflow asked for; the search, the title and the close all use it.
export async function manageIssue({ report, runUrl, gh = runGh, expectedVersion }) {
  // Everything is validated and rendered before the first gh call.
  const { version, clean } = validateReport(report, expectedVersion);
  if (typeof runUrl !== "string" || !/^https:\/\/[^\s]+$/.test(runUrl)) throw new Error("invalid run URL");
  const text = issueBody(clean, version, runUrl);
  const open = findOpen(version, gh);
  const decision = decide({ result: clean.result, version, open });
  switch (decision.action) {
    case "create":
      gh(["issue", "create", "--title", decision.title, "--body-file", "-"], { input: text });
      break;
    case "comment":
      gh(["issue", "comment", String(open.number), "--body-file", "-"], { input: text });
      // The title follows the result, so the next run finds nothing to change.
      gh(["issue", "edit", String(open.number), "--title", decision.title]);
      break;
    case "close":
      gh(["issue", "close", String(open.number), "--comment", `\`@linear/sdk\` ${version} now passes. Run: ${runUrl}`]);
      break;
  }
  return { ...decision, number: open?.number ?? null };
}

// Returns the exit code.
export async function main(argv, { gh = runGh, log = console.log } = {}) {
  const [reportFile, runUrl, flag, expectedVersion, ...rest] = argv;
  if (!reportFile || !runUrl || flag !== "--expected-version" || !expectedVersion || rest.length > 0) {
    console.error("usage: node issue.mjs <compat-report.json> <run-url> --expected-version <version>");
    return 1;
  }
  let report;
  try {
    report = JSON.parse(fs.readFileSync(reportFile, "utf8"));
  } catch (err) {
    console.error(scrub(`cannot read the report ${reportFile}: ${err.message}`));
    return 1;
  }
  try {
    const out = await manageIssue({ report, runUrl, gh, expectedVersion });
    log(`issue for @linear/sdk ${out.title.slice(PREFIX.length + 1)}, action ${out.action}`);
    return 0;
  } catch (err) {
    console.error(scrub(err.message));
    return 1;
  }
}

function isEntryPoint() {
  if (!process.argv[1]) return false;
  try {
    return fs.realpathSync(process.argv[1]) === fs.realpathSync(fileURLToPath(import.meta.url));
  } catch {
    return false;
  }
}

if (isEntryPoint()) {
  process.exitCode = await main(process.argv.slice(2));
}
