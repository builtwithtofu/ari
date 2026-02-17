import { describe, expect, test } from "bun:test";

import {
  parseAthenaOutput,
  parseDemeterOutput,
  parseHephaestusOutput,
  parseLeanAgentOutput,
} from "./contracts";

describe("lean contract parsers", () => {
  test("parses valid ATHENA output", () => {
    const parsed = parseAthenaOutput({
      contract_version: "1.0",
      agent: "athena",
      work_unit: "unit-2",
      session_id: "s1",
      ok: true,
      data: {
        repo_map: "tools/opencode-gaia-plugin/src",
        plan: ["find entry points"],
        risk_list: ["missing parser"],
        suggested_agents: ["hephaestus"],
      },
      errors: [],
    });

    expect(parsed.agent).toBe("athena");
    expect(parsed.data.repo_map).toBe("tools/opencode-gaia-plugin/src");
  });

  test("rejects mismatched agent payload", () => {
    expect(() => {
      parseHephaestusOutput({
        contract_version: "1.0",
        agent: "demeter",
        work_unit: "unit-2",
        session_id: "s2",
        ok: true,
        data: {
          diff_summary: "updated files",
          files_modified: ["src/index.ts"],
          revision_ids: ["abc123"],
          notes: [],
          refactoring_done: [],
          known_issues: [],
        },
        errors: [],
      });
    }).toThrow();
  });

  test("routes parser by agent key", () => {
    const parsed = parseLeanAgentOutput("demeter", {
      contract_version: "1.0",
      agent: "demeter",
      work_unit: "unit-2",
      session_id: "s3",
      ok: true,
      data: {
        log_entry: "Completed unit",
        decisions: [
          {
            type: "question",
            question: "Which runner?",
            answer: "bun test",
            impact: "No Vitest dependency",
          },
        ],
        learnings: ["keep prompts lean"],
        plan_updates: ["unit-2 in progress"],
        session_summary: "Done",
        status_report: {
          active_work_units: ["unit-2"],
          completed_work_units: ["unit-1"],
          blocked_work_units: [],
          upcoming_checkpoints: ["operator review after implementation"],
        },
      },
      errors: [],
    });

    expect(parsed.agent).toBe("demeter");
    expect(parsed.data.decisions[0]?.answer).toBe("bun test");
    expect(parsed.data.status_report.active_work_units).toEqual(["unit-2"]);
  });

  test("rejects bad envelope shape", () => {
    expect(() => {
      parseAthenaOutput({
        contract_version: "2.0",
      });
    }).toThrow();
  });
});
