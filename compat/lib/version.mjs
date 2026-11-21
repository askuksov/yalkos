// Checks of an @linear/sdk version text. The version ends up in `pnpm add`
// arguments and in issue titles, so only plain texts are accepted.

const SEMVER = /^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$/;

// A plain version such as 101.1.0 or 102.0.0-beta.1.
export function isSemver(value) {
  return typeof value === "string" && SEMVER.test(value);
}

// What may be asked for: "latest" or a plain version.
export function isValidSdkVersion(value) {
  return value === "latest" || isSemver(value);
}

// Compares two plain versions: negative when a is older than b, 0 when equal,
// positive when newer. A pre-release is older than its release; pre-release
// identifiers compare numerically when both are numbers, else as text.
export function compareVersions(a, b) {
  const split = (v) => {
    const [core, pre] = v.split(/-(.*)/s);
    return { nums: core.split(".").map(Number), pre: pre === undefined ? null : pre.split(".") };
  };
  const x = split(a);
  const y = split(b);
  for (let i = 0; i < 3; i++) {
    if (x.nums[i] !== y.nums[i]) return x.nums[i] - y.nums[i];
  }
  if (x.pre === null || y.pre === null) return (x.pre === null ? 1 : 0) - (y.pre === null ? 1 : 0);
  for (let i = 0; i < Math.max(x.pre.length, y.pre.length); i++) {
    const [p, q] = [x.pre[i], y.pre[i]];
    if (p === undefined) return -1;
    if (q === undefined) return 1;
    const [pn, qn] = [/^\d+$/.test(p), /^\d+$/.test(q)];
    if (pn && qn && Number(p) !== Number(q)) return Number(p) - Number(q);
    if (pn !== qn) return pn ? -1 : 1;
    if (p !== q) return p < q ? -1 : 1;
  }
  return 0;
}
