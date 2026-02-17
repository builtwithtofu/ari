import { mkdtemp, mkdir, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { afterEach, describe, expect, test } from "bun:test";

import type { ExecFn } from "./exec";
import { runHarnessDoctor } from "./preflight";

const tempDirs: string[] = [];

afterEach(async () => {
  await Promise.all(
    tempDirs.splice(0).map(async (directory) => {
      await rm(directory, { recursive: true, force: true });
    }),
  );
});

async function createRepoWithTemplate(): Promise<string> {
  const repoRoot = await mkdtemp(join(tmpdir(), "gaia-doctor-"));
  tempDirs.push(repoRoot);

  const templatePath = join(repoRoot, "tools", "opencode-gaia-harness", "templates");
  await mkdir(templatePath, { recursive: true });
  await writeFile(join(templatePath, "gaia-plugin.ts"), "export default {};\n", "utf8");

  return repoRoot;
}

describe("runHarnessDoctor", () => {
  test("passes when required files and commands are available", async () => {
    const repoRoot = await createRepoWithTemplate();

    const exec: ExecFn = async () => ({
      exitCode: 0,
      stdout: "ok",
      stderr: "",
    });

    const result = await runHarnessDoctor({ repoRoot, exec });

    expect(result.ok).toBe(true);
    expect(result.checks).toEqual([
      {
        name: "plugin-template",
        ok: true,
        detail: expect.stringContaining("gaia-plugin.ts"),
      },
      {
        name: "bun-cli",
        ok: true,
        detail: expect.stringContaining("available"),
      },
      {
        name: "opencode-cli",
        ok: true,
        detail: expect.stringContaining("available"),
      },
    ]);
  });

  test("fails when opencode command is unavailable", async () => {
    const repoRoot = await createRepoWithTemplate();

    const exec: ExecFn = async (command) => {
      if (command === "opencode") {
        throw new Error("spawn opencode ENOENT");
      }

      return {
        exitCode: 0,
        stdout: "ok",
        stderr: "",
      };
    };

    const result = await runHarnessDoctor({ repoRoot, exec });

    expect(result.ok).toBe(false);
    expect(result.checks).toEqual([
      {
        name: "plugin-template",
        ok: true,
        detail: expect.stringContaining("gaia-plugin.ts"),
      },
      {
        name: "bun-cli",
        ok: true,
        detail: expect.stringContaining("available"),
      },
      {
        name: "opencode-cli",
        ok: false,
        detail: expect.stringContaining("spawn opencode ENOENT"),
      },
    ]);
  });
});
