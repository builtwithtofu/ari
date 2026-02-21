import { tool, type Plugin } from "@opencode-ai/plugin";

import { loadGaiaConfig } from "../../../tools/opencode-gaia-plugin/src/config/loader.ts";
import {
  applyGaiaRuntimeConfig,
  runDelegateGaiaTool,
} from "../../../tools/opencode-gaia-plugin/src/index.ts";

const LEAN_AGENTS = ["gaia", "athena", "hephaestus", "demeter"] as const;

function resolveRepoRoot(context: { directory: string; worktree: string }): string {
  if (context.worktree && context.worktree !== "/") {
    return context.worktree;
  }

  return context.directory;
}

export const GaiaPlugin: Plugin = async () => {
  return {
    config: async (config) => {
      applyGaiaRuntimeConfig(config);
    },
    tool: {
      delegate_gaia: tool({
        description: "Parse delegated GAIA contract output and persist .gaia work-unit artifacts",
        args: {
          workUnit: tool.schema.string().min(1),
          sessionId: tool.schema.string().min(1),
          modelUsed: tool.schema.string().min(1),
          agent: tool.schema.enum(LEAN_AGENTS),
          responseText: tool.schema.string().min(1),
          retryResponseText: tool.schema.string().optional(),
          plan: tool.schema.string().optional(),
          log: tool.schema.string().optional(),
          decisions: tool.schema.string().optional(),
        },
        async execute(args, context) {
          const repoRoot = resolveRepoRoot(context);
          const config = await loadGaiaConfig({ repoRoot });

          const result = await runDelegateGaiaTool({
            repoRoot,
            mode: config.mode,
            workUnit: args.workUnit,
            sessionId: args.sessionId,
            modelUsed: args.modelUsed,
            agent: args.agent,
            responseText: args.responseText,
            ...(args.retryResponseText
              ? { retry: async () => args.retryResponseText as string }
              : {}),
            artifacts: {
              ...(args.plan ? { plan: args.plan } : {}),
              ...(args.log ? { log: args.log } : {}),
              ...(args.decisions ? { decisions: args.decisions } : {}),
            },
          });

          return JSON.stringify(result, null, 2);
        },
      }),
    },
  };
};
