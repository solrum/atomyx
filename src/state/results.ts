/**
 * Test run + bug + finding storage. Shared between Mode B and Mode C.
 *
 * Bugs are critical/blocking issues. Findings are non-critical observations
 * (e.g. "this button has no contentDesc" — useful in exploratory mode).
 */

import { mkdirSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

export type BugSeverity = "low" | "medium" | "high" | "critical";

export interface Bug {
  id: string;
  severity: BugSeverity;
  title: string;
  description?: string;
  screenshotPath?: string;
  treeSnapshot?: unknown;
  context?: Record<string, unknown>;
  reportedAt: number;
}

export interface Finding {
  id: string;
  category: string;
  message: string;
  context?: Record<string, unknown>;
  reportedAt: number;
}

export type RunStatus = "running" | "passed" | "failed" | "error";

export interface TestRun {
  id: string;
  name: string;
  source: "scripted" | "exploratory" | "interactive";
  status: RunStatus;
  startedAt: number;
  finishedAt?: number;
  deviceId?: string;
  platform?: string;
  bugs: Bug[];
  findings: Finding[];
  meta: Record<string, unknown>;
}

export class ResultStore {
  private current: TestRun | null = null;
  private bugCounter = 0;
  private findingCounter = 0;

  startRun(input: Pick<TestRun, "name" | "source"> & Partial<Pick<TestRun, "deviceId" | "platform" | "meta">>): TestRun {
    this.current = {
      id: `run_${Date.now()}`,
      name: input.name,
      source: input.source,
      status: "running",
      startedAt: Date.now(),
      deviceId: input.deviceId,
      platform: input.platform,
      bugs: [],
      findings: [],
      meta: input.meta ?? {},
    };
    this.bugCounter = 0;
    this.findingCounter = 0;
    return this.current;
  }

  currentRun(): TestRun | null {
    return this.current;
  }

  reportBug(bug: Omit<Bug, "id" | "reportedAt">): Bug {
    if (!this.current) {
      this.startRun({ name: "ad-hoc", source: "interactive" });
    }
    const created: Bug = {
      id: `bug_${++this.bugCounter}`,
      reportedAt: Date.now(),
      ...bug,
    };
    this.current!.bugs.push(created);
    return created;
  }

  reportFinding(f: Omit<Finding, "id" | "reportedAt">): Finding {
    if (!this.current) {
      this.startRun({ name: "ad-hoc", source: "interactive" });
    }
    const created: Finding = {
      id: `find_${++this.findingCounter}`,
      reportedAt: Date.now(),
      ...f,
    };
    this.current!.findings.push(created);
    return created;
  }

  finishRun(status: RunStatus): TestRun | null {
    if (!this.current) return null;
    this.current.status = status;
    this.current.finishedAt = Date.now();
    return this.current;
  }

  /**
   * Persist current run to a JSON file in tmpdir/atomyx-results.
   * Returns the path. Caller may also POST to engine separately.
   */
  persistLocal(): string | null {
    if (!this.current) return null;
    const dir = join(tmpdir(), "atomyx-results");
    mkdirSync(dir, { recursive: true });
    const path = join(dir, `${this.current.id}.json`);
    writeFileSync(path, JSON.stringify(this.current, null, 2));
    return path;
  }
}

