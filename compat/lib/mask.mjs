// Secret registry: every secret is passed through mask() before its first use,
// and every text that may reach a log or an error goes through scrub().

const secrets = new Set();

// Registers a secret for scrubbing. In GitHub Actions it also asks the runner
// to hide the value in all later log output (once per value).
export function mask(value) {
  if (typeof value !== "string" || value === "" || secrets.has(value)) return value;
  secrets.add(value);
  if (process.env.GITHUB_ACTIONS === "true") console.log(`::add-mask::${value}`);
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
