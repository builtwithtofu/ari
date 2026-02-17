import { mkdtemp, readFile, readdir, rm, utimes } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { afterEach, describe, expect, test } from "bun:test";

import { processWorkUnit } from "./process-work-unit";
import { readRuntimeJournalEvents } from "./runtime-journal";

const tempDirs: string[] = [];

afterEach(async () => {
  await Promise.all(
    tempDirs.splice(0).map(async (directory) => {
      await rm(directory, { recursive: true, force: true });
    }),
  );
});

describe("processWorkUnit", () => {
  test("delegates, collects, and persists .gaia files", async () => {
    const repoRoot = await mkdtemp(join(tmpdir(), "gaia-runtime-"));
    tempDirs.push(repoRoot);

    const result = await processWorkUnit({
      repoRoot,
      workUnit: "unit-4",
      sessionId: "s1",
      modelUsed: "openai/gpt-5.3-codex",
      responseText: '{"contract_version":"1.0","agent":"gaia"}',
      parse: (input) => input,
      plan: "# Plan\n- next",
      log: "# Log\n- running",
      decisions: "# Decisions\n- keep scope small",
    });

    expect(result.delegation.status).toBe("ok");
    expect(result.collection.total).toBe(1);
    expect(result.collection.success_count).toBe(1);

    const planFile = await readFile(join(repoRoot, ".gaia", "unit-4", "plan.md"), "utf8");
    expect(planFile).toBe("# Plan\n- next");

    const events = await readRuntimeJournalEvents({
      repoRoot,
      sessionId: "s1",
      workUnit: "unit-4",
    });
    expect(events.map((event) => event.event_type)).toEqual([
      "plan_gate_evaluated",
      "delegation_completed",
      "artifacts_written",
      "work_units_pruned",
    ]);

    const stateJson = JSON.parse(
      await readFile(join(repoRoot, ".gaia", "runtime", "s1", "state.json"), "utf8"),
    ) as {
      completed_work_units: string[];
      blocked_work_units: string[];
    };

    expect(stateJson.completed_work_units).toContain("unit-4");
    expect(stateJson.blocked_work_units).toEqual([]);

    const statusDoc = await readFile(
      join(repoRoot, ".gaia", "plans", "session-s1-status.md"),
      "utf8",
    );
    expect(statusDoc).toContain("Session Status: s1");
    expect(statusDoc).toContain("unit-4");
  });

  test("blocks medium-risk work without operator approval", async () => {
    const repoRoot = await mkdtemp(join(tmpdir(), "gaia-runtime-"));
    tempDirs.push(repoRoot);

    await expect(
      processWorkUnit({
        repoRoot,
        workUnit: "unit-gate-1",
        sessionId: "s-gate-1",
        modelUsed: "openai/gpt-5.3-codex",
        responseText: '{"contract_version":"1.0","agent":"gaia"}',
        parse: (input) => input,
        plan: "# Plan\n- gated",
        log: "# Log\n- gated",
        decisions: "# Decisions\n- gated",
        riskLevel: "medium",
      }),
    ).rejects.toThrow("Plan gate requires operator approval for medium-risk work");

    const events = await readRuntimeJournalEvents({
      repoRoot,
      sessionId: "s-gate-1",
      workUnit: "unit-gate-1",
    });
    expect(events).toHaveLength(1);
    expect(events[0]?.event_type).toBe("plan_gate_evaluated");

    const stateJson = JSON.parse(
      await readFile(
        join(repoRoot, ".gaia", "runtime", "s-gate-1", "state.json"),
        "utf8",
      ),
    ) as {
      blocked_work_units: string[];
    };
    expect(stateJson.blocked_work_units).toContain("unit-gate-1");
  });

  test("preserves parse-failed status while still writing artifacts", async () => {
    const repoRoot = await mkdtemp(join(tmpdir(), "gaia-runtime-"));
    tempDirs.push(repoRoot);

    const result = await processWorkUnit({
      repoRoot,
      workUnit: "unit-5",
      sessionId: "s2",
      modelUsed: "openai/gpt-5.3-codex",
      responseText: "not-json",
      retry: async () => "still-not-json",
      parse: (input) => input,
      plan: "# Plan\n- fallback",
      log: "# Log\n- parse failed",
      decisions: "# Decisions\n- capture error",
    });

    expect(result.delegation.status).toBe("parse_failed");
    expect(result.collection.failure_count).toBe(1);

    const decisionsFile = await readFile(
      join(repoRoot, ".gaia", "unit-5", "decisions.md"),
      "utf8",
    );
    expect(decisionsFile).toBe("# Decisions\n- capture error");
  });

  test("pauses delegated agent and requests GAIA-owned rejection feedback", async () => {
    const repoRoot = await mkdtemp(join(tmpdir(), "gaia-runtime-"));
    tempDirs.push(repoRoot);

    const result = await processWorkUnit({
      repoRoot,
      workUnit: "unit-reject-1",
      sessionId: "s-reject-1",
      modelUsed: "opencode/glm-5-free",
      responseText: "Operator rejected this specialist output and requested different approach",
      parse: (input) => input,
      plan: "# Plan\n- rejection path",
      log: "# Log\n- waiting for gaia feedback",
      decisions: "# Decisions\n- pending feedback",
      delegatedAgent: "hephaestus",
    });

    expect(result.delegation.status).toBe("parse_failed");
    expect(result.delegation.rejection_feedback_request).toEqual({
      owner_agent: "gaia",
      paused_agent: null,
      question: "What should GAIA change after this rejection?",
      reason: "Delegated response indicates rejection that requires GAIA-owned feedback handling.",
    });

    const events = await readRuntimeJournalEvents({
      repoRoot,
      sessionId: "s-reject-1",
      workUnit: "unit-reject-1",
    });

    expect(events.map((event) => event.event_type)).toEqual([
      "plan_gate_evaluated",
      "delegation_completed",
      "rejection_feedback_requested",
      "artifacts_written",
      "work_units_pruned",
    ]);

    const rejectionEvent = events.find(
      (event) => event.event_type === "rejection_feedback_requested",
    );
    expect(rejectionEvent?.event_type).toBe("rejection_feedback_requested");
    if (rejectionEvent?.event_type === "rejection_feedback_requested") {
      expect(rejectionEvent.owner_agent).toBe("gaia");
      expect(rejectionEvent.paused_agent).toBe("hephaestus");
      expect(rejectionEvent.question).toBe("What should GAIA change after this rejection?");
    }

    const stateJson = JSON.parse(
      await readFile(
        join(repoRoot, ".gaia", "runtime", "s-reject-1", "state.json"),
        "utf8",
      ),
    ) as {
      blocked_work_units: string[];
      active_work_units: string[];
    };

    expect(stateJson.blocked_work_units).toContain("unit-reject-1");
    expect(stateJson.active_work_units).toEqual([]);
  });

  test("blocks artifact writes in locked mode", async () => {
    const repoRoot = await mkdtemp(join(tmpdir(), "gaia-runtime-"));
    tempDirs.push(repoRoot);

    await expect(
      processWorkUnit({
        repoRoot,
        workUnit: "unit-locked-1",
        sessionId: "s3",
        modelUsed: "openai/gpt-5.3-codex",
        responseText: '{"contract_version":"1.0","agent":"gaia"}',
        parse: (input) => input,
        plan: "# Plan\n- blocked",
        log: "# Log\n- blocked",
        decisions: "# Decisions\n- blocked",
        mode: "locked",
      }),
    ).rejects.toThrow("Locked mode blocks plan_gaia writes");

    await expect(readFile(join(repoRoot, ".gaia", "unit-locked-1", "plan.md"), "utf8")).rejects.toThrow();
  });

  test("archives older work-unit artifacts when retention limit is exceeded", async () => {
    const repoRoot = await mkdtemp(join(tmpdir(), "gaia-runtime-"));
    tempDirs.push(repoRoot);

    await processWorkUnit({
      repoRoot,
      workUnit: "unit-old",
      sessionId: "s-old",
      modelUsed: "openai/gpt-5.3-codex",
      responseText: '{"contract_version":"1.0","agent":"gaia"}',
      parse: (input) => input,
      plan: "# Plan\n- old",
      log: "# Log\n- old",
      decisions: "# Decisions\n- old",
      maxWorkUnits: 1,
    });

    const baseTime = new Date("2026-02-17T12:00:00.000Z");
    await Promise.all([
      utimes(join(repoRoot, ".gaia", "unit-old", "plan.md"), baseTime, new Date(baseTime.getTime() - 5_000)),
      utimes(join(repoRoot, ".gaia", "unit-old", "log.md"), baseTime, new Date(baseTime.getTime() - 5_000)),
      utimes(join(repoRoot, ".gaia", "unit-old", "decisions.md"), baseTime, new Date(baseTime.getTime() - 5_000)),
    ]);

    await processWorkUnit({
      repoRoot,
      workUnit: "unit-new",
      sessionId: "s-new",
      modelUsed: "openai/gpt-5.3-codex",
      responseText: '{"contract_version":"1.0","agent":"gaia"}',
      parse: (input) => input,
      plan: "# Plan\n- new",
      log: "# Log\n- new",
      decisions: "# Decisions\n- new",
      maxWorkUnits: 1,
    });

    await expect(readFile(join(repoRoot, ".gaia", "unit-old", "plan.md"), "utf8")).rejects.toThrow();
    expect(await readFile(join(repoRoot, ".gaia", "unit-new", "plan.md"), "utf8")).toBe("# Plan\n- new");

    const archived = await readdir(join(repoRoot, ".gaia", "archive", "work-units"));
    expect(archived).toHaveLength(1);
    expect(archived[0]).toMatch(/^unit-old--/);
  });
});
