import { resolve } from "node:path";

import type { ExecFn } from "./exec.js";
import { runExec } from "./exec.js";
import { copyFileWithParents, ensureDirectory, fileExists, writeIfMissing } from "./fs.js";
import { buildSandboxEnv, buildSandboxPaths } from "./paths.js";

export function getDefaultSandboxConfigJsonc(): string {
  return `{
  "$schema": "https://opencode.ai/config.json",
  "model": "opencode/glm-5-free",
  "small_model": "opencode/glm-5-free",
  "permission": {
    "bash": "ask",
    "edit": "ask",
    "write": "ask"
  },
  "server": {
    "hostname": "0.0.0.0",
    "port": 4096
  }
}
`;
}

export function getSandboxConfigPackageJson(): string {
  return `{
  "name": "opencode-gaia-sandbox-config",
  "private": true,
  "type": "module",
  "dependencies": {
    "@opencode-ai/plugin": "1.2.6"
  }
}
`;
}

export interface BootstrapOptions {
  repoRoot: string;
  exec?: ExecFn;
}

export async function bootstrapSandbox(options: BootstrapOptions): Promise<void> {
  const exec = options.exec ?? runExec;
  const paths = buildSandboxPaths(resolve(options.repoRoot));

  await ensureDirectory(paths.homeDir);
  await ensureDirectory(paths.xdgConfigHome);
  await ensureDirectory(paths.xdgCacheHome);
  await ensureDirectory(paths.xdgDataHome);
  await ensureDirectory(paths.opencodeConfigDir);
  await ensureDirectory(resolve(paths.opencodeConfigDir, "plugins"));

  await writeIfMissing(paths.opencodeConfigPath, getDefaultSandboxConfigJsonc());
  await writeIfMissing(paths.sandboxPackageJsonPath, getSandboxConfigPackageJson());

  if (!(await fileExists(paths.pluginTemplatePath))) {
    throw new Error(`Plugin template not found: ${paths.pluginTemplatePath}`);
  }

  await copyFileWithParents(paths.pluginTemplatePath, paths.pluginTargetPath);

  const pluginModulePath = resolve(paths.opencodeConfigDir, "node_modules/@opencode-ai/plugin");
  if (!(await fileExists(resolve(pluginModulePath, "dist/index.js")))) {
    await exec("bun", ["install", "--cwd", paths.opencodeConfigDir], {
      cwd: paths.repoRoot,
      env: buildSandboxEnv(paths),
      stdio: "inherit",
    });
  }
}
