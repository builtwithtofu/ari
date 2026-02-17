import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { resolve } from "node:path";

import { bootstrapSandbox } from "./bootstrap.js";
import type { ExecFn } from "./exec.js";
import { fileExists } from "./fs.js";
import { createManualWorkspace, type ManualTuiArgs } from "./manual-session.js";
import { runOpenCode } from "./opencode.js";
import { evaluateGaiaPromptQuality } from "./prompt-quality.js";
import { runHarnessDoctor } from "./preflight.js";
import {
  buildSuiteSteps,
  getBugHarnessPermission,
  getSmokePermission,
  type SuiteMode,
} from "./plans.js";

const DEFAULT_MODEL = "opencode/glm-5-free";
const DEFAULT_BUG_REPORT = "doc/bug-report.example.md";
const DEFAULT_SMOKE_PROMPT =
  "Verify sandbox setup, list relevant files, and suggest one next coding unit.";
const DEFAULT_LIST_TIMEOUT_MS = 60_000;
const DEFAULT_SMOKE_TIMEOUT_MS = 180_000;
const DEFAULT_GAIA_INIT_TIMEOUT_MS = 180_000;
const DEFAULT_BUG_TIMEOUT_MS = 600_000;
const DEFAULT_HEARTBEAT_MS = 10_000;

function selectedModel(envName: string): string {
  return process.env[envName] ?? DEFAULT_MODEL;
}

function getPort(): string {
  return process.env.OPENCODE_PORT ?? "4096";
}

function parseTimeoutMs(value: string | undefined): number | undefined {
  if (!value) {
    return undefined;
  }

  const parsed = Number.parseInt(value, 10);
  if (!Number.isFinite(parsed) || parsed <= 0) {
    return undefined;
  }

  return parsed;
}

function timeoutMsFromEnv(envName: string, fallback: number): number {
  return parseTimeoutMs(process.env[envName]) ?? fallback;
}

function heartbeatMsFromEnv(envName: string): number {
  return parseTimeoutMs(process.env[envName])
    ?? parseTimeoutMs(process.env.OPENCODE_HEARTBEAT_MS)
    ?? DEFAULT_HEARTBEAT_MS;
}

function resolveBugReportPath(repoRoot: string, value: string | undefined): string {
  return resolve(repoRoot, value ?? DEFAULT_BUG_REPORT);
}

export interface CommandContext {
  repoRoot: string;
  exec?: ExecFn;
}

interface GaiaInitRunner {
  runGaiaInit: (args: { repoRoot: string; mode?: "supervised" | "autopilot" | "locked" }) => Promise<unknown>;
}

interface GaiaPromptProvider {
  LEAN_AGENT_PROMPTS: {
    gaia: string;
  };
}

function asRecord(value: unknown): Record<string, unknown> {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    throw new Error("Expected object record");
  }

  return value as Record<string, unknown>;
}

function withExec(exec: ExecFn | undefined): { exec?: ExecFn } {
  return exec ? { exec } : {};
}

function withTimeout(timeoutMs: number | undefined): { timeoutMs?: number } {
  return timeoutMs !== undefined ? { timeoutMs } : {};
}

export async function commandBootstrap(context: CommandContext): Promise<void> {
  await bootstrapSandbox({
    repoRoot: context.repoRoot,
    ...withExec(context.exec),
  });
}

export async function commandDoctor(context: CommandContext): Promise<void> {
  const result = await runHarnessDoctor({
    repoRoot: context.repoRoot,
    ...withExec(context.exec),
  });

  for (const check of result.checks) {
    const state = check.ok ? "ok" : "fail";
    console.log(`[${state}] ${check.name}: ${check.detail}`);
  }

  if (!result.ok) {
    throw new Error("Harness preflight failed; fix checks above and retry");
  }
}

