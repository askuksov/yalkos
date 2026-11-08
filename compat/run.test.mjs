import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";

import { isValidSdkVersion, runChecks, runLatest } from "./run.mjs";
import { createRunner } from "./lib/check.mjs";
import { clearReport } from "./lib/report.mjs";

function tmpDir(t) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "compat-run-test-"));
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
  return dir;
}

async function quiet(fn) {
  const orig = console.log;
  const lines = [];
  console.log = (...args) => lines.push(args.join(" "));
  try {
    return { value: await fn(), lines };
  } finally {
    console.log = orig;
  }
}

const passingStage = {
  name: "fake",
  env: {},
  restart: false,
  async run() {
    const r = createRunner("fake");
    await r.step("one", async () => {});
    return r;
  },
};

// A server and a database that need no psql, binary or network.
function fakeDeps(extra = {}) {
  const dropped = [];
  return {
    dropped,
    deps: {
      createDatabase: () => ({ name: "yalkos_compat_00000000", url: "postgres://u:p@127.0.0.1:1/yalkos_compat_00000000" }),
      dropDatabase: (_url, name) => dropped.push(name),
      startInstance: async () => ({
        baseUrl: "http://127.0.0.1:1",
        logs: () => ["server log line"],
        setupCode: () => "CODE",
        stop: async () => {},
        restart: async () => {},
      }),
      bootstrap: async () => ({ apiKey: "k", ownerCookie: "c", owner: { id: "1" }, member: {}, projects: {}, milestone: {} }),
      ...extra,
    },
  };
}

const options = (reportDir) => ({ mode: "pinned", databaseUrl: "postgres://u:p@127.0.0.1:1/postgres", keep: false, reportDir, baselineModules: null });
const checked = { repoRoot: "/nonexistent", bin: "/nonexistent", selected: [passingStage] };

test("a throwing static check becomes a TOOLING report with the error text, exit 1, and no stale report survives", async (t) => {
  const reportDir = tmpDir(t);
  const reportFile = path.join(reportDir, "compat-report.json");
  fs.writeFileSync(reportFile, JSON.stringify({ result: "OK", stale: true }));
  clearReport(reportDir);
  assert.ok(!fs.existsSync(reportFile), "clearReport left the stale report");
  fs.writeFileSync(reportFile, JSON.stringify({ result: "OK", stale: true }));

  const { deps, dropped } = fakeDeps({
    runStatic: async () => {
      throw new Error("boom in the extractor");
    },
  });
  const { value: code } = await quiet(() => runChecks(options(reportDir), checked, deps));
  assert.equal(code, 1);
  const report = JSON.parse(fs.readFileSync(reportFile, "utf8"));
  assert.equal(report.stale, undefined, "the stale report was not replaced");
  assert.equal(report.result, "TOOLING");
  assert.ok(report.errors.some((e) => e.includes("static check failed: boom in the extractor")), JSON.stringify(report.errors));
  // The dynamic stage still ran and the database was dropped.
  assert.deepEqual(report.steps.map((s) => `${s.stage}/${s.name}`), ["fake/one"]);
  assert.deepEqual(dropped, ["yalkos_compat_00000000"]);
  assert.ok(fs.existsSync(path.join(reportDir, "compat-summary.md")));
});

test("a failure before any stage (database cannot be created) still writes a TOOLING report", async (t) => {
  const reportDir = tmpDir(t);
  const { deps } = fakeDeps({
    runStatic: async () => ({
      result: "OK", documents: { extracted: 53, valid: 53 }, diff: null, added: [], removed: [], retyped: [], errors: [],
    }),
    createDatabase: () => {
      throw new Error("psql failed: no database for you");
    },
  });
  const { value: code } = await quiet(() => runChecks(options(reportDir), checked, deps));
  assert.equal(code, 1);
  const report = JSON.parse(fs.readFileSync(path.join(reportDir, "compat-report.json"), "utf8"));
  assert.equal(report.result, "TOOLING");
  assert.deepEqual(report.errors, ["psql failed: no database for you"]);
});

test("a server that fails to start still drops the database and exits 1", async (t) => {
  const reportDir = tmpDir(t);
  const { deps, dropped } = fakeDeps({
    runStatic: async () => ({
      result: "OK", documents: { extracted: 53, valid: 53 }, diff: null, added: [], removed: [], retyped: [], errors: [],
    }),
    startInstance: async () => {
      throw new Error("server exited before it was ready (code 1, signal null)");
    },
  });
  const { value: code } = await quiet(() => runChecks(options(reportDir), checked, deps));
  assert.equal(code, 1);
  assert.deepEqual(dropped, ["yalkos_compat_00000000"]);
});

