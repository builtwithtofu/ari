import { describe, expect, test } from "bun:test";

import {
  buildPlanArtifactTemplateMarkdown,
  renderPlanArtifactMarkdown,
  validatePlanArtifact,
} from "./plan-artifact";

describe("plan artifact", () => {
  test("builds a template with required sections", () => {
    const template = buildPlanArtifactTemplateMarkdown();

    expect(template).toContain("## Objective");
    expect(template).toContain("## Constraints");
    expect(template).toContain("## Done When");
    expect(template).toContain("## Risk Level");
    expect(template).toContain("## Verification Steps");
  });

  test("validates artifact shape and risk level", () => {
    const artifact = validatePlanArtifact({
      objective: "Add plan gating to work units",
      constraints: ["Keep strict TypeScript", "Do not break locked mode"],
      done_when: ["Gate blocks medium/high risk without approval", "Tests pass"],
      risk_level: "medium",
      open_questions: ["Should medium require checkpoint by default?"],
      verification_steps: ["bun run typecheck", "bun test"],
      non_goals: ["Do not redesign all prompts"],
    });

    expect(artifact.risk_level).toBe("medium");
    expect(artifact.done_when).toHaveLength(2);
  });

  test("renders markdown from artifact", () => {
    const markdown = renderPlanArtifactMarkdown({
      objective: "Ship the next gate slice",
      constraints: ["Keep changes small"],
      done_when: ["Runtime tests pass"],
      risk_level: "low",
      open_questions: [],
      verification_steps: ["bun test"],
      non_goals: [],
    });

    expect(markdown).toContain("## Objective");
    expect(markdown).toContain("Ship the next gate slice");
    expect(markdown).toContain("## Risk Level");
    expect(markdown).toContain("low");
  });
});
