import { spawn, spawnSync } from "node:child_process";
import { randomBytes } from "node:crypto";
import { mkdtempSync, rmSync } from "node:fs";
import { createServer } from "node:net";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { createInterface } from "node:readline";
import { mask, scrub } from "./mask.mjs";

const LOG_LINES = 200;
const READY_TIMEOUT_MS = 30_000;
const STOP_TIMEOUT_MS = 20_000;
const PSQL_TIMEOUT_MS = 60_000;

// Returns the one-time setup code from a `setup required` log line, or null.
export function parseSetupCode(line) {
  let entry;
  try {
    entry = JSON.parse(line);
  } catch {
    return null;
  }
  if (!entry || entry.msg !== "setup required" || typeof entry.url !== "string") return null;
  try {
    return new URL(entry.url).searchParams.get("code") || null;
  } catch {
    return null;
  }
}

export function databaseName() {
  return `yalkos_compat_${randomBytes(4).toString("hex")}`;
}

export function assertPsql() {
  const r = spawnSync("psql", ["--version"], { encoding: "utf8", timeout: 10_000, killSignal: "SIGKILL" });
  if (r.error || r.status !== 0) {
    throw new Error("psql is required on PATH (install the PostgreSQL client)");
  }
}

// Registers the password of a database URL as a secret, so that a psql error
// that echoes the URL is scrubbed. Passwords shorter than 8 characters are not
// registered: the local and CI service password is the word `yalkos`, and
// registering it would blank that word in all output. A short REAL secret
// therefore stays unmasked in a psql error; use a longer one.
export function maskUrlPassword(url) {
  let u;
  try {
    u = new URL(url);
  } catch {
    return;
  }
  const passwords = [u.password];
  try {
    passwords.push(decodeURIComponent(u.password));
  } catch {
    // malformed escape: the raw form alone is registered
  }
  for (const pw of passwords) {
    if (pw.length >= 8) mask(pw);
  }
}

// Splits the password off a database URL: psql gets it in PGPASSWORD, so it
// never shows in the process list. libpq takes a password from the userinfo
// part or from a password query parameter, the latter winning when both are
// given; both are removed from the URL. A URL that does not parse is passed as is.
export function psqlTarget(url) {
  let u;
  try {
    u = new URL(url);
  } catch {
    return { url, password: null };
  }
  let password = null;
  if (u.password) {
    password = u.password;
    try {
      password = decodeURIComponent(u.password);
    } catch {
      // malformed escape: the raw form is the best guess
    }
    u.password = "";
  }
  // The query is split by hand: URLSearchParams decodes "+" as a space and
  // re-encodes the pairs it keeps, while libpq reads "+" literally.
  const decode = (text) => {
    try {
      return decodeURIComponent(text);
    } catch {
      return text;
    }
  };
  const kept = [];
  for (const pair of u.search.slice(1).split("&")) {
    if (pair === "") continue;
    const eq = pair.indexOf("=");
    const name = eq < 0 ? pair : pair.slice(0, eq);
    if (decode(name) === "password") password = decode(eq < 0 ? "" : pair.slice(eq + 1));
    else kept.push(pair);
  }
  u.search = kept.length > 0 ? `?${kept.join("&")}` : "";
  // The same length rule as maskUrlPassword: the short local password stays readable.
  if (password !== null && password.length >= 8) mask(password);
  return { url: u.toString(), password };
}

function psql(url, sql, timeoutMs) {
  maskUrlPassword(url);
  const target = psqlTarget(url);
  const env = { ...process.env };
  if (target.password !== null) env.PGPASSWORD = target.password;
  const r = spawnSync("psql", [target.url, "-v", "ON_ERROR_STOP=1", "-X", "-q", "-c", sql], {
    encoding: "utf8",
    timeout: timeoutMs,
    killSignal: "SIGKILL",
    env,
  });
  if (r.error?.code === "ETIMEDOUT") {
    // Name the statement, never the URL.
    throw new Error(`psql timed out after ${timeoutMs / 1000} s: ${sql.split(/\s+/).slice(0, 2).join(" ")}`);
  }
  if (r.error) throw new Error(scrub(`psql failed to start: ${r.error.message}`));
  if (r.status !== 0) throw new Error(scrub(`psql failed: ${r.stderr.trim()}`));
}

// Creates an empty database next to the one adminUrl points at.
export function createDatabase(adminUrl, { timeoutMs = PSQL_TIMEOUT_MS } = {}) {
  assertPsql();
  const name = databaseName();
  psql(adminUrl, `CREATE DATABASE ${name}`, timeoutMs);
  const u = new URL(adminUrl);
  u.pathname = `/${name}`;
  return { name, url: u.toString() };
}

