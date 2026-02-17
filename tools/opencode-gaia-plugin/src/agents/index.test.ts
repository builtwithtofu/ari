import { describe, expect, test } from "bun:test";

import { AGENT_DEFAULTS } from "../config/defaults";
import { parseGaiaConfig } from "../config/schema";
import { createLeanAgentRegistry, resolveOperationAgentKeys } from "./index";

describe("createLeanAgentRegistry", () => {
  test("builds registry for GAIA lean agents", () => {
    const registry = createLeanAgentRegistry({
      config: parseGaiaConfig({}),
      defaults: AGENT_DEFAULTS,
    });

    expect(Object.keys(registry).sort()).toEqual([
      "athena",
      "demeter",
      "gaia",
      "hephaestus",
    ]);

    expect(registry.gaia.modelConfig.model).toBe("opencode/glm-5-free");
    expect(registry.athena.modelConfig.model).toBe("opencode/glm-5-free");
  });

  test("merges model and prompt overrides", () => {
    const config = parseGaiaConfig({
      agents: {
        hephaestus: {
          model: "custom/model",
          temperature: 0.4,
          prompt_append: "Use migration-safe edits only.",
        },
      },
    });

    const registry = createLeanAgentRegistry({
      config,
      defaults: AGENT_DEFAULTS,
    });

    expect(registry.hephaestus.modelConfig.model).toBe("custom/model");
    expect(registry.hephaestus.modelConfig.temperature).toBe(0.4);
    expect(registry.hephaestus.prompt).toContain("Use migration-safe edits only.");
  });

  test("supports full prompt replacement", () => {
    const config = parseGaiaConfig({
      agents: {
        demeter: {
          prompt: "Custom DEMETER prompt",
          prompt_append: "Append this line",
        },
      },
    });

    const registry = createLeanAgentRegistry({
      config,
      defaults: AGENT_DEFAULTS,
    });

    expect(registry.demeter.prompt).toBe("Custom DEMETER prompt\n\nAppend this line");
  });

  test("propagates disabled flag", () => {
    const config = parseGaiaConfig({
      agents: {
        athena: {
          disabled: true,
        },
      },
    });

    const registry = createLeanAgentRegistry({
      config,
      defaults: AGENT_DEFAULTS,
    });

    expect(registry.athena.disabled).toBe(true);
  });
});

describe("resolveOperationAgentKeys", () => {
  test("returns lean set by default", () => {
    const keys = resolveOperationAgentKeys(parseGaiaConfig({}));

    expect(keys).toEqual(["gaia", "athena", "hephaestus", "demeter"]);
  });

  test("returns custom agent set for custom profile", () => {
    const keys = resolveOperationAgentKeys(
      parseGaiaConfig({
        operationProfile: {
          agentSet: "custom",
          customAgents: ["athena", "demeter"],
        },
      }),
    );

    expect(keys).toEqual(["gaia", "athena", "demeter"]);
  });

  test("returns gaia-only set when custom profile agent list is empty", () => {
    const keys = resolveOperationAgentKeys(
      parseGaiaConfig({
        operationProfile: {
          agentSet: "custom",
          customAgents: [],
        },
      }),
    );

    expect(keys).toEqual(["gaia"]);
  });
});
