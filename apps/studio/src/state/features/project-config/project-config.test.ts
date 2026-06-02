import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { MockProjectConfigStore } from "../../../domain/features/project-config/index.js";
import { createProjectConfigFeature } from "./project-config.impl.js";

describe("project-config feature", () => {
  it("round-trips a JSON object under the current workspace", async () => {
    const port = new MockProjectConfigStore();
    const api = createProjectConfigFeature({
      port,
      getWorkspacePath: () => "/ws",
    });

    await api.writeJson("settings.json", { fontSize: 13, theme: "dark" });
    const value = await api.readJson<{ fontSize: number; theme: string }>(
      "settings.json",
    );
    assert.deepEqual(value, { fontSize: 13, theme: "dark" });
  });

  it("returns null for missing files", async () => {
    const port = new MockProjectConfigStore();
    const api = createProjectConfigFeature({
      port,
      getWorkspacePath: () => "/ws",
    });
    assert.equal(await api.readJson("missing.json"), null);
    assert.equal(await api.readText("missing.txt"), null);
  });

  it("isolates files per workspace", async () => {
    const port = new MockProjectConfigStore();
    let current = "/ws-a";
    const api = createProjectConfigFeature({
      port,
      getWorkspacePath: () => current,
    });
    await api.writeJson("settings.json", { theme: "dark" });

    current = "/ws-b";
    assert.equal(await api.readJson("settings.json"), null);

    await api.writeJson("settings.json", { theme: "light" });
    assert.deepEqual(await api.readJson("settings.json"), { theme: "light" });

    current = "/ws-a";
    assert.deepEqual(await api.readJson("settings.json"), { theme: "dark" });
  });

  it("reads return null without a workspace; writes throw", async () => {
    const port = new MockProjectConfigStore();
    const api = createProjectConfigFeature({
      port,
      getWorkspacePath: () => null,
    });
    assert.equal(await api.readJson("anything.json"), null);
    assert.equal(await api.readText("anything.txt"), null);
    await assert.rejects(
      api.writeJson("settings.json", { a: 1 }),
      /no workspace is open/,
    );
    await assert.rejects(
      api.writeText("environment.yml", "x: y"),
      /no workspace is open/,
    );
  });

  it("hasWorkspace reflects getWorkspacePath() live", async () => {
    const port = new MockProjectConfigStore();
    let current: string | null = null;
    const api = createProjectConfigFeature({
      port,
      getWorkspacePath: () => current,
    });
    assert.equal(api.hasWorkspace(), false);
    current = "/ws";
    assert.equal(api.hasWorkspace(), true);
  });

  it("listJsonDirectory enumerates a folder's .json entries", async () => {
    const port = new MockProjectConfigStore();
    await port.writeJson("/ws", "themes/forest.json", { id: "forest" });
    await port.writeJson("/ws", "themes/midnight.json", { id: "midnight" });
    await port.writeText("/ws", "themes/README.md", "ignored");
    await port.writeJson("/ws", "themes/nested/dark.json", { id: "dark" });
    const entries = await port.listJsonDirectory("/ws", "themes");
    const ids = (entries as readonly { id: string }[])
      .map((e) => e.id)
      .sort();
    assert.deepEqual(ids, ["forest", "midnight"]);
  });
});