export async function commandOpenCode(
  context: CommandContext,
  args: string[],
): Promise<void> {
  await runOpenCode({
    repoRoot: context.repoRoot,
    args,
    stdio: "inherit",
    ...withTimeout(parseTimeoutMs(process.env.OPENCODE_TIMEOUT_MS)),
    ...withExec(context.exec),
  });
}

export async function commandManualTui(context: CommandContext, args: ManualTuiArgs): Promise<void> {
  const workspace = await createManualWorkspace({
    repoRoot: context.repoRoot,
    ...(args.label ? { label: args.label } : {}),
  });

  console.log(`manual-tui workspace: ${workspace.workspacePath}`);
  if (args.model) {
    console.log(`manual-tui model override: ${args.model}`);
  }
  console.log("Launching OpenCode TUI in sandboxed mode...");

  await runOpenCode({
    repoRoot: context.repoRoot,
    cwd: workspace.workspacePath,
    args: args.model ? ["--model", args.model] : [],
    stdio: "inherit",
    ...(args.model ? { envOverrides: { OPENCODE_GAIA_AGENT_MODEL: args.model } } : {}),
    ...withExec(context.exec),
  });
}

export async function commandListFreeModels(context: CommandContext): Promise<void> {
  const result = await runOpenCode({
    repoRoot: context.repoRoot,
    args: ["models"],
    stdio: "pipe",
    timeoutMs: timeoutMsFromEnv("OPENCODE_LIST_TIMEOUT_MS", DEFAULT_LIST_TIMEOUT_MS),
    ...withExec(context.exec),
  });

  const lines = result.stdout
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter((line) => line.includes("-free"));

  for (const line of lines) {
    console.log(line);
  }
}

export async function commandSmoke(context: CommandContext, prompt?: string): Promise<void> {
  await runOpenCode({
    repoRoot: context.repoRoot,
    args: [
      "run",
      "--agent",
      "build",
      "--model",
      selectedModel("OPENCODE_SMOKE_MODEL"),
      prompt ?? DEFAULT_SMOKE_PROMPT,
    ],
    stdio: "inherit",
    timeoutMs: timeoutMsFromEnv("OPENCODE_SMOKE_TIMEOUT_MS", DEFAULT_SMOKE_TIMEOUT_MS),
    ...withTimeout(parseTimeoutMs(process.env.OPENCODE_SMOKE_IDLE_TIMEOUT_MS)),
    heartbeatMs: heartbeatMsFromEnv("OPENCODE_SMOKE_HEARTBEAT_MS"),
    heartbeatLabel: "smoke",
    envOverrides: {
      OPENCODE_PERMISSION: process.env.OPENCODE_PERMISSION ?? getSmokePermission(),
    },
    ...withExec(context.exec),
  });
}

export async function commandBug(context: CommandContext, bugReport?: string): Promise<void> {
  const reportPath = resolveBugReportPath(context.repoRoot, bugReport);
  if (!(await fileExists(reportPath))) {
    throw new Error(`Bug report file not found: ${reportPath}`);
  }

  const prompt =
    "You are running a bug reproduction harness. Read the attached bug report and follow this flow exactly: " +
    "(1) write a failing reproducer test using real values and exact assertions, " +
    "(2) implement the minimal fix, (3) run tests, (4) summarize why the bug is now covered against regression. " +
    "Keep tests low-mock and low-orchestration.";

  await runOpenCode({
    repoRoot: context.repoRoot,
    args: [
      "run",
      "--agent",
      "build",
      "--model",
      selectedModel("OPENCODE_HARNESS_MODEL"),
      "-f",
      reportPath,
      prompt,
    ],
    stdio: "inherit",
    timeoutMs: timeoutMsFromEnv("OPENCODE_BUG_TIMEOUT_MS", DEFAULT_BUG_TIMEOUT_MS),
    ...withTimeout(parseTimeoutMs(process.env.OPENCODE_BUG_IDLE_TIMEOUT_MS)),
    heartbeatMs: heartbeatMsFromEnv("OPENCODE_BUG_HEARTBEAT_MS"),
    heartbeatLabel: "bug-harness",
    envOverrides: {
      OPENCODE_PERMISSION: process.env.OPENCODE_PERMISSION ?? getBugHarnessPermission(),
    },
    ...withExec(context.exec),
  });
}