test("run.mjs executes when started through a symlinked directory", (t) => {
  // os.tmpdir() is a symlink on macOS; a fake psql keeps the test independent of the host.
  const dir = tmpDir(t);
  const fakeBin = path.join(dir, "bin");
  fs.mkdirSync(fakeBin);
  fs.writeFileSync(path.join(fakeBin, "psql"), "#!/bin/sh\necho 'psql (fake)'\n", { mode: 0o755 });
  const link = path.join(dir, "link");
  fs.symlinkSync(path.dirname(fileURLToPath(import.meta.url)), link, "dir");
  const r = spawnSync(process.execPath, [path.join(link, "run.mjs"), "--stages", "nothing"], {
    encoding: "utf8",
    env: {
      ...process.env,
      PATH: `${fakeBin}:${process.env.PATH}`,
      COMPAT_DATABASE_URL: "postgres://u:p@127.0.0.1:1/postgres",
      COMPAT_BIN: process.execPath,
      COMPAT_REPORT_DIR: path.join(dir, "report"),
    },
  });
  assert.equal(r.status, 1, `main did not run: exit ${r.status}, stdout ${r.stdout}, stderr ${r.stderr}`);
  assert.match(r.stderr, /no stage selected/);
});

// --- latest mode: the outer run verifies that the inner run reported ---

function latestFixture(t, innerBehaviour) {
  const reportDir = path.join(tmpDir(t), "report");
  const opts = { mode: "latest", sdkVersion: null, stages: null, reportDir, pnpm: ["fake-pnpm"] };
  const deps = {
    // The "add" step leaves an SDK package.json behind, as pnpm would.
    pnpm: (_o, args, cwd) => {
      if (args[0] !== "add") return;
      const dir = path.join(cwd, "node_modules", "@linear", "sdk");
      fs.mkdirSync(dir, { recursive: true });
      fs.writeFileSync(path.join(dir, "package.json"), JSON.stringify({ version: "999.1.0" }));
    },
    spawnInner: async () => innerBehaviour(reportDir),
  };
  return { opts, deps, reportDir };
}

const writeInnerReport = (reportDir, result) => {
  fs.mkdirSync(reportDir, { recursive: true });
  fs.writeFileSync(path.join(reportDir, "compat-report.json"), JSON.stringify({ candidate: "999.1.0", result }));
};

test("latest: an inner run that exits 0 without a report is a TOOLING failure", async (t) => {
  const { opts, deps, reportDir } = latestFixture(t, () => 0);
  const { value: code } = await quiet(() => runLatest(opts, { bin: "/nonexistent" }, deps));
  assert.equal(code, 1);
  const report = JSON.parse(fs.readFileSync(path.join(reportDir, "compat-report.json"), "utf8"));
  assert.equal(report.result, "TOOLING");
  assert.ok(report.errors.some((e) => e.includes("without writing a report")), JSON.stringify(report.errors));
});

test("latest: a failing inner run without a report is TOOLING; with a report the exit code follows the class", async (t) => {
  const failing = latestFixture(t, () => 7);
  assert.equal((await quiet(() => runLatest(failing.opts, { bin: "/x" }, failing.deps))).value, 1);

  for (const [result, expected] of [["OK", 0], ["INFO", 0], ["DRIFT", 0], ["BREAK", 1], ["TOOLING", 1]]) {
    const fx = latestFixture(t, (dir) => {
      writeInnerReport(dir, result);
      return 0;
    });
    const { value: code } = await quiet(() => runLatest(fx.opts, { bin: "/x" }, fx.deps));
    assert.equal(code, expected, `inner report ${result}`);
  }
  // A non-zero inner exit code is kept even when the report looks fine.
  const odd = latestFixture(t, (dir) => {
    writeInnerReport(dir, "OK");
    return 3;
  });
  assert.equal((await quiet(() => runLatest(odd.opts, { bin: "/x" }, odd.deps))).value, 3);
});

test("the SDK version must be latest or a plain version", () => {
  for (const ok of ["latest", "101.1.0", "102.0.0-beta.1", "0.0.1-rc.2"]) {
    assert.equal(isValidSdkVersion(ok), true, ok);
  }
  for (const bad of ["latest; rm -rf", "--ignore-scripts", "   ", "", "1.2", "v1.2.3", "1.2.3 ", "1.2.3;x", "^1.2.3", "latest ", "1.2.3-", "@1.2.3"]) {
    assert.equal(isValidSdkVersion(bad), false, JSON.stringify(bad));
  }
  assert.equal(isValidSdkVersion(undefined), false);
});

test("run.mjs rejects an invalid SDK version before anything starts", () => {
  for (const [argv, env] of [
    [["--mode", "latest", "--sdk-version", "--ignore-scripts"], {}],
    [["--mode", "latest"], { COMPAT_SDK_VERSION: "latest; rm -rf" }],
    [["--mode", "latest"], { COMPAT_SDK_VERSION: "  " }],
    [["--mode", "latest", "--sdk-version", "1.2"], {}],
  ]) {
    const r = spawnSync(process.execPath, [path.join(path.dirname(fileURLToPath(import.meta.url)), "run.mjs"), ...argv], {
      encoding: "utf8",
      env: { ...process.env, COMPAT_DATABASE_URL: "", COMPAT_SDK_VERSION: "", ...env },
    });
    assert.equal(r.status, 1, JSON.stringify([argv, env]));
    assert.match(r.stderr, /invalid SDK version/);
  }
});
