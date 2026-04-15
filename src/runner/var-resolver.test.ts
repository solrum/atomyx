import { test } from "node:test";
import { strict as assert } from "node:assert";
import { resolveDeep, resolveString } from "./var-resolver.ts";

test("resolveString — data path", () => {
  const out = resolveString("hello ${data.name}", { data: { name: "world" } });
  assert.equal(out, "hello world");
});

test("resolveString — env with default", () => {
  const out = resolveString("api=${env.MISSING:-fallback}", { env: {} });
  assert.equal(out, "api=fallback");
});

test("resolveString — env actual value wins over default", () => {
  const out = resolveString("api=${env.PRESENT:-fallback}", { env: { PRESENT: "real" } });
  assert.equal(out, "api=real");
});

test("resolveString — nested data path", () => {
  const out = resolveString("user=${data.user.email}", {
    data: { user: { email: "a@b.c" } },
  });
  assert.equal(out, "user=a@b.c");
});

test("resolveString — throws on unresolved variable", () => {
  assert.throws(
    () => resolveString("${data.missing}", { data: {} }),
    /unresolved variable/,
  );
});

test("resolveDeep — walks nested objects", () => {
  const out = resolveDeep(
    { name: "${data.app}", steps: [{ tap: { text: "${data.btn}" } }] },
    { data: { app: "myapp", btn: "Login" } },
  );
  assert.deepEqual(out, {
    name: "myapp",
    steps: [{ tap: { text: "Login" } }],
  });
});

test("resolveDeep — preserves non-string values", () => {
  const out = resolveDeep(
    { count: 5, enabled: true, ratio: 1.5, name: "${data.name}" },
    { data: { name: "x" } },
  );
  assert.deepEqual(out, { count: 5, enabled: true, ratio: 1.5, name: "x" });
});