export async function commandGaiaInitSmoke(context: CommandContext): Promise<void> {
  await runOpenCode({
    repoRoot: context.repoRoot,
    args: [
      "run",
      "--agent",
      "build",
      "--model",
      selectedModel("OPENCODE_SMOKE_MODEL"),
      "Use the gaia_init tool now with refresh=false. Return only whether it succeeded.",
    ],
    stdio: "inherit",
    timeoutMs: timeoutMsFromEnv("OPENCODE_GAIA_INIT_TIMEOUT_MS", DEFAULT_GAIA_INIT_TIMEOUT_MS),
    ...withTimeout(parseTimeoutMs(process.env.OPENCODE_GAIA_INIT_IDLE_TIMEOUT_MS)),
    heartbeatMs: heartbeatMsFromEnv("OPENCODE_GAIA_INIT_HEARTBEAT_MS"),
    heartbeatLabel: "gaia-init-smoke",
    envOverrides: {
      OPENCODE_PERMISSION: process.env.OPENCODE_PERMISSION ?? getBugHarnessPermission(),
    },
    ...withExec(context.exec),
  });

  const initPath = resolve(context.repoRoot, ".gaia/gaia-init.md");
  if (!(await fileExists(initPath))) {
    throw new Error(`gaia_init smoke failed: ${initPath} not found`);
  }

  console.log(`gaia_init smoke succeeded: ${initPath}`);
}

export async function commandPromptQualitySmoke(context: CommandContext): Promise<void> {
  const promptsModulePath = new URL("../../opencode-gaia-plugin/src/agents/prompts.ts", import.meta.url).href;
  const promptsModule = (await import(promptsModulePath)) as GaiaPromptProvider;

  const result = evaluateGaiaPromptQuality(promptsModule.LEAN_AGENT_PROMPTS.gaia);
  for (const check of result.checks) {
    const state = check.ok ? "ok" : "fail";
    console.log(`[${state}] ${check.name}: ${check.detail}`);
  }

  if (!result.ok) {
    throw new Error("prompt-quality-smoke failed: GAIA prompt guardrails are incomplete");
  }

  console.log("prompt-quality-smoke succeeded: GAIA guardrails are present");
}

export async function commandLeanSubagentsSmoke(context: CommandContext): Promise<void> {
  const result = await runOpenCode({
    repoRoot: context.repoRoot,
    args: ["debug", "config"],
    stdio: "pipe",
    timeoutMs: timeoutMsFromEnv("OPENCODE_GAIA_INIT_TIMEOUT_MS", DEFAULT_GAIA_INIT_TIMEOUT_MS),
    ...withExec(context.exec),
  });

  const parsed = JSON.parse(result.stdout) as unknown;
  const config = asRecord(parsed);
  const agents = asRecord(config.agent);
  const gaia = asRecord(agents.gaia);
  const athena = asRecord(agents.athena);
  const hephaestus = asRecord(agents.hephaestus);
  const demeter = asRecord(agents.demeter);
  const gaiaPermission = asRecord(gaia.permission);
  const gaiaTaskPermission = asRecord(gaiaPermission.task);

  if (gaia.mode !== "primary") {
    throw new Error("lean-subagents-smoke failed: gaia mode is not primary");
  }

  if (gaiaPermission.edit !== "deny") {
    throw new Error("lean-subagents-smoke failed: gaia edit permission is not deny");
  }

  if (gaiaPermission.bash !== "deny") {
    throw new Error("lean-subagents-smoke failed: gaia bash permission is not deny");
  }

  if (
    gaiaTaskPermission["*"] !== "deny"
    || gaiaTaskPermission.athena !== "allow"
    || gaiaTaskPermission.hephaestus !== "allow"
    || gaiaTaskPermission.demeter !== "allow"
  ) {
    throw new Error("lean-subagents-smoke failed: gaia task permission allowlist is incorrect");
  }

  for (const [name, agent] of [
    ["athena", athena],
    ["hephaestus", hephaestus],
    ["demeter", demeter],
  ] as const) {
    if (agent.mode !== "subagent") {
      throw new Error(`lean-subagents-smoke failed: ${name} mode is not subagent`);
    }

    if (agent.hidden !== true) {
      throw new Error(`lean-subagents-smoke failed: ${name} is not hidden by default`);
    }
  }

  console.log("lean-subagents-smoke succeeded: GAIA primary with hidden lean specialists");
}

