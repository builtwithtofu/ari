import { mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { afterEach, describe, expect, test } from "bun:test";

import { createManualWorkspace, parseManualTuiArgs, parseManualWebArgs } from "./manual-session";

const tempDirs: string[] = [];

afterEach(async () => {
  await Promise.all(
    tempDirs.splice(0).map(async (directory) => {
      await rm(directory, { recursive: true, force: true });
    }),
  );
});

describe("createManualWorkspace", () => {
  test("creates timestamped workspace with sanitized label", async () => {
    const repoRoot = await mkdtemp(join(tmpdir(), "gaia-manual-"));
    tempDirs.push(repoRoot);

    const created = await createManualWorkspace({
      repoRoot,
      label: "Critical Bug!!",
      now: new Date("2026-02-17T12:00:00.000Z"),
    });

    expect(created.workspaceId).toBe("20260217-120000-critical-bug");
    expect(created.workspacePath).toBe(
      join(repoRoot, ".sandbox", "workspaces", "20260217-120000-critical-bug"),
    );

    const readme = await readFile(join(created.workspacePath, "README.md"), "utf8");
    expect(readme).toContain("manual TUI testing workspace");
    expect(readme).toContain("feature-x");
    expect(readme).toContain("urgent-bug");
    expect(readme).toContain("go-hello-planning/");
    expect(readme).toContain("research-ops-planning/");
    expect(readme).toContain("bug-hunt/");

    const helloGoMain = await readFile(
      join(created.workspacePath, "go-hello-planning", "cmd", "hello", "main.go"),
      "utf8",
    );
    expect(helloGoMain).toContain("Hello, world!");

    const bugReport = await readFile(
      join(created.workspacePath, "bug-hunt", "bug-report.md"),
      "utf8",
    );
    expect(bugReport).toContain("Expected total: 1350");

    const researchScenario = await readFile(
      join(created.workspacePath, "research-ops-planning", "README.md"),
      "utf8",
    );
    expect(researchScenario).toContain("cross-functional task without writing code");
  });

  test("falls back to sandbox-work when label is empty", async () => {
    const repoRoot = await mkdtemp(join(tmpdir(), "gaia-manual-"));
    tempDirs.push(repoRoot);

    const created = await createManualWorkspace({
      repoRoot,
      label: "!!!",
      now: new Date("2026-02-17T12:00:00.000Z"),
    });

    expect(created.workspaceId).toBe("20260217-120000-sandbox-work");
  });
});

describe("parseManualTuiArgs", () => {
  test("parses label and model flag", () => {
    const parsed = parseManualTuiArgs(["critical", "bug", "--model", "opencode/glm-5-free"]);

    expect(parsed).toEqual({
      label: "critical bug",
      model: "opencode/glm-5-free",
    });
  });

  test("supports model flag before label", () => {
    const parsed = parseManualTuiArgs(["--model", "anthropic/claude-sonnet-4", "feature", "x"]);

    expect(parsed).toEqual({
      label: "feature x",
      model: "anthropic/claude-sonnet-4",
    });
  });

  test("throws when --model has no value", () => {
    expect(() => parseManualTuiArgs(["critical", "--model"])).toThrow(
      "manual-tui requires a model value after --model",
    );
  });

  test("rejects --port for manual-tui", () => {
    expect(() => parseManualTuiArgs(["critical", "--port", "4096"])).toThrow(
      "manual-tui does not support --port",
    );
  });
});

describe("parseManualWebArgs", () => {
  test("parses label, model, and port", () => {
    const parsed = parseManualWebArgs([
      "critical",
      "bug",
      "--model",
      "opencode/glm-5-free",
      "--port",
      "4199",
    ]);

    expect(parsed).toEqual({
      label: "critical bug",
      model: "opencode/glm-5-free",
      port: "4199",
    });
  });

  test("throws when --port is missing value", () => {
    expect(() => parseManualWebArgs(["--port"])).toThrow(
      "manual-web requires a port value after --port",
    );
  });

  test("throws on invalid port values", () => {
    expect(() => parseManualWebArgs(["--port", "abc"])).toThrow(
      "manual-web received invalid port: abc",
    );
  });
});
