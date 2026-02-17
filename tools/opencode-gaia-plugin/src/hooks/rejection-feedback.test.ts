import { describe, expect, test } from "bun:test";

import {
  buildRejectionDecisionEntry,
  buildRejectionFollowupQuestion,
  buildRejectionPrefill,
} from "./rejection-feedback";

describe("buildRejectionPrefill", () => {
  test("creates stable rejection prefill text", () => {
    expect(buildRejectionPrefill("bash")).toBe("Rejected bash because: ");
  });

  test("normalizes extra whitespace around tool name", () => {
    expect(buildRejectionPrefill("  edit  ")).toBe("Rejected edit because: ");
  });

  test("builds targeted rejection follow-up question", () => {
    expect(buildRejectionFollowupQuestion("write")).toBe(
      "What should GAIA change after you rejected write?",
    );
  });

  test("formats structured rejection decision capture entry", () => {
    const entry = buildRejectionDecisionEntry({
      toolName: "edit",
      answer: "Need smaller scoped changes",
      impact: "Split into two work units",
      rationale: "Current patch was too broad",
    });

    expect(entry.type).toBe("rejection");
    expect(entry.question).toBe("What should GAIA change after you rejected edit?");
    expect(entry.answer).toBe("Need smaller scoped changes");
    expect(entry.impact).toBe("Split into two work units");
    expect(entry.rationale).toBe("Current patch was too broad");
  });
});
