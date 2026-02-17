import { describe, expect, test } from "bun:test";

import { evaluateGaiaPromptQuality } from "./prompt-quality";

describe("evaluateGaiaPromptQuality", () => {
  test("passes when all required guardrails are present", () => {
    const prompt = [
      "Do not edit or write files directly.",
      "Never call edit or write tools directly.",
      "Treat permission-denied responses as policy signals, not transient failures.",
      "Do not retry blocked mutation actions.",
      "When code or file mutation is needed, delegate implementation to HEPHAESTUS.",
    ].join("\n");

    const result = evaluateGaiaPromptQuality(prompt);
    expect(result.ok).toBe(true);
    expect(result.checks.every((check) => check.ok)).toBe(true);
  });

  test("fails with actionable missing checks", () => {
    const result = evaluateGaiaPromptQuality("Do not edit or write files directly.");
    expect(result.ok).toBe(false);
    expect(result.checks.filter((check) => !check.ok).map((check) => check.name)).toEqual([
      "direct-tool-block",
      "permission-denied-guidance",
      "no-retry-blocked-mutations",
      "delegate-to-hephaestus",
    ]);
  });
});
