import { describe, expect, test } from "bun:test";

import { getAgentPrompt, LEAN_AGENT_PROMPTS } from "./prompts";

describe("LEAN_AGENT_PROMPTS", () => {
  test("defines all lean prompts", () => {
    expect(Object.keys(LEAN_AGENT_PROMPTS).sort()).toEqual([
      "athena",
      "demeter",
      "gaia",
      "hephaestus",
    ]);
  });

  test("keeps prompts contract-oriented", () => {
    expect(LEAN_AGENT_PROMPTS.gaia).toContain("## Output Contract");
    expect(LEAN_AGENT_PROMPTS.athena).toContain("## Output Contract");
    expect(LEAN_AGENT_PROMPTS.hephaestus).toContain("## Output Contract");
    expect(LEAN_AGENT_PROMPTS.demeter).toContain("## Output Contract");
  });

  test("locks each prompt to reason, goal, and non-goals", () => {
    const prompts = Object.values(LEAN_AGENT_PROMPTS);

    for (const prompt of prompts) {
      expect(prompt).toContain("## Reason for Being");
      expect(prompt).toContain("## Primary Goal");
      expect(prompt).toContain("## Non-Goals");
    }
  });

  test("returns prompt by agent key", () => {
    expect(getAgentPrompt("gaia")).toBe(LEAN_AGENT_PROMPTS.gaia);
    expect(getAgentPrompt("athena")).toBe(LEAN_AGENT_PROMPTS.athena);
    expect(getAgentPrompt("hephaestus")).toBe(LEAN_AGENT_PROMPTS.hephaestus);
    expect(getAgentPrompt("demeter")).toBe(LEAN_AGENT_PROMPTS.demeter);
  });

  test("enforces small-unit TDD and checkpoint guidance", () => {
    expect(LEAN_AGENT_PROMPTS.gaia).toContain("small, actionable working unit");
    expect(LEAN_AGENT_PROMPTS.gaia).toContain("checkpoint");
    expect(LEAN_AGENT_PROMPTS.gaia).toContain("stacked PR");
    expect(LEAN_AGENT_PROMPTS.gaia).toContain("outside GAIA");
    expect(LEAN_AGENT_PROMPTS.gaia).toContain("stack traces, logs, or repro steps");
    expect(LEAN_AGENT_PROMPTS.gaia).toContain("reproducer test");
    expect(LEAN_AGENT_PROMPTS.gaia).toContain("Operator");
    expect(LEAN_AGENT_PROMPTS.gaia).toContain("Owner");
    expect(LEAN_AGENT_PROMPTS.gaia).toContain("Context");
    expect(LEAN_AGENT_PROMPTS.gaia).toContain("Options");
    expect(LEAN_AGENT_PROMPTS.gaia).toContain("Recommendation");
    expect(LEAN_AGENT_PROMPTS.gaia).toContain("Action needed");
    expect(LEAN_AGENT_PROMPTS.gaia).toContain("Approve work unit");
    expect(LEAN_AGENT_PROMPTS.gaia).toContain("targeted questions");
    expect(LEAN_AGENT_PROMPTS.gaia).toContain("active-plan.json");
    expect(LEAN_AGENT_PROMPTS.gaia).toContain("not code-only");
    expect(LEAN_AGENT_PROMPTS.gaia).toContain("classify -> plan -> checkpoint -> delegate -> harvest");
    expect(LEAN_AGENT_PROMPTS.gaia).toContain("base GAIA mode");
    expect(LEAN_AGENT_PROMPTS.gaia).toContain("work_unit");
    expect(LEAN_AGENT_PROMPTS.gaia).toContain("done_when");
    expect(LEAN_AGENT_PROMPTS.gaia).toContain("Do not edit or write files directly");
    expect(LEAN_AGENT_PROMPTS.gaia).toContain("simple informational tasks");
    expect(LEAN_AGENT_PROMPTS.gaia).toContain("Never call edit or write tools directly");
    expect(LEAN_AGENT_PROMPTS.gaia).toContain("permission-denied");
    expect(LEAN_AGENT_PROMPTS.gaia).toContain("delegate implementation to HEPHAESTUS");
    expect(LEAN_AGENT_PROMPTS.gaia).toContain("Do not retry blocked mutation actions");
    expect(LEAN_AGENT_PROMPTS.gaia).toContain("rejection feedback");
    expect(LEAN_AGENT_PROMPTS.gaia).toContain("What should GAIA change");
    expect(LEAN_AGENT_PROMPTS.gaia).toContain("Only GAIA asks follow-up rejection questions");

    expect(LEAN_AGENT_PROMPTS.hephaestus).toContain("TDD cycle");
    expect(LEAN_AGENT_PROMPTS.hephaestus).toContain("failing test first");
    expect(LEAN_AGENT_PROMPTS.hephaestus).toContain("small, working increments");
    expect(LEAN_AGENT_PROMPTS.hephaestus).toContain("stacked PR");
    expect(LEAN_AGENT_PROMPTS.hephaestus).toContain("tests passing");
    expect(LEAN_AGENT_PROMPTS.hephaestus).toContain("review-ready");
    expect(LEAN_AGENT_PROMPTS.hephaestus).toContain("submission-ready");
    expect(LEAN_AGENT_PROMPTS.hephaestus).toContain("low-mock");
    expect(LEAN_AGENT_PROMPTS.hephaestus).toContain("low-orchestration");
    expect(LEAN_AGENT_PROMPTS.hephaestus).toContain("real values");
    expect(LEAN_AGENT_PROMPTS.hephaestus).toContain("exact assertions");
    expect(LEAN_AGENT_PROMPTS.hephaestus).toContain("partial-response assertions");
    expect(LEAN_AGENT_PROMPTS.hephaestus).toContain("Do not ask the Operator for rejection feedback");
    expect(LEAN_AGENT_PROMPTS.hephaestus).toContain("needs_gaia_feedback");

    expect(LEAN_AGENT_PROMPTS.athena).toContain("Do not ask the Operator for rejection feedback");
    expect(LEAN_AGENT_PROMPTS.athena).toContain("needs_gaia_feedback");

    expect(LEAN_AGENT_PROMPTS.demeter).toContain("status_report");
    expect(LEAN_AGENT_PROMPTS.demeter).toContain("active_work_units");
    expect(LEAN_AGENT_PROMPTS.demeter).toContain("completed_work_units");
    expect(LEAN_AGENT_PROMPTS.demeter).toContain("blocked_work_units");
    expect(LEAN_AGENT_PROMPTS.demeter).toContain("Do not ask the Operator for rejection feedback");
    expect(LEAN_AGENT_PROMPTS.demeter).toContain("needs_gaia_feedback");
  });
});
