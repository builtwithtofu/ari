import { getAgentPrompt } from "../agents/prompts.js";
import type { LeanAgentKey } from "../agents/types.js";
import { AGENT_DEFAULTS } from "./defaults.js";

const LEAN_AGENT_KEYS: readonly LeanAgentKey[] = ["gaia", "athena", "hephaestus", "demeter"];

const LEAN_AGENT_MODES: Record<LeanAgentKey, "primary" | "subagent"> = {
  gaia: "primary",
  athena: "subagent",
  hephaestus: "subagent",
  demeter: "subagent",
};

const LEAN_AGENT_DESCRIPTIONS: Record<LeanAgentKey, string> = {
  gaia: "GAIA orchestrates human-in-the-loop product and engineering work units.",
  athena: "ATHENA maps repository reality and routes next actions.",
  hephaestus: "HEPHAESTUS implements scoped changes with TDD discipline.",
  demeter: "DEMETER captures decisions, logs, and durable project memory.",
};

export const GAIA_SLASH_COMMAND_NAME = "gaia-init";
export const GAIA_AGENT_MODEL_OVERRIDE_ENV = "OPENCODE_GAIA_AGENT_MODEL";

const GAIA_SLASH_COMMAND_DEFAULT = {
  template:
    "Run the gaia_init tool now with refresh=false unless the user explicitly asks to refresh.",
  description: "Create or refresh .gaia/gaia-init.md using GAIA init defaults.",
  agent: "gaia",
} as const;

type UnknownRecord = Record<string, unknown>;

function isRecord(value: unknown): value is UnknownRecord {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function toRecord(value: unknown): UnknownRecord {
  if (!isRecord(value)) {
    return {};
  }

  return value;
}

function mergeRecord(base: UnknownRecord, override: unknown): UnknownRecord {
  return {
    ...base,
    ...toRecord(override),
  };
}

function buildAgentDefaults(agent: LeanAgentKey): UnknownRecord {
  const modelDefaults = AGENT_DEFAULTS[agent];
  const base: UnknownRecord = {
    model: modelDefaults.model,
    fallback: modelDefaults.fallback,
    temperature: modelDefaults.temperature,
    prompt: getAgentPrompt(agent),
    mode: LEAN_AGENT_MODES[agent],
    description: LEAN_AGENT_DESCRIPTIONS[agent],
  };

  if (modelDefaults.reasoningEffort) {
    base.reasoningEffort = modelDefaults.reasoningEffort;
  }

  if (agent === "gaia") {
    base.permission = {
      read: "allow",
      edit: "deny",
      bash: "deny",
      gaia_init: "allow",
      delegate_gaia: "allow",
      question: "allow",
      task: {
        "*": "deny",
        athena: "allow",
        hephaestus: "allow",
        demeter: "allow",
      },
    };
  }

  if (agent !== "gaia") {
    base.hidden = true;
  }

  return base;
}

function mergeAgentDefaults(agent: LeanAgentKey, existing: unknown): UnknownRecord {
  const defaults = buildAgentDefaults(agent);
  const merged = mergeRecord(defaults, existing);
  const mergedPermission = mergeRecord(toRecord(defaults.permission), toRecord(toRecord(existing).permission));

  if (Object.keys(mergedPermission).length > 0) {
    merged.permission = mergedPermission;
  }

  return merged;
}

function readAgentModelOverride(): string | undefined {
  const raw = process.env[GAIA_AGENT_MODEL_OVERRIDE_ENV];
  if (!raw) {
    return undefined;
  }

  const trimmed = raw.trim();
  return trimmed.length > 0 ? trimmed : undefined;
}

export function applyGaiaRuntimeConfig(configInput: unknown): void {
  if (!isRecord(configInput)) {
    return;
  }

  const config = configInput;
  const existingAgents = toRecord(config.agent);
  const nextAgents: UnknownRecord = {
    ...existingAgents,
  };
  const modelOverride = readAgentModelOverride();

  for (const agent of LEAN_AGENT_KEYS) {
    const merged = mergeAgentDefaults(agent, existingAgents[agent]);
    if (modelOverride) {
      merged.model = modelOverride;
    }

    nextAgents[agent] = merged;
  }

  config.agent = nextAgents;

  const existingCommands = toRecord(config.command);
  const nextCommands: UnknownRecord = {
    ...existingCommands,
  };

  nextCommands[GAIA_SLASH_COMMAND_NAME] = mergeRecord(
    GAIA_SLASH_COMMAND_DEFAULT as unknown as UnknownRecord,
    existingCommands[GAIA_SLASH_COMMAND_NAME],
  );

  config.command = nextCommands;
}
