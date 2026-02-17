import { resolve } from "node:path";

import type { ExecFn } from "./exec.js";
import { runExec } from "./exec.js";
import { fileExists } from "./fs.js";

export interface HarnessDoctorCheck {
  name: "plugin-template" | "bun-cli" | "opencode-cli";
  ok: boolean;
  detail: string;
}

export interface HarnessDoctorResult {
  ok: boolean;
  checks: HarnessDoctorCheck[];
}

export interface RunHarnessDoctorArgs {
  repoRoot: string;
  exec?: ExecFn;
}

async function checkCommandAvailability(
  exec: ExecFn,
  command: "bun" | "opencode",
): Promise<HarnessDoctorCheck> {
  const name = command === "bun" ? "bun-cli" : "opencode-cli";

  try {
    const result = await exec(command, ["--version"], {
      stdio: "pipe",
      allowFailure: true,
    });

    if (result.exitCode !== 0) {
      return {
        name,
        ok: false,
        detail: `${command} --version exited with ${result.exitCode}`,
      };
    }

    const version = result.stdout.trim() || result.stderr.trim() || "version output unavailable";
    return {
      name,
      ok: true,
      detail: `${command} available (${version})`,
    };
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    return {
      name,
      ok: false,
      detail: `${command} unavailable: ${message}`,
    };
  }
}

export async function runHarnessDoctor(args: RunHarnessDoctorArgs): Promise<HarnessDoctorResult> {
  const exec = args.exec ?? runExec;
  const pluginTemplatePath = resolve(
    args.repoRoot,
    "tools",
    "opencode-gaia-harness",
    "templates",
    "gaia-plugin.ts",
  );
  const hasTemplate = await fileExists(pluginTemplatePath);

  const templateCheck: HarnessDoctorCheck = {
    name: "plugin-template",
    ok: hasTemplate,
    detail: hasTemplate ? `Found ${pluginTemplatePath}` : `Missing ${pluginTemplatePath}`,
  };

  const [bunCheck, opencodeCheck] = await Promise.all([
    checkCommandAvailability(exec, "bun"),
    checkCommandAvailability(exec, "opencode"),
  ]);

  const checks = [templateCheck, bunCheck, opencodeCheck];
  return {
    ok: checks.every((check) => check.ok),
    checks,
  };
}
