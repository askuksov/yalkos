// Minimal step runner for compatibility checks. No dependencies.

export class StepFailure extends Error {
  constructor(message) {
    super(message);
    this.name = "StepFailure";
  }
}

// A step that could not run because an earlier step did not produce the value it
// needs. It is a consequence of that earlier failure, never a cause of its own.
export class MissingPrerequisite extends StepFailure {
  constructor(message) {
    super(message);
    this.name = "MissingPrerequisite";
  }
}

// Fails the step with a clear text when an earlier step did not produce its value.
// Only for values stored by an earlier step; a lookup inside a step is a plain failure.
export function need(value, what, step) {
  if (value === undefined || value === null) throw new MissingPrerequisite(`${what} is missing: step "${step}" did not complete`);
  return value;
}

// Runs change() and then settle() (waiting for what the change caused). Returns the
// error change() threw, or null. A missing prerequisite is rethrown at once: nothing
// was changed, so there is nothing to wait for, and a failing settle() must not hide it.
export async function changeThenSettle(change, settle) {
  let changeError = null;
  try {
    await change();
  } catch (e) {
    changeError = e;
  }
  if (changeError instanceof MissingPrerequisite) throw changeError;
  await settle();
  return changeError;
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
        const record = { name, ok: false, message };
        if (err instanceof MissingPrerequisite) record.prerequisite = true;
        steps.push(record);
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
