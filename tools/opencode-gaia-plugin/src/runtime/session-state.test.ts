import { mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { afterEach, describe, expect, test } from "bun:test";

import { appendRuntimeJournalEvent } from "./runtime-journal";
import { aggregateSessionRuntimeState, refreshSessionRuntimeState } from "./session-state";

const tempDirs: string[] = [];

afterEach(async () => {
  await Promise.all(
    tempDirs.splice(0).map(async (directory) => {
      await rm(directory, { recursive: true, force: true });
    }),
  );
});

describe("session-state", () => {
  test("aggregates completed/active/blocked work units", () => {
    const state = aggregateSessionRuntimeState({
      session_id: "s-agg",
      work_units: {
        "unit-complete": {
          session_id: "s-agg",
          work_unit: "unit-complete",
          stream_id: "stream-a",
          vcs_context: null,
          latest_risk_level: "low",
          latest_gate: "auto_approved",
          gate_allowed: true,
          gate_reason: "auto",
          latest_status: "ok",
          last_model_used: "openai/gpt-5.3-codex",
          last_error: null,
          rejection_feedback_pending: false,
          rejection_feedback: null,
          artifacts_written: true,
          artifact_paths: null,
          archived_work_units: [],
          event_count: 3,
          last_event_at: "2026-02-17T12:00:00.000Z",
        },
        "unit-blocked": {
          session_id: "s-agg",
          work_unit: "unit-blocked",
          stream_id: "stream-a",
          vcs_context: null,
          latest_risk_level: "medium",
          latest_gate: "needs_operator_approval",
          gate_allowed: false,
          gate_reason: "approval required",
          latest_status: null,
          last_model_used: null,
          last_error: null,
          rejection_feedback_pending: false,
          rejection_feedback: null,
          artifacts_written: false,
          artifact_paths: null,
          archived_work_units: [],
          event_count: 1,
          last_event_at: "2026-02-17T12:01:00.000Z",
        },
        "unit-active": {
          session_id: "s-agg",
          work_unit: "unit-active",
          stream_id: "stream-b",
          vcs_context: null,
          latest_risk_level: "low",
          latest_gate: "auto_approved",
          gate_allowed: true,
          gate_reason: "auto",
          latest_status: "parse_failed",
          last_model_used: "openai/gpt-5.3-codex",
          last_error: "No JSON object found",
          rejection_feedback_pending: true,
          rejection_feedback: {
            owner_agent: "gaia",
            paused_agent: "hephaestus",
            question: "What should GAIA change after this rejection?",
            reason: "Delegated response indicates rejection that requires GAIA-owned feedback handling.",
          },
          artifacts_written: true,
          artifact_paths: null,
          archived_work_units: [],
          event_count: 2,
          last_event_at: "2026-02-17T12:02:00.000Z",
        },
      },
    });

    expect(state.completed_work_units).toEqual(["unit-complete"]);
    expect(state.blocked_work_units).toEqual(["unit-active", "unit-blocked"]);
    expect(state.active_work_units).toEqual([]);
    expect(state.current_stream_id).toBe("stream-b");
  });

  test("rebuilds session state from runtime journal and writes state.json", async () => {
    const repoRoot = await mkdtemp(join(tmpdir(), "gaia-session-state-"));
    tempDirs.push(repoRoot);

    await appendRuntimeJournalEvent({
      repoRoot,
      event: {
        event_version: "1.0",
        event_type: "plan_gate_evaluated",
        timestamp: "2026-02-17T12:00:00.000Z",
        session_id: "s-refresh",
        work_unit: "unit-1",
        stream_id: "feature-x",
        risk_level: "low",
        operator_approved: false,
        allowed: true,
        gate: "auto_approved",
        reason: "low risk",
      },
    });

    await appendRuntimeJournalEvent({
      repoRoot,
      event: {
        event_version: "1.0",
        event_type: "delegation_completed",
        timestamp: "2026-02-17T12:00:01.000Z",
        session_id: "s-refresh",
        work_unit: "unit-1",
        stream_id: "feature-x",
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
        timestamp: "2026-02-17T12:00:02.000Z",
        session_id: "s-refresh",
        work_unit: "unit-1",
        stream_id: "feature-x",
        plan_path: ".gaia/unit-1/plan.md",
        log_path: ".gaia/unit-1/log.md",
        decisions_path: ".gaia/unit-1/decisions.md",
      },
    });

    const state = await refreshSessionRuntimeState({
      repoRoot,
      sessionId: "s-refresh",
    });

    expect(state.completed_work_units).toEqual(["unit-1"]);
    expect(state.blocked_work_units).toEqual([]);
    expect(state.current_stream_id).toBe("feature-x");

    const persisted = JSON.parse(
      await readFile(join(repoRoot, ".gaia", "runtime", "s-refresh", "state.json"), "utf8"),
    ) as { completed_work_units: string[] };
    expect(persisted.completed_work_units).toEqual(["unit-1"]);
  });
});