export function dropDatabase(adminUrl, name, { timeoutMs = PSQL_TIMEOUT_MS } = {}) {
  if (!/^yalkos_compat_[0-9a-f]{8}$/.test(name)) throw new Error(`refusing to drop database ${name}`);
  psql(adminUrl, `DROP DATABASE IF EXISTS ${name} WITH (FORCE)`, timeoutMs);
}

function freePort() {
  return new Promise((resolve, reject) => {
    const srv = createServer();
    srv.once("error", reject);
    srv.listen(0, "127.0.0.1", () => {
      const { port } = srv.address();
      srv.close(() => resolve(port));
    });
  });
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// Starts `bin serve` against dbUrl and waits for /readyz. env adds to (and may
// override) the defaults. The setup code is read from the child's log.
export async function startInstance({ bin, dbUrl, env = {}, port }) {
  port = port ?? (await freePort());
  const baseUrl = `http://127.0.0.1:${port}`;
  const dataDir = env.DATA_DIR ? null : mkdtempSync(join(tmpdir(), "yalkos-compat-data-"));
  const baseEnv = {
    PATH: process.env.PATH,
    HOME: process.env.HOME,
    DATABASE_URL: dbUrl,
    LISTEN_ADDR: `127.0.0.1:${port}`,
    BASE_URL: baseUrl,
    DATA_DIR: dataDir ?? env.DATA_DIR,
    LOG_LEVEL: "info",
    ...env,
  };

  const lines = [];
  let setupCode = null;
  let child = null;
  let exited = null; // resolves when the current child is gone
  let spawnError = null;

  const push = (line) => {
    const code = parseSetupCode(line);
    if (code) {
      setupCode = mask(code);
    }
    lines.push(scrub(line));
    if (lines.length > LOG_LINES) lines.shift();
  };

  const spawnChild = (overrides) => {
    const proc = spawn(bin, ["serve"], {
      env: { ...baseEnv, ...overrides },
      stdio: ["ignore", "pipe", "pipe"],
    });
    for (const stream of [proc.stdout, proc.stderr]) {
      createInterface({ input: stream }).on("line", push);
    }
    spawnError = null;
    // Resolves when the child is gone and its output is fully read (close), or
    // when it could not be started at all (error).
    exited = new Promise((resolve) => {
      proc.once("close", (c, sig) => resolve({ code: c, signal: sig }));
      proc.once("error", (err) => {
        spawnError = err;
        resolve({ code: null, signal: null });
      });
    });
    child = proc;
  };

  const logs = () => lines.map(scrub);

  const stopChild = async () => {
    const proc = child;
    if (!proc || spawnError || proc.exitCode !== null || proc.signalCode !== null) return;
    proc.kill("SIGTERM");
    let timer;
    const timeout = new Promise((resolve) => {
      timer = setTimeout(() => resolve("timeout"), STOP_TIMEOUT_MS);
    });
    try {
      const result = await Promise.race([exited, timeout]);
      if (result === "timeout") {
        proc.kill("SIGKILL");
        await exited;
      }
    } finally {
      clearTimeout(timer);
    }
  };

  const waitReady = async () => {
    const gone = exited;
    const deadline = Date.now() + READY_TIMEOUT_MS;
    while (Date.now() < deadline) {
      const state = await Promise.race([gone, sleep(0).then(() => null)]);
      if (spawnError) {
        throw new Error(`cannot start ${bin}: ${spawnError.code ?? spawnError.message}`);
      }
      if (state) {
        throw new Error(`server exited before it was ready (code ${state.code}, signal ${state.signal})\n${logs().join("\n")}`);
      }
      try {
        const res = await fetch(`${baseUrl}/readyz`, { signal: AbortSignal.timeout(2000) });
        if (res.status === 200) return;
      } catch {
        // not listening yet
      }
      await sleep(100);
    }
    throw new Error(`server not ready after ${READY_TIMEOUT_MS / 1000} s\n${logs().join("\n")}`);
  };

  const cleanData = () => {
    if (dataDir) rmSync(dataDir, { recursive: true, force: true });
  };

  spawnChild({});
  try {
    await waitReady();
  } catch (err) {
    await stopChild();
    cleanData();
    throw err;
  }

  return {
    baseUrl,
    port,
    logs,
    // The one-time code of the first start, or null when setup was already done.
    setupCode: () => setupCode,
    // Same port and database; envOverrides apply to this start only.
    async restart(envOverrides = {}) {
      await stopChild();
      spawnChild(envOverrides);
      try {
        await waitReady();
      } catch (err) {
        await stopChild();
        throw err;
      }
    },
    async stop() {
      await stopChild();
      cleanData();
    },
  };
}
