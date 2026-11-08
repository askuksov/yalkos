import { test } from "node:test";
import assert from "node:assert/strict";
import { createRunner, assertEqual, assertMultiset, assertMatch, StepFailure, MissingPrerequisite, need } from "./check.mjs";

// Run fn with console.log captured so runner output does not pollute test output.
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

test("a passing and a failing step are both recorded, later steps still run", async () => {
  const r = createRunner("demo");
  const ran = [];
  const out = await capture(async () => {
    await r.step("first", () => ran.push("first"));
    await r.step("second", () => {
      ran.push("second");
      assertEqual(1, 2, "one is not two");
    });
    await r.step("third", () => ran.push("third"));
  });
  assert.deepEqual(ran, ["first", "second", "third"]);
  const s = r.summary();
  assert.equal(s.stage, "demo");
  assert.equal(s.total, 3);
  assert.equal(s.passed, 2);
  assert.equal(s.failed, 1);
  assert.deepEqual(s.steps.map((x) => [x.name, x.ok]), [["first", true], ["second", false], ["third", true]]);
  assert.match(s.steps[1].message, /one is not two/);
  assert.ok(out.includes("PASS demo/first"));
  assert.ok(out.some((l) => l.startsWith("FAIL demo/second") && l.includes("one is not two")));
  assert.equal(r.results().length, 3);
});

test("assertMultiset compares counts, not membership", () => {
  assertMultiset(["b", "a", "a"], ["a", "a", "b"], "same");
  assert.throws(
    () => assertMultiset(["a", "a", "b"], ["a", "b"], "counts differ"),
    (err) => {
      assert.ok(err instanceof StepFailure);
      assert.match(err.message, /counts differ/);
      assert.match(err.message, /"a","a","b"/);
      assert.match(err.message, /"a","b"/);
      return true;
    },
  );
});

test("a runner with 0 steps fails in summary()", () => {
  const r = createRunner("empty");
  assert.throws(() => r.summary(), /stage empty ran 0 steps/);
});

test("a thrown non-Error value is recorded as a failed step", async () => {
  const r = createRunner("odd");
  await capture(async () => {
    await r.step("throws string", () => {
      throw "boom";
    });
    await r.step("throws object", () => {
      throw { code: 7 };
    });
  });
  const s = r.summary();
  assert.equal(s.failed, 2);
  assert.equal(s.steps[0].ok, false);
  assert.match(s.steps[0].message, /boom/);
  assert.match(s.steps[1].message, /code/);
  assertMatch("abc", /b/, "match works");
});

test("need inside a step records a prerequisite failure; a plain StepFailure has no flag", async () => {
  const r = createRunner("pre");
  await capture(async () => {
    await r.step("lacks a value", () => {
      need(null, "identifier", "create");
    });
    await r.step("plain failure", () => {
      throw new StepFailure("expected 1, got 2");
    });
    await r.step("fine", () => {});
  });
  const steps = r.summary().steps;
  assert.equal(steps[0].ok, false);
  assert.equal(steps[0].prerequisite, true);
  assert.equal(steps[0].message, 'identifier is missing: step "create" did not complete');
  assert.equal(steps[1].ok, false);
  assert.ok(!steps[1].prerequisite);
  assert.ok(!steps[2].prerequisite);
  assert.throws(() => need(undefined, "x", "s"), MissingPrerequisite);
  assert.ok(new MissingPrerequisite("m") instanceof StepFailure);
});

test("need returns falsy values that are not null or undefined", () => {
  assert.equal(need(0, "count", "s"), 0);
  assert.equal(need("", "text", "s"), "");
  assert.equal(need(false, "flag", "s"), false);
});
