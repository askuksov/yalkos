#!/usr/bin/env node
// Runs the compatibility checks: the static check of the SDK documents, then
// the scenario stages against a real instance. See the flags below.
//
//   node run.mjs [--mode pinned|latest] [--stages a,b] [--sdk-version <v|latest>]
//
// Environment: COMPAT_DATABASE_URL (required), COMPAT_BIN, COMPAT_KEEP,
// COMPAT_REPORT_DIR, COMPAT_SDK_VERSION, COMPAT_PNPM (latest mode, default pnpm).
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { spawn, spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";

import { STAGES } from "./scenarios/index.mjs";
import { runStatic } from "./lib/static.mjs";
import { assertPsql, createDatabase, dropDatabase, startInstance } from "./lib/server.mjs";
import { bootstrap } from "./lib/bootstrap.mjs";
import { scrub } from "./lib/mask.mjs";
import { buildReport, clearReport, exitCode, render, writeReport } from "./lib/report.mjs";

const compatDir = path.dirname(fileURLToPath(import.meta.url));

const message = (err) => scrub(err instanceof Error ? err.message : String(err));

function fail(text) {
  console.error(scrub(text));
  process.exit(1);
}

// --- options ---

function parseArgs(argv, env) {
  const opts = {
    mode: "pinned",
    stages: null,
    sdkVersion: env.COMPAT_SDK_VERSION || null,
    bin: env.COMPAT_BIN || null,
    databaseUrl: env.COMPAT_DATABASE_URL || "",
    keep: env.COMPAT_KEEP === "1" || env.COMPAT_KEEP === "true",
    reportDir: env.COMPAT_REPORT_DIR || path.join(os.tmpdir(), "yalkos-compat-report"),
    pnpm: (env.COMPAT_PNPM || "pnpm").trim().split(/\s+/),
    baselineModules: env.COMPAT_BASELINE_MODULES || null,
  };
  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i];
    const value = () => {
      if (i + 1 >= argv.length) fail(`${arg} needs a value`);
      return argv[++i];
    };
    if (arg === "--mode") opts.mode = value();
    else if (arg === "--stages") opts.stages = value().split(",").map((s) => s.trim()).filter(Boolean);
    else if (arg === "--sdk-version") opts.sdkVersion = value();
    else fail(`unknown argument: ${arg}`);
  }
  if (!["pinned", "latest", "latest-inner"].includes(opts.mode)) {
    fail(`unknown mode: ${opts.mode} (use pinned or latest)`);
  }
  if (opts.mode === "pinned" && opts.sdkVersion) fail("--sdk-version is only used with --mode latest");
  return opts;
}

// Fails (before anything is created) when the run cannot start.
function preflight(opts) {
  try {
    assertPsql();
  } catch (err) {
    fail(err.message);
  }
  if (!opts.databaseUrl) fail("COMPAT_DATABASE_URL is required (a Postgres URL whose user may create databases)");
  const repoRoot = path.resolve(opts.baselineModules ? path.resolve(opts.baselineModules, "..", "..") : path.join(compatDir, ".."));
  const bin = path.resolve(opts.bin ?? path.join(repoRoot, "bin", "yalkos-host"));
  try {
    fs.accessSync(bin, fs.constants.X_OK);
  } catch {
    fail(`binary not found or not executable: ${bin} (build it first, or set COMPAT_BIN)`);
  }
  const names = opts.stages ?? STAGES.map((s) => s.name);
  const unknown = names.filter((n) => !STAGES.some((s) => s.name === n));
  const selected = STAGES.filter((s) => names.includes(s.name));
  if (selected.length === 0 || unknown.length > 0) {
    const what = selected.length === 0 ? "no stage selected: " : "";
    fail(`${what}unknown stage ${unknown.map((n) => JSON.stringify(n)).join(", ")} (available: ${STAGES.map((s) => s.name).join(", ")})`);
  }
  return { repoRoot, bin, selected };
}

// --- helpers ---

function sdkVersionOf(nodeModules) {
  try {
    return JSON.parse(fs.readFileSync(path.join(nodeModules, "@linear", "sdk", "package.json"), "utf8")).version ?? null;
  } catch {
    return null;
  }
}

const emptyStatic = (errors) => ({
  result: "TOOLING",
  documents: { extracted: 0, valid: 0 },
  diff: null,
  added: [],
  removed: [],
  retyped: [],
  errors,
});

