import type { AgentKey, AgentModelConfig } from "../agents/types.js";

export const AGENT_DEFAULTS: Record<AgentKey, AgentModelConfig> = {
  gaia: {
    model: "opencode/glm-5-free",
    fallback: ["opencode/glm-5-free"],
    temperature: 0.2,
  },
  athena: {
    model: "opencode/glm-5-free",
    fallback: ["opencode/glm-5-free"],
    temperature: 0.1,
  },
  hephaestus: {
    model: "opencode/glm-5-free",
    fallback: ["opencode/glm-5-free"],
    temperature: 0.1,
  },
  demeter: {
    model: "opencode/glm-5-free",
    fallback: ["opencode/glm-5-free"],
    temperature: 0.1,
  },
};
