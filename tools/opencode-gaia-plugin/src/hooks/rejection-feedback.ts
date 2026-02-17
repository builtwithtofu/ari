import type { DecisionCaptureEntry } from "./decision-capture.js";

export function buildRejectionPrefill(toolName: string): string {
  const normalizedTool = toolName.trim();
  return `Rejected ${normalizedTool} because: `;
}

export function buildRejectionFollowupQuestion(toolName: string): string {
  const normalizedTool = toolName.trim();
  return `What should GAIA change after you rejected ${normalizedTool}?`;
}

export interface BuildRejectionDecisionEntryArgs {
  toolName: string;
  answer: string;
  impact: string;
  rationale?: string;
}

export function buildRejectionDecisionEntry(
  args: BuildRejectionDecisionEntryArgs,
): DecisionCaptureEntry {
  const answer = args.answer.trim();
  const impact = args.impact.trim();

  return {
    type: "rejection",
    question: buildRejectionFollowupQuestion(args.toolName),
    answer,
    ...(args.rationale ? { rationale: args.rationale.trim() } : {}),
    impact,
  };
}