export async function commandLockedSmoke(context: CommandContext): Promise<void> {
  const tmpWorktree = await mkdtemp(resolve(tmpdir(), "gaia-locked-"));

  try {
    await mkdir(resolve(tmpWorktree, ".gaia"), { recursive: true });

    const gaiaModulePath = new URL("../../opencode-gaia-plugin/src/index.ts", import.meta.url).href;
    const gaiaModule = (await import(gaiaModulePath)) as GaiaInitRunner;

    await expectLockedError(async () => {
      await gaiaModule.runGaiaInit({
        repoRoot: tmpWorktree,
        mode: "locked",
      });
    });

    if (await fileExists(resolve(tmpWorktree, ".gaia/gaia-init.md"))) {
      throw new Error("locked smoke failed: gaia-init file was created in locked mode");
    }

    console.log("locked smoke succeeded: mutation blocked in locked mode");
  } finally {
    await rm(tmpWorktree, { recursive: true, force: true });
  }
}

async function expectLockedError(run: () => Promise<void>): Promise<void> {
  try {
    await run();
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    if (message.includes("Locked mode blocks gaia_init")) {
      return;
    }

    throw new Error(`locked smoke failed: unexpected error\n${message}`);
  }

  throw new Error("locked smoke failed: expected locked-mode refusal");
}

export async function commandServeWeb(context: CommandContext): Promise<void> {
  await commandBootstrap(context);

  await runOpenCode({
    repoRoot: context.repoRoot,
    args: ["web", "--hostname", "0.0.0.0", "--port", getPort()],
    stdio: "inherit",
    ...withExec(context.exec),
  });
}

export async function commandServeApi(context: CommandContext): Promise<void> {
  await commandBootstrap(context);

  await runOpenCode({
    repoRoot: context.repoRoot,
    args: ["serve", "--hostname", "0.0.0.0", "--port", getPort()],
    stdio: "inherit",
    ...withExec(context.exec),
  });
}

export async function commandSuite(
  context: CommandContext,
  mode: string,
  bugReport?: string,
): Promise<void> {
  const steps = buildSuiteSteps(mode);

  for (const step of steps) {
    switch (step) {
      case "doctor":
        await commandDoctor(context);
        break;
      case "bootstrap":
        await commandBootstrap(context);
        break;
      case "list-free-models":
        await commandListFreeModels(context);
        break;
      case "smoke":
        await commandSmoke(
          context,
          "Agentic smoke test: confirm sandbox context and list exactly 3 repository files.",
        );
        break;
      case "lean-subagents-smoke":
        await commandLeanSubagentsSmoke(context);
        break;
      case "gaia-init-smoke":
        await commandGaiaInitSmoke(context);
        break;
      case "prompt-quality-smoke":
        await commandPromptQualitySmoke(context);
        break;
      case "locked-smoke":
        await commandLockedSmoke(context);
        break;
      case "bug":
        await commandBug(context, bugReport);
        break;
      default: {
        const unreachable: never = step;
        throw new Error(`Unsupported suite step: ${unreachable}`);
      }
    }
  }
}

export function suiteModesHelp(): SuiteMode[] {
  return ["basic", "plugin", "quickstart", "quality", "locked", "bug", "full"];
}
