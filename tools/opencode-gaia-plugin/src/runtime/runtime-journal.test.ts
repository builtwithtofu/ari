import { mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { afterEach, describe, expect, test } from "bun:test";

import {
  appendRuntimeJournalEvent,
  readRuntimeJournalEvents,
  reduceRuntimeJournal,
} from "./runtime-journal";

const tempDirs: string[] = [];

afterEach(async () => {
  await Promise.all(
    tempDirs.splice(0).map(async (directory) => {
      await rm(directory, { recursive: true, force: true });
    }),
  );
});

describe("runtime journal", () => {
  test("appends ndjson events and reads them back", async () => {
    const repoRoot = await mkdtemp(join(tmpdir(), "gaia-journal-"));
    tempDirs.push(repoRoot);

    await appendRuntimeJournalEvent({
      repoRoot,
      event: {
        event_version: "1.0",
        event_type: "delegation_completed",
        timestamp: "2026-02-17T12:00:00.000Z",
        session_id: "s-j1",
        work_unit: "unit-9",
        stream_id: "default",
        status: "ok",
        attempt_count: 1,
        model_used: "openai/gpt-5.3-codex",
        parse_error: null,
      },
    });

    await appendRuntimeJournalEvent({
      repoRoot,
      event: {
        event_version: "1.0",
        event_type: "artifacts_written",
        timestamp: "2026-02-17T12:00:01.000Z",
        session_id: "s-j1",
        work_unit: "unit-9",
        stream_id: "default",
        plan_path: ".gaia/unit-9/plan.md",
        log_path: ".gaia/unit-9/log.md",
        decisions_path: ".gaia/unit-9/decisions.md",
      },
    });

    const events = await readRuntimeJournalEvents({
      repoRoot,
      sessionId: "s-j1",
      workUnit: "unit-9",
    });

    expect(events).toHaveLength(2);
    expect(events[0]?.event_type).toBe("delegation_completed");
    expect(events[1]?.event_type).toBe("artifacts_written");

    const raw = await readFile(
      join(repoRoot, ".gaia", "runtime", "s-j1", "unit-9.ndjson"),
      "utf8",
    );
    expect(raw.trim().split("\n")).toHaveLength(2);
  });

  test("reduces journal events into resumable state", () => {
    const state = reduceRuntimeJournal([
      {
        event_version: "1.0",
        event_type: "plan_gate_evaluated",
        timestamp: "2026-02-17T11:59:59.000Z",
        session_id: "s-j2",
        work_unit: "unit-10",
        stream_id: "stream-x",
        risk_level: "medium",
        operator_approved: true,
        allowed: true,
        gate: "operator_approved",
        reason: "Operator approval recorded for medium/high-risk work.",
      },
      {
        event_version: "1.0",
        event_type: "delegation_completed",
        timestamp: "2026-02-17T12:00:00.000Z",
        session_id: "s-j2",
        work_unit: "unit-10",
        stream_id: "stream-x",
        status: "parse_failed",
        attempt_count: 2,
        model_used: "openai/gpt-5.3-codex",
        parse_error: "No JSON object found",
      },
      {
        event_version: "1.0",
        event_type: "artifacts_written",
        timestamp: "2026-02-17T12:00:01.000Z",
        session_id: "s-j2",
        work_unit: "unit-10",
        stream_id: "stream-x",
        plan_path: ".gaia/unit-10/plan.md",
        log_path: ".gaia/unit-10/log.md",
        decisions_path: ".gaia/unit-10/decisions.md",
      },
      {
        event_version: "1.0",
        event_type: "work_units_pruned",
        timestamp: "2026-02-17T12:00:02.000Z",
        session_id: "s-j2",
        work_unit: "unit-10",
        stream_id: "stream-x",
        archived_work_units: ["unit-3"],
      },
    ]);

    expect(state.session_id).toBe("s-j2");
    expect(state.work_unit).toBe("unit-10");
    expect(state.stream_id).toBe("stream-x");
    expect(state.latest_risk_level).toBe("medium");
    expect(state.latest_gate).toBe("operator_approved");
    expect(state.gate_allowed).toBe(true);
    expect(state.latest_status).toBe("parse_failed");
    expect(state.last_error).toBe("No JSON object found");
    expect(state.rejection_feedback_pending).toBe(false);
    expect(state.rejection_feedback).toBeNull();
    expect(state.artifacts_written).toBe(true);
    expect(state.archived_work_units).toEqual(["unit-3"]);
  });

  test("marks work unit as awaiting GAIA-owned rejection feedback", () => {
    const state = reduceRuntimeJournal([
      {
        event_version: "1.0",
        event_type: "plan_gate_evaluated",
        timestamp: "2026-02-17T12:00:00.000Z",
        session_id: "s-j3",
        work_unit: "unit-11",
        stream_id: "stream-y",
        risk_level: "low",
        operator_approved: false,
        allowed: true,
        gate: "auto_approved",
        reason: "Plan gate auto-approved for low-risk work.",
      },
      {
        event_version: "1.0",
        event_type: "delegation_completed",
        timestamp: "2026-02-17T12:00:01.000Z",
        session_id: "s-j3",
        work_unit: "unit-11",
        stream_id: "stream-y",
        status: "parse_failed",
        attempt_count: 1,
        model_used: "opencode/glm-5-free",
        parse_error: "Rejection signal detected from delegated response.",
      },
      {
        event_version: "1.0",
        event_type: "rejection_feedback_requested",
        timestamp: "2026-02-17T12:00:02.000Z",
        session_id: "s-j3",
        work_unit: "unit-11",
        stream_id: "stream-y",
        owner_agent: "gaia",
        paused_agent: "hephaestus",
        question: "What should GAIA change after this rejection?",
        reason: "Delegated response indicates rejection that requires GAIA-owned feedback handling.",
      },
    ]);

    expect(state.rejection_feedback_pending).toBe(true);
    expect(state.rejection_feedback).toEqual({
      owner_agent: "gaia",
      paused_agent: "hephaestus",
      question: "What should GAIA change after this rejection?",
      reason: "Delegated response indicates rejection that requires GAIA-owned feedback handling.",
    });
  });
});
