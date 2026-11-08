import { test } from "node:test";
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { chmodSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { mask, scrub, resetMasks } from "./mask.mjs";
import { parseSetupCode, databaseName, assertPsql, startInstance, createDatabase, maskUrlPassword } from "./server.mjs";
import { bootstrap } from "./bootstrap.mjs";

// The exact shape slog's JSON handler writes (see the Go test for the setup log).
const REAL_LINE =
  '{"time":"2026-11-08T10:00:00.123456+01:00","level":"WARN","msg":"setup required","url":"http://127.0.0.1:18080/setup?code=7EV2RUJH"}';

// Run fn with console.log captured.
async function capture(fn) {
  const orig = console.log;
  const lines = [];
  console.log = (...args) => lines.push(args.join(" "));
  try {
    await fn();
  } finally {
    console.log = orig;
  }
  return lines;
}

test("parseSetupCode reads the code from the real log line", () => {
  assert.equal(parseSetupCode(REAL_LINE), "7EV2RUJH");
  // The host comes from BASE_URL: only the query parameter counts.
  const other = REAL_LINE.replace("http://127.0.0.1:18080", "https://example.org");
  assert.equal(parseSetupCode(other), "7EV2RUJH");
});

test("parseSetupCode ignores other lines and invalid JSON", () => {
  assert.equal(parseSetupCode('{"level":"INFO","msg":"listening","addr":"127.0.0.1:1"}'), null);
  assert.equal(parseSetupCode("not json at all"), null);
  assert.equal(parseSetupCode(""), null);
  assert.equal(parseSetupCode('{"msg":"setup required"}'), null);
  assert.equal(parseSetupCode('{"msg":"setup required","url":"::not a url::"}'), null);
  assert.equal(parseSetupCode('{"msg":"setup required","url":"http://127.0.0.1:1/setup"}'), null);
});

test("TestMaskBeforeUse: mask prints ::add-mask:: once and scrub hides the value", async () => {
  resetMasks();
  const prev = process.env.GITHUB_ACTIONS;
  process.env.GITHUB_ACTIONS = "true";
  try {
    const out = await capture(() => {
      mask("s3cret-value-1");
      mask("s3cret-value-1");
    });
    assert.deepEqual(out, ["::add-mask::s3cret-value-1"]);
    assert.equal(scrub("token s3cret-value-1 end"), "token *** end");
    process.env.GITHUB_ACTIONS = "false";
    resetMasks();
    const quiet = await capture(() => mask("local-secret-2"));
    assert.deepEqual(quiet, []);
    assert.equal(scrub("x local-secret-2 y"), "x *** y");
  } finally {
    if (prev === undefined) delete process.env.GITHUB_ACTIONS;
    else process.env.GITHUB_ACTIONS = prev;
    resetMasks();
  }
});

test("scrub removes a registered secret from a server log line", () => {
  resetMasks();
  mask("7EV2RUJH");
  const cleaned = scrub(REAL_LINE);
  assert.ok(!cleaned.includes("7EV2RUJH"));
  assert.ok(cleaned.includes("***"));
  resetMasks();
});

test("mask ignores empty and non-string values", () => {
  resetMasks();
  mask("");
  mask(undefined);
  assert.equal(scrub("plain text"), "plain text");
});

test("assertPsql names the missing binary", () => {
  const prev = process.env.PATH;
  process.env.PATH = "";
  try {
    assert.throws(() => assertPsql(), /psql is required on PATH \(install the PostgreSQL client\)/);
  } finally {
    process.env.PATH = prev;
  }
});

test("databaseName has the yalkos_compat_ prefix and 8 hex digits", () => {
  const a = databaseName();
  assert.match(a, /^yalkos_compat_[0-9a-f]{8}$/);
  assert.notEqual(a, databaseName());
});

test("bootstrap errors name the mutation and never carry secrets", async () => {
  resetMasks();
  const origFetch = globalThis.fetch;
  const code = "ABCD2345";
  const password = "super-secret-password";
  // A server that echoes the submitted variables back inside the error message.
  globalThis.fetch = async (_url, init) => {
    const body = JSON.parse(init.body);
    return new Response(
      JSON.stringify({
        errors: [{ message: `bad input ${JSON.stringify(body.variables)}`, extensions: { code: "BAD_USER_INPUT" } }],
      }),
      { status: 200, headers: { "content-type": "application/json" } },
    );
  };
  try {
    await assert.rejects(
      () => bootstrap({ baseUrl: "http://127.0.0.1:1", code, password }),
      (err) => {
        assert.match(err.message, /^setup:/);
        assert.match(err.message, /BAD_USER_INPUT/);
        assert.ok(!err.message.includes(code), "setup code leaked");
        assert.ok(!err.message.includes(password), "password leaked");
        return true;
      },
    );
  } finally {
    globalThis.fetch = origFetch;
    resetMasks();
  }
});

test("startInstance rejects at once for a binary that does not exist", async () => {
  const bin = join(tmpdir(), "yalkos-compat-no-such-binary");
  const started = Date.now();
  await assert.rejects(
    () => startInstance({ bin, dbUrl: "postgres://unused" }),
    (err) => {
      assert.ok(err.message.includes(bin), `message lacks the path: ${err.message}`);
      return true;
    },
  );
  assert.ok(Date.now() - started < 5000, "took as long as the readiness timeout");
});

test("stop leaves no pending timer: the process exits promptly after stop()", async () => {
  const dir = mkdtempSync(join(tmpdir(), "yalkos-compat-fake-"));
  try {
    // A fake server: answers /readyz and exits on SIGTERM.
    const fake = join(dir, "fake-server.mjs");
    writeFileSync(
      fake,
      `#!/usr/bin/env node
import { createServer } from "node:http";
const port = Number(process.env.LISTEN_ADDR.split(":").pop());
const srv = createServer((req, res) => { res.statusCode = 200; res.end("{}"); });
srv.listen(port, "127.0.0.1");
process.on("SIGTERM", () => process.exit(0));
`,
    );
    chmodSync(fake, 0o755);
    const driver = join(dir, "driver.mjs");
    const serverModule = fileURLToPath(new URL("./server.mjs", import.meta.url));
    writeFileSync(
      driver,
      `import { startInstance } from ${JSON.stringify(serverModule)};
const inst = await startInstance({ bin: ${JSON.stringify(fake)}, dbUrl: "postgres://unused" });
await inst.stop();
console.log("stopped");
`,
    );
    const started = Date.now();
    const out = [];
    const child = spawn(process.execPath, [driver], { stdio: ["ignore", "pipe", "inherit"] });
    child.stdout.on("data", (d) => out.push(String(d)));
    const code = await new Promise((resolve) => child.once("close", resolve));
    assert.equal(code, 0);
    assert.match(out.join(""), /stopped/);
    assert.ok(Date.now() - started < 5000, "process lingered after stop()");
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

// Runs fn with a fake `psql` first on PATH. The script answers --version and
// otherwise runs `body` (shell).
async function withFakePsql(body, fn) {
  const dir = mkdtempSync(join(tmpdir(), "yalkos-compat-fakepsql-"));
  const prevPath = process.env.PATH;
  try {
    const script = join(dir, "psql");
    writeFileSync(script, `#!/bin/sh\nif [ "$1" = "--version" ]; then echo "psql (fake)"; exit 0; fi\n${body}\n`);
    chmodSync(script, 0o755);
    process.env.PATH = `${dir}:${prevPath}`;
    await fn();
  } finally {
    process.env.PATH = prevPath;
    rmSync(dir, { recursive: true, force: true });
    resetMasks();
  }
}

test("a psql error that echoes the URL never carries the password", async () => {
  resetMasks();
  const password = "Sup3rS3cretPw";
  const url = `postgres://admin:${password}@127.0.0.1:1/postgres`;
  await withFakePsql('echo "psql: could not connect using $1" >&2; exit 2', async () => {
    assert.throws(
      () => createDatabase(url),
      (err) => {
        assert.match(err.message, /^psql failed: /);
        assert.match(err.message, /could not connect/);
        assert.ok(!err.message.includes(password), `password leaked: ${err.message}`);
        return true;
      },
    );
  });
});

test("a hung psql is killed after the timeout and the error names the statement, not the URL", async () => {
  resetMasks();
  const password = "Sup3rS3cretPw";
  const url = `postgres://admin:${password}@127.0.0.1:1/postgres`;
  await withFakePsql("exec sleep 30", async () => {
    const started = Date.now();
    assert.throws(
      () => createDatabase(url, { timeoutMs: 300 }),
      (err) => {
        assert.match(err.message, /^psql timed out after 0\.3 s: CREATE DATABASE$/);
        assert.ok(!err.message.includes(password));
        return true;
      },
    );
    assert.ok(Date.now() - started < 5000, "the hung psql was not killed");
  });
});

test("maskUrlPassword survives a malformed escape and still masks the raw password", () => {
  resetMasks();
  assert.doesNotThrow(() => maskUrlPassword("postgres://u:abc%E0%A4%Axyz@h/db"));
  assert.equal(scrub("x abc%E0%A4%Axyz y"), "x *** y");
  assert.doesNotThrow(() => maskUrlPassword("not a url"));
  resetMasks();
});
