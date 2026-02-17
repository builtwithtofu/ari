import { describe, expect, test } from "bun:test";

import { evaluatePlanGate } from "./plan-gates";

describe("evaluatePlanGate", () => {
  test("allows low-risk work without approval", () => {
    const gate = evaluatePlanGate({
      riskLevel: "low",
      operatorApproved: false,
    });

    expect(gate.allowed).toBe(true);
    expect(gate.gate).toBe("auto_approved");
    expect(gate.reason).toContain("low-risk");
  });

  test("blocks medium/high risk work without approval", () => {
    const medium = evaluatePlanGate({
      riskLevel: "medium",
      operatorApproved: false,
    });
    const high = evaluatePlanGate({
      riskLevel: "high",
      operatorApproved: false,
    });

    expect(medium.allowed).toBe(false);
    expect(medium.gate).toBe("needs_operator_approval");
    expect(high.allowed).toBe(false);
    expect(high.gate).toBe("needs_operator_approval");
  });

  test("allows medium/high risk work when operator approval exists", () => {
    const gate = evaluatePlanGate({
      riskLevel: "high",
      operatorApproved: true,
    });

    expect(gate.allowed).toBe(true);
    expect(gate.gate).toBe("operator_approved");
    expect(gate.reason).toContain("Operator approval");
  });
});
