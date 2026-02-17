import { parseLeanAgentOutput } from "../agents/contracts.js";
import type { LeanAgentKey } from "../agents/types.js";
import type { GaiaMode } from "../shared/mode.js";

import {
  processWorkUnit,
  type ProcessWorkUnitResult,
} from "./process-work-unit.js";

export interface DelegateGaiaToolArtifacts {
  plan?: string;
  log?: string;
  decisions?: string;
}

export interface DelegateGaiaToolArgs {
  repoRoot: string;
  mode?: GaiaMode;
  workUnit: string;
  sessionId: string;
  modelUsed: string;
  agent: LeanAgentKey;
  responseText: string;
  retry?: () => Promise<string>;
  artifacts?: DelegateGaiaToolArtifacts;
}

const DEFAULT_ARTIFACTS = {
  plan: "# Plan\n",
  log: "# Log\n",
  decisions: "# Decisions\n",
} as const;

export async function runDelegateGaiaTool(
  args: DelegateGaiaToolArgs,
): Promise<ProcessWorkUnitResult<ReturnType<typeof parseLeanAgentOutput>>> {
  const artifacts = {
    ...DEFAULT_ARTIFACTS,
    ...(args.artifacts ?? {}),
  };

  const result = await processWorkUnit({
    repoRoot: args.repoRoot,
    ...(args.mode ? { mode: args.mode } : {}),
    workUnit: args.workUnit,
    sessionId: args.sessionId,
    modelUsed: args.modelUsed,
    responseText: args.responseText,
    parse: (input) => parseLeanAgentOutput(args.agent, input),
    ...(args.retry ? { retry: args.retry } : {}),
    plan: artifacts.plan,
    log: artifacts.log,
    decisions: artifacts.decisions,
    delegatedAgent: args.agent,
  });

  if (result.delegation.rejection_feedback_request) {
    result.delegation.rejection_feedback_request = {
      ...result.delegation.rejection_feedback_request,
      paused_agent: args.agent,
    };
  }

  return result;
}
