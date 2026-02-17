import { mkdtemp, readFile, readdir, rm, utimes } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { afterEach, describe, expect, test } from "bun:test";

import {
  getPlanGaiaPaths,
  prunePlanGaiaWorkUnits,
  readPlanGaia,
  writePlanGaia,
} from "./plan-gaia";

const tempDirs: string[] = [];

afterEach(async () => {
  await Promise.all(
    tempDirs.splice(0).map(async (directory) => {
      await rm(directory, { recursive: true, force: true });
    }),
  );
});

describe("plan-gaia paths", () => {
  test("builds .gaia work-unit file paths", () => {
    const paths = getPlanGaiaPaths("/repo", "unit-1");

    expect(paths.base_dir).toBe("/repo/.gaia/unit-1");
    expect(paths.plan_path).toBe("/repo/.gaia/unit-1/plan.md");
    expect(paths.log_path).toBe("/repo/.gaia/unit-1/log.md");
    expect(paths.decisions_path).toBe("/repo/.gaia/unit-1/decisions.md");
  });

  test("rejects invalid work-unit identifiers", () => {
    expect(() => getPlanGaiaPaths("/repo", "../escape")).toThrow();
    expect(() => getPlanGaiaPaths("/repo", "unit/1")).toThrow();
  });
});

describe("plan-gaia io", () => {
  test("writes and reads plan/log/decisions files", async () => {
    const repoRoot = await mkdtemp(join(tmpdir(), "gaia-plan-"));
    tempDirs.push(repoRoot);

    await writePlanGaia({
      repoRoot,
      workUnit: "unit-2",
      plan: "# Plan\n- implement",
      log: "# Log\n- started",
      decisions: "# Decisions\n- use bun",
    });

    const readBack = await readPlanGaia({ repoRoot, workUnit: "unit-2" });

    expect(readBack.plan).toBe("# Plan\n- implement");
    expect(readBack.log).toBe("# Log\n- started");
    expect(readBack.decisions).toBe("# Decisions\n- use bun");

    const direct = await readFile(join(repoRoot, ".gaia", "unit-2", "plan.md"), "utf8");
    expect(direct).toBe("# Plan\n- implement");
  });

  test("prunes older work units into .gaia/archive while preserving keep targets", async () => {
    const repoRoot = await mkdtemp(join(tmpdir(), "gaia-plan-"));
    tempDirs.push(repoRoot);

    for (const workUnit of ["unit-a", "unit-b", "unit-c"] as const) {
      await writePlanGaia({
        repoRoot,
        workUnit,
        plan: `# Plan\n- ${workUnit}`,
        log: `# Log\n- ${workUnit}`,
        decisions: `# Decisions\n- ${workUnit}`,
      });
    }

    const at = new Date("2026-02-17T12:00:00.000Z");
    await utimes(join(repoRoot, ".gaia", "unit-a", "plan.md"), at, new Date(at.getTime() - 3_000));
    await utimes(join(repoRoot, ".gaia", "unit-b", "plan.md"), at, new Date(at.getTime() - 2_000));
    await utimes(join(repoRoot, ".gaia", "unit-c", "plan.md"), at, new Date(at.getTime() - 1_000));

    const result = await prunePlanGaiaWorkUnits({
      repoRoot,
      keepLatest: 1,
      keepWorkUnits: ["unit-a"],
      now: at,
    });

    expect(result.scanned).toBe(3);
    expect(result.kept).toEqual(["unit-c", "unit-a"]);
    expect(result.archived).toEqual(["unit-b"]);

    await expect(readFile(join(repoRoot, ".gaia", "unit-b", "plan.md"), "utf8")).rejects.toThrow();

    const archived = await readdir(join(repoRoot, ".gaia", "archive", "work-units"));
    expect(archived).toEqual([
      "unit-b--2026-02-17T12-00-00-000Z",
    ]);
  });

  test("returns empty result when .gaia is missing", async () => {
    const repoRoot = await mkdtemp(join(tmpdir(), "gaia-plan-"));
    tempDirs.push(repoRoot);

    const result = await prunePlanGaiaWorkUnits({
      repoRoot,
      keepLatest: 2,
    });

    expect(result).toEqual({
      scanned: 0,
      kept: [],
      archived: [],
    });
  });

  test("rejects invalid keepLatest values", async () => {
    const repoRoot = await mkdtemp(join(tmpdir(), "gaia-plan-"));
    tempDirs.push(repoRoot);

    await expect(
      prunePlanGaiaWorkUnits({
        repoRoot,
        keepLatest: 0,
      }),
    ).rejects.toThrow("keepLatest must be a positive integer");
  });
});
