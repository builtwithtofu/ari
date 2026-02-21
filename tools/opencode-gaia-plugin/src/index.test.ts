import { mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { describe, expect, test } from "bun:test";

import {
  getPluginBanner,
  PLUGIN_NAME,
  PROJECT_PHASE,
  runDelegateGaiaTool,
  runGaiaWorkUnit,
} from "./index";

describe("plugin scaffold", () => {
  test("exports stable plugin name", () => {
    expect(PLUGIN_NAME).toBe("opencode-gaia-plugin");
  });

  test("marks project as pre-alpha", () => {
    expect(PROJECT_PHASE).toBe("pre-alpha");
  });

  test("renders deterministic banner", () => {
    expect(getPluginBanner()).toBe("opencode-gaia-plugin (pre-alpha)");
  });

  test("exposes a runnable work-unit pipeline", async () => {
    const repoRoot = await mkdtemp(join(tmpdir(), "gaia-entry-"));

    try {
      const result = await runGaiaWorkUnit({
        repoRoot,
        workUnit: "unit-entry-1",
        sessionId: "entry-s1",
        modelUsed: "openai/gpt-5.3-codex",
        responseText: '{"contract_version":"1.0","agent":"gaia"}',
        parse: (input) => input,
        plan: "# Plan\n- unit",
        log: "# Log\n- running",
        decisions: "# Decisions\n- ship",
      });

      expect(result.delegation.status).toBe("ok");
      expect(result.collection.total).toBe(1);

      const planContent = await readFile(join(repoRoot, ".gaia", "unit-entry-1", "plan.md"), "utf8");
      expect(planContent).toBe("# Plan\n- unit");
    } finally {
      await rm(repoRoot, { recursive: true, force: true });
    }
  });

  test("exposes delegate_gaia facade from entrypoint", async () => {
    const repoRoot = await mkdtemp(join(tmpdir(), "gaia-entry-"));

    try {
      const responseText = JSON.stringify({
        contract_version: "1.0",
        agent: "gaia",
        work_unit: "unit-entry-2",
        session_id: "entry-s2",
        ok: true,
        data: {
          next_actions: ["continue"],
          delegations: ["athena"],
          summary: "ok",
        },
        errors: [],
      });

      const result = await runDelegateGaiaTool({
        repoRoot,
        workUnit: "unit-entry-2",
        sessionId: "entry-s2",
        modelUsed: "openai/gpt-5.3-codex",
        agent: "gaia",
        responseText,
      });

      expect(result.delegation.status).toBe("ok");
      expect(result.delegation.parsed_json?.agent).toBe("gaia");
    } finally {
      await rm(repoRoot, { recursive: true, force: true });
    }
  });
});