// Writes the report, prints its tail and the report path; returns the exit code.
function finish({ opts, mode, report, instance }) {
  const out = render(report);
  console.log("");
  console.log(out.tail);
  const { jsonPath } = writeReport(opts.reportDir, report);
  console.log(`report: ${jsonPath}`);
  const code = exitCode(report.result, mode);
  if (instance && (code !== 0 || report.steps.some((s) => !s.ok))) {
    console.log("");
    console.log("--- last 200 server log lines (scrubbed) ---");
    console.log(instance.logs().map(scrub).join("\n"));
  }
  return code;
}

// --- one run (pinned, or the inner part of latest) ---

// deps are replaceable for tests; the defaults are the real implementations.
const REAL_DEPS = { runStatic, createDatabase, dropDatabase, startInstance, bootstrap };

export async function runChecks(opts, { repoRoot, bin, selected }, deps = {}) {
  const d = { ...REAL_DEPS, ...deps };
  const mode = opts.mode === "pinned" ? "pinned" : "latest";
  const nodeModules = path.join(compatDir, "node_modules");
  const baselineModules = opts.baselineModules ? path.resolve(opts.baselineModules) : undefined;
  if (mode === "latest" && !baselineModules) fail("COMPAT_BASELINE_MODULES is required in the latest mode");
  const candidate = sdkVersionOf(nodeModules);
  const baseline = baselineModules ? sdkVersionOf(baselineModules) : null;
  console.log(`@linear/sdk ${candidate ?? "(unknown)"}${baseline ? ` (baseline ${baseline})` : ""}, mode ${mode}`);

  console.log("== static");
  let staticResult;
  try {
    staticResult = await d.runStatic({ nodeModules, repoRoot, baselineNodeModules: baselineModules });
  } catch (err) {
    // An extractor or validator that throws is a broken check, not a pass.
    staticResult = emptyStatic([`static check failed: ${message(err)}`]);
  }
  console.log(
    `static: ${staticResult.result}, documents ${staticResult.documents.valid}/${staticResult.documents.extracted} valid`,
  );

  const stages = [];
  const errors = [];
  let instance = null;
  let database = null;
  let cleaning = null;
  const cleanup = () => {
    cleaning ??= (async () => {
      if (instance) {
        try {
          await instance.stop();
        } catch (err) {
          errors.push(`stop: ${message(err)}`);
        }
      }
      if (database && !opts.keep) {
        try {
          d.dropDatabase(opts.databaseUrl, database.name);
        } catch (err) {
          errors.push(`drop database: ${message(err)}`);
        }
      }
    })();
    return cleaning;
  };
  const onSignal = (signal) => {
    cleanup().finally(() => process.exit(signal === "SIGINT" ? 130 : 143));
  };
  process.once("SIGINT", onSignal);
  process.once("SIGTERM", onSignal);

  try {
    database = d.createDatabase(opts.databaseUrl);
    console.log(`database ${database.name}${opts.keep ? " (kept at the end)" : ""}`);
    instance = await d.startInstance({ bin, dbUrl: database.url });
    const boot = await d.bootstrap({ baseUrl: instance.baseUrl, code: instance.setupCode() });
    const ctx = { baseUrl: instance.baseUrl, ...boot, instance, sdkVersion: candidate };

    let serverGone = false;
    for (const stage of selected) {
      console.log(`== stage ${stage.name}`);
      if (serverGone) {
        stages.push({ name: stage.name, steps: [], error: "skipped: the server is not running" });
        continue;
      }
      if (stage.restart) {
        try {
          await instance.restart(stage.env);
        } catch (err) {
          serverGone = true;
          stages.push({ name: stage.name, steps: [], error: `restart failed: ${message(err)}` });
          continue;
        }
      }
      stages.push(await runStage(stage, ctx));
    }
  } catch (err) {
    errors.push(message(err));
  } finally {
    await cleanup();
    process.off("SIGINT", onSignal);
    process.off("SIGTERM", onSignal);
  }

  const report = buildReport({ candidate, baseline, staticResult, stages, errors });
  return finish({ opts, mode, report, instance });
}

// Runs one stage. A stage that throws or ran 0 steps is recorded with its error
// and no steps, which makes the run TOOLING.
async function runStage(stage, ctx) {
  try {
    const runner = await stage.run(ctx);
    const summary = runner.summary();
    return { name: stage.name, steps: summary.steps, error: null };
  } catch (err) {
    return { name: stage.name, steps: [], error: message(err) };
  }
}

// --- latest: a throwaway copy with the newest SDK, then the inner run ---

function pnpm(opts, args, cwd) {
  const [cmd, ...pre] = opts.pnpm;
  const r = spawnSync(cmd, [...pre, ...args], { cwd, stdio: "inherit", env: process.env });
  if (r.error) throw new Error(`cannot run ${opts.pnpm.join(" ")}: ${r.error.message}`);
  if (r.status !== 0) throw new Error(`${opts.pnpm.join(" ")} ${args.join(" ")} exited with ${r.status}`);
}

