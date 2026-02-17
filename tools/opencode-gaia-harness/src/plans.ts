export type SuiteMode = "basic" | "plugin" | "quickstart" | "quality" | "locked" | "bug" | "full";
export type SuiteStep =
  | "doctor"
  | "bootstrap"
  | "list-free-models"
  | "smoke"
  | "lean-subagents-smoke"
  | "gaia-init-smoke"
  | "prompt-quality-smoke"
  | "locked-smoke"
  | "bug";

const SUITE_STEP_MAP: Record<SuiteMode, readonly SuiteStep[]> = {
  basic: ["bootstrap", "list-free-models", "smoke"],
  plugin: ["bootstrap", "lean-subagents-smoke", "gaia-init-smoke", "prompt-quality-smoke"],
  quickstart: ["doctor", "bootstrap", "lean-subagents-smoke", "gaia-init-smoke", "locked-smoke"],
  quality: ["prompt-quality-smoke"],
  locked: ["bootstrap", "locked-smoke"],
  bug: ["bootstrap", "bug"],
  full: [
    "bootstrap",
    "list-free-models",
    "smoke",
    "lean-subagents-smoke",
    "gaia-init-smoke",
    "prompt-quality-smoke",
    "locked-smoke",
    "bug",
  ],
};

export function buildSuiteSteps(mode: string): readonly SuiteStep[] {
  if (mode in SUITE_STEP_MAP) {
    return SUITE_STEP_MAP[mode as SuiteMode];
  }

  throw new Error(`Unknown harness mode: ${mode}`);
}

export function getSmokePermission(): string {
  return '{"bash":"allow","read":"allow","edit":"deny","write":"deny"}';
}

export function getBugHarnessPermission(): string {
  return '{"bash":"allow","read":"allow","edit":"allow","write":"allow"}';
}
