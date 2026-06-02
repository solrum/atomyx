import { test } from "node:test";
import { strict as assert } from "node:assert";
import { fuzzyScore } from "./fuzzy-score.js";

test("fuzzyScore", async (t) => {
  await t.test("scores exact equality highest", () => {
    assert.equal(fuzzyScore("foo", "foo"), 1000);
  });

  await t.test("is case-insensitive", () => {
    assert.equal(fuzzyScore("FOO", "foo"), 1000);
    assert.equal(fuzzyScore("foo", "FOO"), 1000);
  });

  await t.test("rewards prefix matches over substring matches", () => {
    const prefix = fuzzyScore("foo", "foobar");
    const sub = fuzzyScore("bar", "foobar");
    assert.ok(prefix > sub, `prefix ${prefix} should beat substring ${sub}`);
  });

  await t.test("substring score decays with position and length", () => {
    const early = fuzzyScore("bar", "barfoo");
    const late = fuzzyScore("bar", "foobar");
    assert.ok(early > late);
  });

  await t.test("returns -1 when query characters do not all appear", () => {
    assert.equal(fuzzyScore("xyz", "foobar"), -1);
  });

  await t.test("subsequence matches score below substrings", () => {
    const sub = fuzzyScore("fbr", "foobar");
    const exact = fuzzyScore("foo", "foobar");
    assert.ok(sub < exact);
    assert.ok(sub >= 0, "subsequence still matches");
  });

  await t.test("ranks tighter subsequences higher", () => {
    const tight = fuzzyScore("ab", "ab_x");
    const loose = fuzzyScore("ab", "a__b");
    assert.ok(tight >= loose);
  });
});
