export type PlanRiskLevel = "low" | "medium" | "high";
export type PlanGateState = "auto_approved" | "operator_approved" | "needs_operator_approval";

export interface EvaluatePlanGateArgs {
  riskLevel: PlanRiskLevel;
  operatorApproved: boolean;
}

export interface PlanGateDecision {
  allowed: boolean;
  gate: PlanGateState;
  reason: string;
}

export function evaluatePlanGate(args: EvaluatePlanGateArgs): PlanGateDecision {
  if (args.riskLevel === "low") {
    return {
      allowed: true,
      gate: "auto_approved",
      reason: "Plan gate auto-approved for low-risk work.",
    };
  }

  if (args.operatorApproved) {
    return {
      allowed: true,
      gate: "operator_approved",
      reason: "Operator approval recorded for medium/high-risk work.",
    };
  }

  return {
    allowed: false,
    gate: "needs_operator_approval",
    reason: `Plan gate requires operator approval for ${args.riskLevel}-risk work.`,
  };
}
