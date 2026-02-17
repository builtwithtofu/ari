import { relative } from "node:path";

import { collectResults, type CollectResultsSummary } from "../tools/collect-results.js";
import { delegateGaia, type DelegateGaiaResult } from "../tools/delegate-gaia.js";
import {
  prunePlanGaiaWorkUnits,
  type PlanGaiaPaths,
  writePlanGaia,
} from "../tools/plan-gaia.js";
import { assertMutationAllowed, type GaiaMode } from "../shared/mode.js";
import { evaluatePlanGate, type PlanRiskLevel } from "./plan-gates.js";
import { appendRuntimeJournalEvent } from "./runtime-journal.js";
import { refreshSessionRuntimeState } from "./session-state.js";
import type { StreamVcsContext } from "./streams.js";
import type { LeanAgentKey } from "../agents/types.js";

const DEFAULT_MAX_WORK_UNITS = 20;

export interface ProcessWorkUnitArgs<TParsed> {
  repoRoot: string;
  mode?: GaiaMode;
  workUnit: string;
  sessionId: string;
  modelUsed: string;
  responseText: string;
  parse: (input: unknown) => TParsed;
  retry?: () => Promise<string>;
  plan: string;
  log: string;
  decisions: string;
  maxWorkUnits?: number;
  riskLevel?: PlanRiskLevel;
  operatorApproved?: boolean;
  streamId?: string;
  vcsContext?: StreamVcsContext;
  delegatedAgent?: LeanAgentKey;
}

export interface ProcessWorkUnitResult<TParsed> {
  delegation: DelegateGaiaResult<TParsed>;
  collection: CollectResultsSummary<TParsed>;
  paths: PlanGaiaPaths;
}

function toRepoRelativePath(repoRoot: string, absolutePath: string): string {
  return relative(repoRoot, absolutePath).split("\\").join("/");
}

export async function processWorkUnit<TParsed>(
  args: ProcessWorkUnitArgs<TParsed>,
): Promise<ProcessWorkUnitResult<TParsed>> {
  assertMutationAllowed(args.mode, "plan_gaia writes");

  const riskLevel = args.riskLevel ?? "low";
  const operatorApproved = args.operatorApproved ?? false;
  const streamId = args.streamId ?? "default";
  const gateDecision = evaluatePlanGate({
    riskLevel,
    operatorApproved,
  });

  await appendRuntimeJournalEvent({
    repoRoot: args.repoRoot,
    event: {
      event_version: "1.0",
      event_type: "plan_gate_evaluated",
      timestamp: new Date().toISOString(),
      session_id: args.sessionId,
      work_unit: args.workUnit,
      stream_id: streamId,
      ...(args.vcsContext ? { vcs_context: args.vcsContext } : {}),
      risk_level: riskLevel,
      operator_approved: operatorApproved,
      allowed: gateDecision.allowed,
      gate: gateDecision.gate,
      reason: gateDecision.reason,
    },
  });

  if (!gateDecision.allowed) {
    await refreshSessionRuntimeState({
      repoRoot: args.repoRoot,
      sessionId: args.sessionId,
    });

    throw new Error(gateDecision.reason);
  }

  const delegateArgs = {
    sessionId: args.sessionId,
    modelUsed: args.modelUsed,
    responseText: args.responseText,
    parse: args.parse,
    ...(args.retry ? { retry: args.retry } : {}),
  };

  const delegation = await delegateGaia({
    ...delegateArgs,
  });

  await appendRuntimeJournalEvent({
    repoRoot: args.repoRoot,
    event: {
      event_version: "1.0",
      event_type: "delegation_completed",
      timestamp: new Date().toISOString(),
      session_id: args.sessionId,
      work_unit: args.workUnit,
      stream_id: streamId,
      ...(args.vcsContext ? { vcs_context: args.vcsContext } : {}),
      status: delegation.status,
      attempt_count: delegation.attempt_count,
      model_used: delegation.model_used,
      parse_error: delegation.parse_error,
    },
  });

  if (delegation.rejection_feedback_request) {
    await appendRuntimeJournalEvent({
      repoRoot: args.repoRoot,
      event: {
        event_version: "1.0",
        event_type: "rejection_feedback_requested",
        timestamp: new Date().toISOString(),
        session_id: args.sessionId,
        work_unit: args.workUnit,
        stream_id: streamId,
        ...(args.vcsContext ? { vcs_context: args.vcsContext } : {}),
        owner_agent: delegation.rejection_feedback_request.owner_agent,
        paused_agent: args.delegatedAgent ?? "unknown",
        question: delegation.rejection_feedback_request.question,
        reason: delegation.rejection_feedback_request.reason,
      },
    });
  }

  const collection = collectResults([delegation]);
  const paths = await writePlanGaia({
    repoRoot: args.repoRoot,
    workUnit: args.workUnit,
    plan: args.plan,
    log: args.log,
    decisions: args.decisions,
  });

  await appendRuntimeJournalEvent({
    repoRoot: args.repoRoot,
    event: {
      event_version: "1.0",
      event_type: "artifacts_written",
      timestamp: new Date().toISOString(),
      session_id: args.sessionId,
      work_unit: args.workUnit,
      stream_id: streamId,
      ...(args.vcsContext ? { vcs_context: args.vcsContext } : {}),
      plan_path: toRepoRelativePath(args.repoRoot, paths.plan_path),
      log_path: toRepoRelativePath(args.repoRoot, paths.log_path),
      decisions_path: toRepoRelativePath(args.repoRoot, paths.decisions_path),
    },
  });

  const prune = await prunePlanGaiaWorkUnits({
    repoRoot: args.repoRoot,
    keepLatest: args.maxWorkUnits ?? DEFAULT_MAX_WORK_UNITS,
    keepWorkUnits: [args.workUnit],
  });

  await appendRuntimeJournalEvent({
    repoRoot: args.repoRoot,
    event: {
      event_version: "1.0",
      event_type: "work_units_pruned",
      timestamp: new Date().toISOString(),
      session_id: args.sessionId,
      work_unit: args.workUnit,
      stream_id: streamId,
      ...(args.vcsContext ? { vcs_context: args.vcsContext } : {}),
      archived_work_units: prune.archived,
    },
  });

  await refreshSessionRuntimeState({
    repoRoot: args.repoRoot,
    sessionId: args.sessionId,
  });

  return {
    delegation,
    collection,
    paths,
  };
}
