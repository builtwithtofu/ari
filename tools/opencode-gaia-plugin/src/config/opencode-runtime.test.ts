import { describe, expect, test } from "bun:test";

import {
  applyGaiaRuntimeConfig,
  GAIA_AGENT_MODEL_OVERRIDE_ENV,
  GAIA_SLASH_COMMAND_NAME,
} from "./opencode-runtime";

function asRecord(value: unknown): Record<string, unknown> {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    throw new Error("Expected object record");
  }

  return value as Record<string, unknown>;
}

describe("applyGaiaRuntimeConfig", () => {
  test("injects GAIA agents and slash command defaults", () => {
    const config: Record<string, unknown> = {};

    applyGaiaRuntimeConfig(config);

    const agents = asRecord(config.agent);
    const commands = asRecord(config.command);

    const gaia = asRecord(agents.gaia);
    const athena = asRecord(agents.athena);
    const hephaestus = asRecord(agents.hephaestus);
    const demeter = asRecord(agents.demeter);
    const gaiaInitCommand = asRecord(commands[GAIA_SLASH_COMMAND_NAME]);

    expect(gaia.mode).toBe("primary");
    expect(athena.mode).toBe("subagent");
    expect(hephaestus.mode).toBe("subagent");
    expect(demeter.mode).toBe("subagent");
    expect(athena.hidden).toBe(true);
    expect(hephaestus.hidden).toBe(true);
    expect(demeter.hidden).toBe(true);
    expect(typeof gaia.prompt).toBe("string");
    expect(typeof gaia.model).toBe("string");

    const gaiaPermission = asRecord(gaia.permission);
    const gaiaTaskPermission = asRecord(gaiaPermission.task);

    expect(gaiaPermission.edit).toBe("deny");
    expect(gaiaPermission.bash).toBe("deny");
    expect(gaiaTaskPermission["*"]).toBe("deny");
    expect(gaiaTaskPermission.athena).toBe("allow");
    expect(gaiaTaskPermission.hephaestus).toBe("allow");
    expect(gaiaTaskPermission.demeter).toBe("allow");

    expect(gaiaInitCommand.template).toBe(
      "Run the gaia_init tool now with refresh=false unless the user explicitly asks to refresh.",
    );
    expect(gaiaInitCommand.agent).toBe("gaia");
  });

  test("preserves explicit user overrides for GAIA agent and slash command", () => {
    const config: Record<string, unknown> = {
      agent: {
        gaia: {
          model: "custom/model",
          prompt: "custom prompt",
          mode: "primary",
          permission: {
            gaia_init: "deny",
            edit: "allow",
            task: {
              "*": "allow",
            },
          },
        },
      },
      command: {
        [GAIA_SLASH_COMMAND_NAME]: {
          template: "custom template",
          description: "custom description",
          agent: "build",
        },
      },
    };

    applyGaiaRuntimeConfig(config);

    const agents = asRecord(config.agent);
    const commands = asRecord(config.command);
    const gaia = asRecord(agents.gaia);
    const permission = asRecord(gaia.permission);
    const gaiaInitCommand = asRecord(commands[GAIA_SLASH_COMMAND_NAME]);

    expect(gaia.model).toBe("custom/model");
    expect(gaia.prompt).toBe("custom prompt");
    expect(permission.gaia_init).toBe("deny");
    expect(permission.edit).toBe("allow");
    expect(asRecord(permission.task)["*"]).toBe("allow");
    expect(gaiaInitCommand.template).toBe("custom template");
    expect(gaiaInitCommand.agent).toBe("build");
  });

  test("applies global model override to GAIA and subagents", () => {
    const original = process.env[GAIA_AGENT_MODEL_OVERRIDE_ENV];
    process.env[GAIA_AGENT_MODEL_OVERRIDE_ENV] = "openai/gpt-5.3-codex";

    try {
      const config: Record<string, unknown> = {
        agent: {
          gaia: {
            model: "custom/model",
          },
        },
      };

      applyGaiaRuntimeConfig(config);

      const agents = asRecord(config.agent);
      expect(asRecord(agents.gaia).model).toBe("openai/gpt-5.3-codex");
      expect(asRecord(agents.athena).model).toBe("openai/gpt-5.3-codex");
      expect(asRecord(agents.hephaestus).model).toBe("openai/gpt-5.3-codex");
      expect(asRecord(agents.demeter).model).toBe("openai/gpt-5.3-codex");
    } finally {
      if (original === undefined) {
        delete process.env[GAIA_AGENT_MODEL_OVERRIDE_ENV];
      } else {
        process.env[GAIA_AGENT_MODEL_OVERRIDE_ENV] = original;
      }
    }
  });
});
