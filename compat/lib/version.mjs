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
