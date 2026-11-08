// Minimal step runner for compatibility checks. No dependencies.

export class StepFailure extends Error {
  constructor(message) {
    super(message);
    this.name = "StepFailure";
  }
}

function describe(value) {
  if (value instanceof Error) return value.message;
  if (typeof value === "string") return value;
  try {
    return JSON.stringify(value) ?? String(value);
  } catch {
    return String(value);
  }
}

export function createRunner(stage) {
  const steps = [];
  return {
    // Runs one step; a throw is recorded as a failure and later steps still run.
    async step(name, fn) {
      try {
        await fn();
        steps.push({ name, ok: true, message: "" });
        console.log(`PASS ${stage}/${name}`);
      } catch (err) {
        const message = describe(err);
        steps.push({ name, ok: false, message });
        console.log(`FAIL ${stage}/${name}: ${message}`);
      }
    },
    results() {
      return steps.map((s) => ({ ...s }));
    },
    summary() {
      const total = steps.length;
      if (total === 0) throw new Error(`stage ${stage} ran 0 steps`);
      const passed = steps.filter((s) => s.ok).length;
      return { stage, total, passed, failed: total - passed, steps: this.results() };
    },
  };
}

export function assertEqual(a, b, msg) {
  if (!Object.is(a, b)) {
    throw new StepFailure(`${msg}: expected ${JSON.stringify(b)}, got ${JSON.stringify(a)}`);
  }
}

// Compares element counts regardless of order.
export function assertMultiset(got, expected, msg) {
  const sortedGot = [...got].sort();
  const sortedExpected = [...expected].sort();
  const same =
    sortedGot.length === sortedExpected.length && sortedGot.every((v, i) => Object.is(v, sortedExpected[i]));
  if (!same) {
    throw new StepFailure(`${msg}: expected ${JSON.stringify(sortedExpected)}, got ${JSON.stringify(sortedGot)}`);
  }
}

export function assertMatch(s, re, msg) {
  if (typeof s !== "string" || !re.test(s)) {
    throw new StepFailure(`${msg}: ${JSON.stringify(s)} does not match ${re}`);
  }
}
