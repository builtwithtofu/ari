import { mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { afterEach, describe, expect, test } from "bun:test";

import { runDelegateGaiaTool } from "./delegate-gaia-tool";

const tempDirs: string[] = [];

afterEach(async () => {
  await Promise.all(
    tempDirs.splice(0).map(async (directory) => {
      await rm(directory, { recursive: true, force: true });
    }),
  );
});

describe("runDelegateGaiaTool", () => {
  test("parses lean contract output and writes artifacts", async () => {
    const repoRoot = await mkdtemp(join(tmpdir(), "gaia-tool-"));
    tempDirs.push(repoRoot);

    const responseText = JSON.stringify({
      contract_version: "1.0",
      agent: "demeter",
      work_unit: "unit-6",
      session_id: "s-tool-1",
      ok: true,
      data: {
        log_entry: "Captured progress",
        decisions: [],
        learnings: ["keep tests exact"],
        plan_updates: ["unit-6 complete"],
        session_summary: "done",
        status_report: {
          active_work_units: ["unit-6"],
          completed_work_units: ["unit-5"],
          blocked_work_units: [],
          upcoming_checkpoints: ["review unit-6 output"],
        },
      },
      errors: [],
    });

    const result = await runDelegateGaiaTool({
      repoRoot,
      workUnit: "unit-6",
      sessionId: "s-tool-1",
      modelUsed: "openai/gpt-5.3-codex",
      agent: "demeter",
      responseText,
      artifacts: {
        plan: "# Plan\n- track",
        log: "# Log\n- demeter",
        decisions: "# Decisions\n- captured",
      },
    });

    expect(result.delegation.status).toBe("ok");
    expect(result.delegation.parsed_json?.agent).toBe("demeter");

    const logFile = await readFile(join(repoRoot, ".gaia", "unit-6", "log.md"), "utf8");
    expect(logFile).toBe("# Log\n- demeter");
  });

  test("returns parse_failed and still writes default artifacts", async () => {
    const repoRoot = await mkdtemp(join(tmpdir(), "gaia-tool-"));
    tempDirs.push(repoRoot);

    const result = await runDelegateGaiaTool({
      repoRoot,
      workUnit: "unit-7",
      sessionId: "s-tool-2",
      modelUsed: "openai/gpt-5.3-codex",
      agent: "gaia",
      responseText: "not-json",
    });

    expect(result.delegation.status).toBe("parse_failed");
    expect(result.collection.failure_count).toBe(1);

    const planFile = await readFile(join(repoRoot, ".gaia", "unit-7", "plan.md"), "utf8");
    expect(planFile).toBe("# Plan\n");
  });

  test("routes rejection feedback ownership to GAIA and pauses delegated agent", async () => {
    const repoRoot = await mkdtemp(join(tmpdir(), "gaia-tool-"));
    tempDirs.push(repoRoot);

    const result = await runDelegateGaiaTool({
      repoRoot,
      workUnit: "unit-8",
      sessionId: "s-tool-3",
      modelUsed: "opencode/glm-5-free",
      agent: "hephaestus",
      responseText: "User rejected this subagent output and wants a different path",
    });

    expect(result.delegation.status).toBe("parse_failed");
    expect(result.delegation.rejection_feedback_request).toEqual({
      owner_agent: "gaia",
      paused_agent: "hephaestus",
      question: "What should GAIA change after this rejection?",
      reason: "Delegated response indicates rejection that requires GAIA-owned feedback handling.",
    });
  });

  test("blocks delegate flow in locked mode", async () => {
    const repoRoot = await mkdtemp(join(tmpdir(), "gaia-tool-"));
    tempDirs.push(repoRoot);

    await expect(
      runDelegateGaiaTool({
        repoRoot,
        workUnit: "unit-locked-2",
        sessionId: "s-tool-locked",
        modelUsed: "openai/gpt-5.3-codex",
        agent: "gaia",
        responseText: '{"contract_version":"1.0","agent":"gaia"}',
        mode: "locked",
      }),
    ).rejects.toThrow("Locked mode blocks plan_gaia writes");
  });
});
