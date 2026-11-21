// Secret registry: every secret is passed through mask() before its first use,
// and every text that may reach a log or an error goes through scrub().

const secrets = new Set();

// Escapes a value for a workflow command: the runner decodes %25, %0D and %0A,
// so a raw % (say, in a password) would otherwise hide a different value.
export function escapeCommandValue(value) {
  return value.replaceAll("%", "%25").replaceAll("\r", "%0D").replaceAll("\n", "%0A");
}

// The ::add-mask:: lines for value: one per line of it, since the runner
// matches a masked value within a single log line.
export function maskCommands(value) {
  return value
    .split(/\r?\n/)
    .filter((line) => line !== "")
    .map((line) => `::add-mask::${escapeCommandValue(line)}`);
}

// Registers a secret for scrubbing. In GitHub Actions it also asks the runner
// to hide the value in all later log output (once per value).
export function mask(value) {
  if (typeof value !== "string" || value === "" || secrets.has(value)) return value;
  secrets.add(value);
  if (process.env.GITHUB_ACTIONS === "true") for (const line of maskCommands(value)) console.log(line);
  return value;
}

// Replaces every registered secret in text with ***.
export function scrub(text) {
  let out = String(text);
  // Longest first, so a secret that contains another one is hidden as a whole.
  for (const s of [...secrets].sort((a, b) => b.length - a.length)) {
    out = out.split(s).join("***");
  }
  return out;
}

// For tests: forget all registered values.
export function resetMasks() {
  secrets.clear();
}
