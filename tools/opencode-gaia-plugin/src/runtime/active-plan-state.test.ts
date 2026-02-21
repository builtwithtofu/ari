import { mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { afterEach, describe, expect, test } from "bun:test";

import { writeActivePlanState } from "./active-plan-state";

const tempDirs: string[] = [];

afterEach(async () => {
  await Promise.all(
    tempDirs.splice(0).map(async (directory) => {
      await rm(directory, { recursive: true, force: true });
    }),
  );
});

describe("active plan state", () => {
  test("writes deterministic active-plan.json under runtime session", async () => {
    const repoRoot = await mkdtemp(join(tmpdir(), "gaia-active-plan-"));
    tempDirs.push(repoRoot);

    const result = await writeActivePlanState({
      repoRoot,
      sessionId: "s-active-1",
      workUnit: "unit-active-1",
      streamId: "stream-a",
      riskLevel: "medium",
      gate: "operator_approved",
      allowed: true,
      status: "ok",
      paths: {
        plan_path: ".gaia/unit-active-1/plan.md",
        log_path: ".gaia/unit-active-1/log.md",
        decisions_path: ".gaia/unit-active-1/decisions.md",
      },
      now: new Date("2026-02-21T10:30:00.000Z"),
    });

    expect(result.path).toBe(join(repoRoot, ".gaia", "runtime", "s-active-1", "active-plan.json"));

    const saved = JSON.parse(await readFile(result.path, "utf8")) as {
      session_id: string;
      work_unit: string;
      stream_id: string;
      risk_level: string;
      gate: string;
      allowed: boolean;
      status: string;
      updated_at: string;
      artifact_paths: {
        plan_path: string;
        log_path: string;
        decisions_path: string;
      };
    };

    expect(saved).toEqual({
      session_id: "s-active-1",
      work_unit: "unit-active-1",
      stream_id: "stream-a",
      risk_level: "medium",
      gate: "operator_approved",
      allowed: true,
      status: "ok",
      updated_at: "2026-02-21T10:30:00.000Z",
      artifact_paths: {
        plan_path: ".gaia/unit-active-1/plan.md",
        log_path: ".gaia/unit-active-1/log.md",
        decisions_path: ".gaia/unit-active-1/decisions.md",
      },
    });
  });
});
