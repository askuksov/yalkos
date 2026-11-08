// Result classes, report rendering and the exit code of a compatibility run.
import fs from "node:fs";
import path from "node:path";
import { scrub } from "./mask.mjs";

const ADDITIVE_STEP = /Cannot query field |Unknown argument |Unknown type /;
const ERRORS_SHOWN = 30;

// Combines the static result and the stages into one class:
// TOOLING > BREAK > DRIFT > INFO > OK. A run without a stage, a stage without a
// step and a run error are TOOLING: a check that did not run is never green.
// A failed step flagged `prerequisite` only lacked the result of an earlier step; it
// follows that step's failure instead of deciding the class (unless nothing else failed).
// stages: [{ name, steps: [{ name, ok, message, prerequisite? }], error }], errors: run errors.
export function classify({ staticResult, stages, errors = [] }) {
  if (staticResult.result === "TOOLING") return "TOOLING";
  if (errors.length > 0 || stages.length === 0 || stages.some((s) => s.steps.length === 0)) return "TOOLING";
  const failed = stages.flatMap((s) => s.steps).filter((s) => !s.ok);
  const causes = failed.filter((s) => !s.prerequisite);
  const decisive = causes.length > 0 ? causes : failed;
  if (staticResult.result === "BREAK" || decisive.some((s) => !ADDITIVE_STEP.test(s.message))) return "BREAK";
  if (staticResult.result === "DRIFT" || failed.length > 0) return "DRIFT";
  return staticResult.result === "INFO" ? "INFO" : "OK";
}

// The process exit code. A pinned run must never drift; a latest run reports
// drift without failing.
export function exitCode(result, mode) {
  switch (result) {
    case "OK":
    case "INFO":
      return 0;
    case "DRIFT":
      return mode === "pinned" ? 1 : 0;
    case "BREAK":
    case "TOOLING":
      return 1;
    default:
      throw new Error(`unknown result class: ${result}`);
  }
}

// Builds the report that goes to compat-report.json. Every text is scrubbed of
// registered secrets here, so nothing downstream can leak one.
export function buildReport({ candidate, baseline, staticResult, stages, errors = [] }) {
  const list = (xs) => (xs ?? []).map(scrub);
  const stageErrors = stages.filter((s) => s.error).map((s) => `${s.name}: ${s.error}`);
  return {
    candidate: candidate ?? null,
    baseline: baseline ?? null,
    result: classify({ staticResult, stages, errors }),
    documents: { extracted: staticResult.documents.extracted, valid: staticResult.documents.valid },
    added: list(staticResult.added),
    removed: list(staticResult.removed),
    retyped: list(staticResult.retyped),
    steps: stages.flatMap((s) =>
      s.steps.map((st) => ({ stage: s.name, name: st.name, ok: st.ok, message: scrub(st.message ?? "") })),
    ),
    errors: [...list(staticResult.errors), ...list(stageErrors), ...list(errors)],
  };
}

// "<operation>: <rest>" -> [operation, rest]
function splitEntry(entry) {
  const i = entry.indexOf(": ");
  return i < 0 ? ["?", entry] : [entry.slice(0, i), entry.slice(i + 2)];
}

const isVar = (rest) => rest.startsWith("$");
const isArg = (rest) => /\([^)]*\)$/.test(rest);
const cell = (text) => String(text).replace(/\|/g, "\\|").replace(/\s*\n\s*/g, " ");

// Per-operation change lists: +added, -removed, ~retyped.
function changeRows(report) {
  const rows = new Map();
  const add = (entries, sign) => {
    for (const entry of entries) {
      const [op, rest] = splitEntry(entry);
      if (!rows.has(op)) rows.set(op, []);
      rows.get(op).push(`${sign}${rest}`);
    }
  };
  add(report.added, "+");
  add(report.removed, "-");
  add(report.retyped, "~");
  return rows;
}

// Console text, summary markdown and JSON text for a report.
export function render(report) {
  const passed = report.steps.filter((s) => s.ok).length;
  const rows = changeRows(report);

  const added = report.added.map((e) => splitEntry(e)[1]);
  const counts = {
    fields: added.filter((r) => !isVar(r) && !isArg(r)).length,
    args: added.filter((r) => !isVar(r) && isArg(r)).length,
    vars: added.filter(isVar).length,
  };
  // Removed fields, arguments and variables are all counted as "-fields".
  const diffLine =
    report.baseline === null || report.documents.extracted === 0
      ? `semantic diff: not run${report.baseline === null ? " (no baseline)" : ""}`
      : `semantic diff: ${rows.size} documents changed, +field paths ${counts.fields}, +args ${counts.args}, ` +
        `+vars ${counts.vars}, -fields ${report.removed.length}, retyped ${report.retyped.length}`;
  const head = [
    report.baseline === null
      ? `@linear/sdk ${report.candidate} (pinned)`
      : `@linear/sdk candidate ${report.candidate} vs baseline ${report.baseline}`,
    `documents extracted: ${report.documents.extracted}/53   static validation: ${report.documents.valid}/53 valid   ` +
      `steps: ${passed}/${report.steps.length} passed`,
    diffLine,
    `RESULT: ${report.result === "DRIFT" ? "DRIFT (additive)" : report.result}`,
  ];

  const table = rows.size === 0 ? [] : ["| operation | change |", "| --- | --- |"];
  for (const [op, changes] of rows) table.push(`| ${cell(op)} | ${cell(changes.join(" "))} |`);

  const failed = report.steps.filter((s) => !s.ok);
  const failLines = failed.length === 0 ? [] : ["failed steps:", ...failed.map((s) => `- ${s.stage}/${s.name}: ${s.message}`)];
  const shown = report.errors.slice(0, ERRORS_SHOWN);
  const errLines =
    report.errors.length === 0
      ? []
      : [
          "errors:",
          ...shown.map((e) => `- ${e}`),
          ...(report.errors.length > shown.length ? [`- ... ${report.errors.length - shown.length} more (see compat-report.json)`] : []),
        ];

  const stepLines = report.steps.map((s) => (s.ok ? `PASS ${s.stage}/${s.name}` : `FAIL ${s.stage}/${s.name}: ${s.message}`));
  const tail = [...head, ...errLines];
  return {
    console: scrub([...stepLines, "", ...tail].join("\n")),
    // The console text without the per-step lines (they were printed live).
    tail: scrub(tail.join("\n")),
    markdown: scrub([...head, ...table, ...failLines, ...errLines].join("\n")) + "\n",
    json: scrub(JSON.stringify(report, null, 2)) + "\n",
  };
}

// Writes compat-report.json and compat-summary.md into dir, and appends the
// summary to $GITHUB_STEP_SUMMARY when it is set.
export function writeReport(dir, report) {
  const out = render(report);
  fs.mkdirSync(dir, { recursive: true });
  const jsonPath = path.join(dir, "compat-report.json");
  const markdownPath = path.join(dir, "compat-summary.md");
  fs.writeFileSync(jsonPath, out.json);
  fs.writeFileSync(markdownPath, out.markdown);
  const stepSummary = process.env.GITHUB_STEP_SUMMARY;
  if (stepSummary) fs.appendFileSync(stepSummary, out.markdown + "\n");
  return { jsonPath, markdownPath };
}

// Removes the files of an earlier run, so a run that fails before it can write
// its own report leaves none behind.
export function clearReport(dir) {
  for (const file of ["compat-report.json", "compat-summary.md"]) {
    fs.rmSync(path.join(dir, file), { force: true });
  }
}