// Starts the inner run and resolves with its exit code.
function spawnInner(opts, checked, copy) {
  const args = [path.join(copy, "run.mjs"), "--mode", "latest-inner"];
  if (opts.stages) args.push("--stages", opts.stages.join(","));
  const child = spawn(process.execPath, args, {
    stdio: "inherit",
    env: {
      ...process.env,
      COMPAT_BASELINE_MODULES: path.join(compatDir, "node_modules"),
      COMPAT_BIN: checked.bin,
      COMPAT_REPORT_DIR: opts.reportDir,
    },
  });
  return new Promise((resolve) => {
    child.once("error", (err) => {
      console.error(scrub(`cannot start the inner run: ${err.message}`));
      resolve(1);
    });
    child.once("close", (code, signal) => resolve(code ?? (signal ? 1 : 0)));
  });
}

// A run that executed nothing must never be green: after the inner process
// exits, its report has to exist and name the candidate version. Returns the
// exit code of the outer run.
export function verifyInner(opts, innerCode) {
  const reportFile = path.join(opts.reportDir, "compat-report.json");
  let reported = null;
  try {
    reported = JSON.parse(fs.readFileSync(reportFile, "utf8"));
  } catch {
    // missing or unreadable: handled below
  }
  if (!reported || typeof reported.candidate !== "string" || typeof reported.result !== "string") {
    const report = buildReport({
      candidate: null,
      baseline: sdkVersionOf(path.join(compatDir, "node_modules")),
      staticResult: emptyStatic([`the inner run exited with ${innerCode} without writing a report (${reportFile})`]),
      stages: [],
    });
    return finish({ opts, mode: "latest", report, instance: null });
  }
  return innerCode !== 0 ? innerCode : exitCode(reported.result, "latest");
}

export async function runLatest(opts, checked, deps = {}) {
  const d = { pnpm, spawnInner, ...deps };
  const version = opts.sdkVersion || "latest";
  const tmp = fs.mkdtempSync(path.join(os.tmpdir(), "yalkos-compat-latest-"));
  const copy = path.join(tmp, "compat");
  const remove = () => fs.rmSync(tmp, { recursive: true, force: true });
  const onSignal = (signal) => {
    remove();
    process.exit(signal === "SIGINT" ? 130 : 143);
  };
  process.once("SIGINT", onSignal);
  process.once("SIGTERM", onSignal);
  try {
    fs.cpSync(compatDir, copy, { recursive: true, filter: (src) => path.basename(src) !== "node_modules" });
    let resolved = null;
    try {
      console.log(`installing the compatibility check dependencies in ${copy}`);
      d.pnpm(opts, ["install", "--frozen-lockfile"], copy);
      console.log(`adding @linear/sdk@${version}`);
      d.pnpm(opts, ["add", `@linear/sdk@${version}`, "--lockfile=false"], copy);
      resolved = sdkVersionOf(path.join(copy, "node_modules"));
      if (!resolved) throw new Error(`@linear/sdk@${version} is not installed after pnpm add`);
    } catch (err) {
      const report = buildReport({
        candidate: null,
        baseline: sdkVersionOf(path.join(compatDir, "node_modules")),
        staticResult: emptyStatic([`cannot install @linear/sdk@${version}: ${message(err)}`]),
        stages: [],
      });
      return finish({ opts, mode: "latest", report, instance: null });
    }
    console.log(`resolved @linear/sdk ${resolved}`);

    const innerCode = await d.spawnInner(opts, checked, copy);
    return verifyInner(opts, innerCode);
  } finally {
    process.off("SIGINT", onSignal);
    process.off("SIGTERM", onSignal);
    remove();
  }
}

// --- main ---

async function main() {
  const opts = parseArgs(process.argv.slice(2), process.env);
  // A report of an earlier run must never be mistaken for this run's result.
  clearReport(opts.reportDir);
  const checked = preflight(opts);
  const code = opts.mode === "latest" ? await runLatest(opts, checked) : await runChecks(opts, checked);
  // Let buffered output drain; the unref'd timer only forces the exit if a handle lingers.
  process.exitCode = code;
  setTimeout(() => process.exit(code), 3000).unref();
}

// True when this file is the entry point. argv[1] is not resolved through
// symlinks (a temp directory on macOS is one), import.meta.url is.
function isEntryPoint() {
  if (!process.argv[1]) return false;
  try {
    return fs.realpathSync(process.argv[1]) === fs.realpathSync(fileURLToPath(import.meta.url));
  } catch {
    return false;
  }
}

if (isEntryPoint()) {
  main().catch((err) => fail(message(err)));
}
