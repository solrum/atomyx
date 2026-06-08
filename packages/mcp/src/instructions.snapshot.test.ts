import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";
import { INSTRUCTIONS_TEXT } from "./bin.js";

const __dirname = dirname(fileURLToPath(import.meta.url));
const SNAPSHOT_PATH = join(__dirname, "instructions.snapshot.txt");

const UPDATE = process.argv.includes("--update");

if (UPDATE) {
  writeFileSync(SNAPSHOT_PATH, INSTRUCTIONS_TEXT, "utf8");
  process.stdout.write(`Updated snapshot: ${SNAPSHOT_PATH}\n`);
  process.exit(0);
}

test("MCP instructions match committed snapshot", () => {
  const committed = readFileSync(SNAPSHOT_PATH, "utf8");
  assert.equal(
    INSTRUCTIONS_TEXT,
    committed,
    "INSTRUCTIONS_TEXT in bin.ts does not match instructions.snapshot.txt — " +
      "run `node --import tsx packages/mcp/src/instructions.snapshot.test.ts --update` to regenerate",
  );
});
