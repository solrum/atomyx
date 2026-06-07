import { describe, it, mock, before, after } from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, rm } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";

// Capture and restore process.exit so tests that trigger the non-zero
// path do not terminate the process.
function withExitSpy(fn: (spy: { code: number | undefined }) => Promise<void>): () => Promise<void> {
  return async () => {
    const spy: { code: number | undefined } = { code: undefined };
    const original = process.exit.bind(process);
    // @ts-expect-error — narrowing process.exit to a spy for this test
    process.exit = (code?: number) => { spy.code = code; };
    try {
      await fn(spy);
    } finally {
      process.exit = original;
    }
  };
}

let tmpBase: string;

before(async () => {
  tmpBase = await mkdtemp(join(tmpdir(), "atomyx-skills-execute-test-"));
});

after(async () => {
  await rm(tmpBase, { recursive: true, force: true });
});

describe("skills execute — help", () => {
  it("undefined args → prints help, no exit", async () => {
    const { execute } = await import("./execute.js");
    const written: string[] = [];
    const origWrite = process.stdout.write.bind(process.stdout);
    process.stdout.write = (s: string) => { written.push(s); return true; };
    try {
      await execute([]);
    } finally {
      process.stdout.write = origWrite;
    }
    assert.ok(written.some((s) => s.includes("atomyx skills")));
  });

  it("help command → prints help, no exit", async () => {
    const { execute } = await import("./execute.js");
    const written: string[] = [];
    const origWrite = process.stdout.write.bind(process.stdout);
    process.stdout.write = (s: string) => { written.push(s); return true; };
    try {
      await execute(["help"]);
    } finally {
      process.stdout.write = origWrite;
    }
    assert.ok(written.some((s) => s.includes("COMMANDS")));
  });
});

describe("skills execute — unknown command", () => {
  it("unknown command calls process.exit(2)", withExitSpy(async (spy) => {
    const { execute } = await import("./execute.js");
    const origWrite = process.stderr.write.bind(process.stderr);
    process.stderr.write = () => true;
    try {
      await execute(["bogus-command"]);
    } finally {
      process.stderr.write = origWrite;
    }
    assert.equal(spy.code, 2);
  }));
});

describe("skills execute — init dispatch", () => {
  it("init with valid target returns without exit", async () => {
    const { execute } = await import("./execute.js");
    const targetDir = join(tmpBase, "exec-init-test");
    // process.exit must not be called on success (code 0 skips it)
    const origExit = process.exit.bind(process);
    let exited = false;
    // @ts-expect-error — spy
    process.exit = () => { exited = true; };
    try {
      await execute(["init", `--target=${targetDir}`]);
    } finally {
      process.exit = origExit;
    }
    assert.equal(exited, false, "process.exit must not be called on success");
  });

  it("init with unknown flag calls process.exit(2)", withExitSpy(async (spy) => {
    const { execute } = await import("./execute.js");
    const origWrite = process.stderr.write.bind(process.stderr);
    process.stderr.write = () => true;
    try {
      await execute(["init", "--no-such-flag"]);
    } finally {
      process.stderr.write = origWrite;
    }
    assert.equal(spy.code, 2);
  }));
});
