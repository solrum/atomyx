import { writeFileSync } from "node:fs";
import type { RunSummary } from "../runner/spec-runner.js";

function escape(s: string): string {
  return s
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&apos;");
}

export function writeJUnitXml(summaries: RunSummary[], outputPath: string): void {
  const totalTests = summaries.length;
  const totalFailures = summaries.filter((s) => s.status === "failed").length;
  const totalErrors = summaries.filter((s) => s.status === "error").length;
  const totalTime = summaries.reduce((acc, s) => acc + s.durationMs / 1000, 0);

  const testcases = summaries
    .map((s) => {
      const time = (s.durationMs / 1000).toFixed(3);
      if (s.status === "passed") {
        return `    <testcase name="${escape(s.spec)}" classname="atomyx" time="${time}"/>`;
      }
      const failed = s.steps.filter((st) => st.status === "failed");
      const failureMessage = failed.map((f) => `step ${f.index} (${f.kind}): ${f.error ?? ""}`).join("\n");
      return `    <testcase name="${escape(s.spec)}" classname="atomyx" time="${time}">
      <failure message="${escape(failed[0]?.error ?? "verify failed")}">${escape(failureMessage)}</failure>
    </testcase>`;
    })
    .join("\n");

  const xml = `<?xml version="1.0" encoding="UTF-8"?>
<testsuites tests="${totalTests}" failures="${totalFailures}" errors="${totalErrors}" time="${totalTime.toFixed(3)}">
  <testsuite name="atomyx" tests="${totalTests}" failures="${totalFailures}" errors="${totalErrors}" time="${totalTime.toFixed(3)}">
${testcases}
  </testsuite>
</testsuites>
`;

  writeFileSync(outputPath, xml);
}
