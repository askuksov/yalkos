// Static check of an SDK version: extract its documents, run the Go check on
// them, classify failures with graphql-js and compare with a baseline.
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { spawn } from "node:child_process";

import { extractDocuments, OPERATIONS } from "../static/extract-documents.mjs";
import { semanticDiff } from "../static/semantic-diff.mjs";
import { validateDocuments } from "../static/validate.mjs";

const EXPECTED = OPERATIONS.length;
const ADDITIVE = [/^Cannot query field /, /^Unknown argument /, /^Unknown type /];
const GO_PREFIX = "TestSDKDocuments/";

// "additive" when every message is a missing field, argument or type.
export function classifyMessages(messages) {
  if (messages.length === 0) return "other";
  return messages.every((m) => ADDITIVE.some((re) => re.test(m))) ? "additive" : "other";
}

// Reads the lines of `go test -json`; counts the per-document subtests.
export function parseGoTestJson(lines) {
  const passed = new Set();
  const failedTests = new Map();
  const output = new Map();
  let packageFailed = false;
  for (const line of lines) {
    let ev;
    try {
      ev = JSON.parse(line);
    } catch {
      continue;
    }
    const sub = typeof ev.Test === "string" && ev.Test.startsWith(GO_PREFIX);
    if (ev.Action === "output" && ev.Test) {
      output.set(ev.Test, (output.get(ev.Test) ?? "") + ev.Output);
    } else if (ev.Action === "pass" && sub) {
      passed.add(ev.Test);
    } else if (ev.Action === "fail" && sub) {
      failedTests.set(ev.Test, true);
    } else if (ev.Action === "fail" && !ev.Test) {
      packageFailed = true;
    }
  }
  const failures = [...failedTests.keys()].map((test) => ({ test, output: output.get(test) ?? "" }));
  // A failed package without a failed subtest (build error, panic) is still a failure.
  if (failures.length === 0 && packageFailed) failures.push({ test: "(package)", output: "" });
  return { passed: passed.size, failed: failures.length, failures };
}

// Default Go step: the authoritative check of the documents.
export function runGoTest({ documentsDir, repoRoot }) {
  return new Promise((resolve, reject) => {
    const args = ["test", "./internal/graphql/linear", "-run", "^TestSDKDocuments$", "-count=1", "-json"];
    const child = spawn("go", args, {
      cwd: repoRoot,
      env: { ...process.env, LINEAR_SDK_DOCUMENTS: documentsDir },
      stdio: ["ignore", "pipe", "pipe"],
    });
    let out = "";
    child.stdout.on("data", (d) => (out += d));
    child.stderr.on("data", (d) => (out += d));
    child.on("error", reject);
    child.on("close", (exitCode) => resolve({ lines: out.split("\n").filter(Boolean), exitCode }));
  });
}

const flatten = (changed, key, fmt = (x) => x) => changed.flatMap((c) => c[key].map((x) => `${c.op}: ${fmt(x)}`));

function summarizeDiff(diff) {
  return {
    added: [...flatten(diff.changed, "addedFields"), ...flatten(diff.changed, "addedVars")],
    removed: [...flatten(diff.changed, "removedFields"), ...flatten(diff.changed, "removedVars")],
    retyped: flatten(diff.changed, "retypedVars", (r) => `${r.variable}: ${r.from} -> ${r.to}`),
  };
}

const distFile = (nodeModules) => path.join(nodeModules, "@linear", "sdk", "dist", "index.mjs");

export async function runStatic({ nodeModules, repoRoot, baselineNodeModules, runGo = runGoTest }) {
  const result = {
    result: "TOOLING",
    documents: { extracted: 0, valid: 0 },
    diff: null,
    added: [],
    removed: [],
    retyped: [],
    go: { passed: 0, failed: 0 },
    errors: [],
  };
  const tmp = fs.mkdtempSync(path.join(os.tmpdir(), "compat-docs-"));
  try {
    const sdkDistFile = distFile(nodeModules);
    if (!fs.existsSync(sdkDistFile)) {
      result.errors.push(`SDK bundle not found: ${sdkDistFile}`);
      return result;
    }
    const documentsDir = path.join(tmp, "documents");
    const candidate = extractDocuments({ sdkDistFile, outDir: documentsDir });
    result.documents.extracted = candidate.extracted;
    if (candidate.extracted !== EXPECTED) {
      result.errors.push(
        `extracted ${candidate.extracted} of ${EXPECTED} documents; missing: ${candidate.missing.join(", ")}`,
      );
      return result;
    }

    let baseline = null;
    if (baselineNodeModules) {
      const baselineFile = distFile(baselineNodeModules);
      if (!fs.existsSync(baselineFile)) {
        result.errors.push(`baseline SDK bundle not found: ${baselineFile}`);
        return result;
      }
      baseline = extractDocuments({ sdkDistFile: baselineFile });
      if (baseline.extracted !== EXPECTED) {
        result.errors.push(
          `baseline: extracted ${baseline.extracted} of ${EXPECTED} documents; missing: ${baseline.missing.join(", ")}`,
        );
        return result;
      }
      result.diff = semanticDiff(baseline.all, candidate.all);
      Object.assign(result, summarizeDiff(result.diff));
    }

    let go;
    try {
      go = parseGoTestJson((await runGo({ documentsDir, repoRoot })).lines);
    } catch (err) {
      result.errors.push(`go test could not run: ${err.message}`);
      return result;
    }
    result.go = { passed: go.passed, failed: go.failed };
    if (go.failed === 0 && go.passed !== EXPECTED) {
      result.errors.push(`go test passed ${go.passed} documents, expected ${EXPECTED}`);
      return result;
    }

    const validated = validateDocuments({ schemaDir: path.join(repoRoot, "api", "graphql", "linear"), docsDir: documentsDir });
    const invalid = validated.filter((v) => v.messages.length > 0);
    result.documents.valid = validated.length - invalid.length;
    const messages = invalid.flatMap((v) => v.messages.map((m) => `${v.file}: ${m}`));
    const bare = invalid.flatMap((v) => v.messages);

    // Go and graphql-js must fail exactly the same documents.
    const goFailed = new Set(go.failures.map((f) => f.test.startsWith(GO_PREFIX) ? f.test.slice(GO_PREFIX.length) : f.test));
    const jsFailed = new Set(invalid.map((v) => v.file.replace(/\.graphql$/, "")));
    const disagree = [
      ...[...goFailed].filter((op) => !jsFailed.has(op)).map((op) => `${op}: go test failed it, graphql-js accepts it`),
      ...[...jsFailed].filter((op) => !goFailed.has(op)).map((op) => `${op}: graphql-js rejects it, go test passed it`),
    ];
    if (disagree.length > 0) {
      result.errors.push(...disagree, ...messages);
      result.result = "BREAK";
      return result;
    }
    if (invalid.length > 0) {
      result.errors.push(...messages);
      result.result = classifyMessages(bare) === "additive" ? "DRIFT" : "BREAK";
      return result;
    }

    // Check passed; a baseline diff that only removes things is informational.
    const onlyRemovals =
      result.diff !== null &&
      result.diff.changed.length > 0 &&
      result.added.length === 0 &&
      result.retyped.length === 0;
    result.result = onlyRemovals ? "INFO" : "OK";
    return result;
  } finally {
    fs.rmSync(tmp, { recursive: true, force: true });
  }
}
