import { mkdir, writeFile } from "node:fs/promises";
import { join } from "node:path";

import type { DelegateGaiaStatus } from "../tools/delegate-gaia.js";
import type { PlanGateState, PlanRiskLevel } from "./plan-gates.js";

export interface ActivePlanArtifactPaths {
  plan_path: string;
  log_path: string;
  decisions_path: string;
}

export interface ActivePlanState {
  session_id: string;
  work_unit: string;
  stream_id: string;
  risk_level: PlanRiskLevel;
  gate: PlanGateState;
  allowed: boolean;
  status: DelegateGaiaStatus;
  updated_at: string;
  artifact_paths: ActivePlanArtifactPaths;
}

export interface WriteActivePlanStateArgs {
  repoRoot: string;
  sessionId: string;
  workUnit: string;
  streamId: string;
  riskLevel: PlanRiskLevel;
  gate: PlanGateState;
  allowed: boolean;
  status: DelegateGaiaStatus;
  paths: ActivePlanArtifactPaths;
  now?: Date;
}

export interface WriteActivePlanStateResult {
  path: string;
  state: ActivePlanState;
}

function activePlanStatePath(repoRoot: string, sessionId: string): string {
  return join(repoRoot, ".gaia", "runtime", sessionId, "active-plan.json");
}

export async function writeActivePlanState(args: WriteActivePlanStateArgs): Promise<WriteActivePlanStateResult> {
  const state: ActivePlanState = {
    session_id: args.sessionId,
    work_unit: args.workUnit,
    stream_id: args.streamId,
    risk_level: args.riskLevel,
    gate: args.gate,
    allowed: args.allowed,
    status: args.status,
    updated_at: (args.now ?? new Date()).toISOString(),
    artifact_paths: args.paths,
  };

  const path = activePlanStatePath(args.repoRoot, args.sessionId);
  await mkdir(join(args.repoRoot, ".gaia", "runtime", args.sessionId), { recursive: true });
  await writeFile(path, `${JSON.stringify(state, null, 2)}\n`, "utf8");

  return {
    path,
    state,
  };
}
