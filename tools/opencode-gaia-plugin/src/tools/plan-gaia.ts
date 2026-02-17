import { mkdir, readFile, readdir, rename, stat, writeFile } from "node:fs/promises";
import { join } from "node:path";

const WORK_UNIT_PATTERN = /^[A-Za-z0-9][A-Za-z0-9._-]*$/;
const WORK_UNIT_ARTIFACT_FILES = ["plan.md", "log.md", "decisions.md"] as const;
const RESERVED_GAIA_DIRECTORIES = new Set(["plans", "runtime", "archive"]);

const DEFAULT_ARCHIVE_SUBDIR = join("archive", "work-units");

export interface PlanGaiaPaths {
  base_dir: string;
  plan_path: string;
  log_path: string;
  decisions_path: string;
}

export interface PlanGaiaWriteArgs {
  repoRoot: string;
  workUnit: string;
  plan: string;
  log: string;
  decisions: string;
}

export interface PlanGaiaReadArgs {
  repoRoot: string;
  workUnit: string;
}

export interface PlanGaiaReadResult {
  plan: string;
  log: string;
  decisions: string;
}

export interface PrunePlanGaiaWorkUnitsArgs {
  repoRoot: string;
  keepLatest: number;
  keepWorkUnits?: string[];
  now?: Date;
}

export interface PrunePlanGaiaWorkUnitsResult {
  scanned: number;
  kept: string[];
  archived: string[];
}

interface PlanGaiaWorkUnitEntry {
  workUnit: string;
  baseDir: string;
  modifiedAtMs: number;
}

function validateWorkUnit(workUnit: string): void {
  if (!WORK_UNIT_PATTERN.test(workUnit)) {
    throw new Error(`Invalid work unit: ${workUnit}`);
  }
}

export function getPlanGaiaPaths(repoRoot: string, workUnit: string): PlanGaiaPaths {
  validateWorkUnit(workUnit);

  const baseDir = join(repoRoot, ".gaia", workUnit);
  return {
    base_dir: baseDir,
    plan_path: join(baseDir, "plan.md"),
    log_path: join(baseDir, "log.md"),
    decisions_path: join(baseDir, "decisions.md"),
  };
}

export async function writePlanGaia(args: PlanGaiaWriteArgs): Promise<PlanGaiaPaths> {
  const paths = getPlanGaiaPaths(args.repoRoot, args.workUnit);
  await mkdir(paths.base_dir, { recursive: true });

  await Promise.all([
    writeFile(paths.plan_path, args.plan, "utf8"),
    writeFile(paths.log_path, args.log, "utf8"),
    writeFile(paths.decisions_path, args.decisions, "utf8"),
  ]);

  return paths;
}

export async function readPlanGaia(args: PlanGaiaReadArgs): Promise<PlanGaiaReadResult> {
  const paths = getPlanGaiaPaths(args.repoRoot, args.workUnit);
  const [plan, log, decisions] = await Promise.all([
    readFile(paths.plan_path, "utf8"),
    readFile(paths.log_path, "utf8"),
    readFile(paths.decisions_path, "utf8"),
  ]);

  return { plan, log, decisions };
}

async function pathExists(path: string): Promise<boolean> {
  try {
    await stat(path);
    return true;
  } catch {
    return false;
  }
}

async function resolveWorkUnitModifiedAtMs(baseDir: string): Promise<number> {
  const stats = await Promise.all(
    WORK_UNIT_ARTIFACT_FILES.map(async (fileName) => stat(join(baseDir, fileName))),
  );

  return Math.max(...stats.map((entry) => entry.mtimeMs));
}

async function listPlanGaiaWorkUnits(repoRoot: string): Promise<PlanGaiaWorkUnitEntry[]> {
  const gaiaRoot = join(repoRoot, ".gaia");

  if (!(await pathExists(gaiaRoot))) {
    return [];
  }

  const entries = await readdir(gaiaRoot, { withFileTypes: true });
  const discovered = await Promise.all(
    entries.map(async (entry) => {
      if (!entry.isDirectory()) {
        return null;
      }

      const workUnit = entry.name;
      if (RESERVED_GAIA_DIRECTORIES.has(workUnit) || !WORK_UNIT_PATTERN.test(workUnit)) {
        return null;
      }

      const baseDir = join(gaiaRoot, workUnit);
      const hasArtifacts = await Promise.all(
        WORK_UNIT_ARTIFACT_FILES.map(async (fileName) => pathExists(join(baseDir, fileName))),
      );

      if (hasArtifacts.some((present) => !present)) {
        return null;
      }

      return {
        workUnit,
        baseDir,
        modifiedAtMs: await resolveWorkUnitModifiedAtMs(baseDir),
      } satisfies PlanGaiaWorkUnitEntry;
    }),
  );

  return discovered
    .filter((entry): entry is PlanGaiaWorkUnitEntry => entry !== null)
    .sort((left, right) => {
      if (right.modifiedAtMs !== left.modifiedAtMs) {
        return right.modifiedAtMs - left.modifiedAtMs;
      }

      return right.workUnit.localeCompare(left.workUnit);
    });
}

function formatArchiveSuffix(now: Date): string {
  return now.toISOString().replaceAll(":", "-").replaceAll(".", "-");
}

async function resolveArchivePath(archiveRoot: string, workUnit: string, suffix: string): Promise<string> {
  let index = 0;

  while (true) {
    const candidateName = index === 0 ? `${workUnit}--${suffix}` : `${workUnit}--${suffix}-${index}`;
    const candidatePath = join(archiveRoot, candidateName);
    if (!(await pathExists(candidatePath))) {
      return candidatePath;
    }

    index += 1;
  }
}

export async function prunePlanGaiaWorkUnits(
  args: PrunePlanGaiaWorkUnitsArgs,
): Promise<PrunePlanGaiaWorkUnitsResult> {
  if (!Number.isInteger(args.keepLatest) || args.keepLatest < 1) {
    throw new Error("keepLatest must be a positive integer");
  }

  const keepSet = new Set<string>();
  for (const workUnit of args.keepWorkUnits ?? []) {
    validateWorkUnit(workUnit);
    keepSet.add(workUnit);
  }

  const workUnits = await listPlanGaiaWorkUnits(args.repoRoot);
  if (workUnits.length === 0) {
    return {
      scanned: 0,
      kept: [],
      archived: [],
    };
  }

  const kept: string[] = [];
  const toArchive: PlanGaiaWorkUnitEntry[] = [];
  for (const entry of workUnits) {
    if (keepSet.has(entry.workUnit) || kept.length < args.keepLatest) {
      kept.push(entry.workUnit);
      continue;
    }

    toArchive.push(entry);
  }

  const archived: string[] = [];
  if (toArchive.length > 0) {
    const archiveRoot = join(args.repoRoot, ".gaia", DEFAULT_ARCHIVE_SUBDIR);
    await mkdir(archiveRoot, { recursive: true });

    const suffix = formatArchiveSuffix(args.now ?? new Date());
    for (const entry of toArchive) {
      const archivePath = await resolveArchivePath(archiveRoot, entry.workUnit, suffix);
      await rename(entry.baseDir, archivePath);
      archived.push(entry.workUnit);
    }
  }

  return {
    scanned: workUnits.length,
    kept,
    archived,
  };
}
