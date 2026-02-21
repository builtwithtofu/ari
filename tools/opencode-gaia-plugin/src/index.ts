import {
  processWorkUnit,
  type ProcessWorkUnitArgs,
  type ProcessWorkUnitResult,
} from "./runtime/process-work-unit.js";
import {
  runDelegateGaiaTool,
  type DelegateGaiaToolArgs,
  type DelegateGaiaToolArtifacts,
} from "./runtime/delegate-gaia-tool.js";

export const PLUGIN_NAME = "opencode-gaia-plugin";
export const PROJECT_PHASE = "pre-alpha";

export function getPluginBanner(): string {
  return `${PLUGIN_NAME} (${PROJECT_PHASE})`;
}

export async function runGaiaWorkUnit<TParsed>(
  args: ProcessWorkUnitArgs<TParsed>,
): Promise<ProcessWorkUnitResult<TParsed>> {
  return processWorkUnit(args);
}

export { processWorkUnit };
export { runDelegateGaiaTool };
export { applyGaiaRuntimeConfig } from "./config/opencode-runtime.js";
export {
  forkStream,
  openStream,
  setActiveStream,
  updateStreamProgress,
} from "./runtime/streams.js";
export {
  aggregateSessionRuntimeState,
  refreshSessionRuntimeState,
} from "./runtime/session-state.js";
export type { ProcessWorkUnitArgs, ProcessWorkUnitResult };
export type { DelegateGaiaToolArgs, DelegateGaiaToolArtifacts };
