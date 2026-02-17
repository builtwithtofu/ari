import { z } from "zod";

import type {
  AthenaOutput,
  DemeterOutput,
  GaiaOutput,
  HephaestusOutput,
  LeanAgentKey,
} from "./types.js";

const BaseEnvelopeSchema = z.object({
  contract_version: z.literal("1.0"),
  work_unit: z.string().min(1),
  session_id: z.string().min(1),
  ok: z.boolean(),
  errors: z.array(z.string()),
});

const GaiaOutputSchema = BaseEnvelopeSchema.extend({
  agent: z.literal("gaia"),
  data: z.object({
    next_actions: z.array(z.string()),
    delegations: z.array(z.string()),
    summary: z.string(),
  }),
});

const AthenaOutputSchema = BaseEnvelopeSchema.extend({
  agent: z.literal("athena"),
  data: z.object({
    repo_map: z.string(),
    plan: z.array(z.string()),
    risk_list: z.array(z.string()),
    suggested_agents: z.array(z.string()),
  }),
});

const HephaestusOutputSchema = BaseEnvelopeSchema.extend({
  agent: z.literal("hephaestus"),
  data: z.object({
    diff_summary: z.string(),
    files_modified: z.array(z.string()),
    revision_ids: z.array(z.string()),
    notes: z.array(z.string()),
    refactoring_done: z.array(z.string()),
    known_issues: z.array(z.string()),
  }),
});

const DemeterOutputSchema = BaseEnvelopeSchema.extend({
  agent: z.literal("demeter"),
  data: z.object({
    log_entry: z.string(),
    decisions: z.array(
      z.object({
        type: z.enum(["question", "rejection", "mode_switch", "pair_feedback"]),
        question: z.string(),
        answer: z.string(),
        rationale: z.string().optional(),
        impact: z.string(),
      }),
    ),
    learnings: z.array(z.string()),
    plan_updates: z.array(z.string()),
    session_summary: z.string(),
    status_report: z.object({
      active_work_units: z.array(z.string()),
      completed_work_units: z.array(z.string()),
      blocked_work_units: z.array(z.string()),
      upcoming_checkpoints: z.array(z.string()),
    }),
  }),
});

export function parseGaiaOutput(input: unknown): GaiaOutput {
  return GaiaOutputSchema.parse(input) as GaiaOutput;
}

export function parseAthenaOutput(input: unknown): AthenaOutput {
  return AthenaOutputSchema.parse(input) as AthenaOutput;
}

export function parseHephaestusOutput(input: unknown): HephaestusOutput {
  return HephaestusOutputSchema.parse(input) as HephaestusOutput;
}

export function parseDemeterOutput(input: unknown): DemeterOutput {
  return DemeterOutputSchema.parse(input) as DemeterOutput;
}

export function parseLeanAgentOutput(
  agent: LeanAgentKey,
  input: unknown,
): GaiaOutput | AthenaOutput | HephaestusOutput | DemeterOutput {
  switch (agent) {
    case "gaia":
      return parseGaiaOutput(input);
    case "athena":
      return parseAthenaOutput(input);
    case "hephaestus":
      return parseHephaestusOutput(input);
    case "demeter":
      return parseDemeterOutput(input);
    default: {
      const neverAgent: never = agent;
      throw new Error(`Unsupported lean agent: ${neverAgent}`);
    }
  }
}
