import { describe, expect, test } from "bun:test";

import { AGENT_DEFAULTS } from "../config/defaults";
import { resolveModel } from "./models";

describe("resolveModel", () => {
  test("prefers user override when available", () => {
    const resolved = resolveModel({
      agent: "hephaestus",
      defaults: AGENT_DEFAULTS,
      agentOverride: {
        model: "openai/gpt-5.1-codex",
      },
      availableModels: new Set([
        "opencode/glm-5-free",
        "opencode/big-pickle",
        "openai/gpt-5.1-codex",
      ]),
    });

    expect(resolved.model).toBe("openai/gpt-5.1-codex");
    expect(resolved.source).toBe("override");
  });

  test("falls back from unavailable override to default", () => {
    const resolved = resolveModel({
      agent: "hephaestus",
      defaults: AGENT_DEFAULTS,
      agentOverride: {
        model: "custom/not-present",
      },
      availableModels: new Set(["opencode/glm-5-free"]),
    });

    expect(resolved.model).toBe("opencode/glm-5-free");
    expect(resolved.source).toBe("default");
  });

  test("falls through fallback chain then system default", () => {
    const resolved = resolveModel({
      agent: "demeter",
      defaults: AGENT_DEFAULTS,
      availableModels: new Set(["openai/gpt-5.1-codex-mini"]),
      systemDefaultModel: "openai/gpt-5.1-codex-mini",
    });

    expect(resolved.model).toBe("openai/gpt-5.1-codex-mini");
    expect(resolved.source).toBe("system");
  });

  test("uses custom user model when added to availability", () => {
    const resolved = resolveModel({
      agent: "gaia",
      defaults: AGENT_DEFAULTS,
      agentOverride: {
        model: "opencode/big-pickle",
      },
      availableModels: new Set(["opencode/big-pickle"]),
    });

    expect(resolved.model).toBe("opencode/big-pickle");
    expect(resolved.source).toBe("override");
  });
});
