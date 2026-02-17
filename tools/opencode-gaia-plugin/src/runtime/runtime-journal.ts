import { appendFile, mkdir, readFile } from "node:fs/promises";
import { join } from "node:path";

import { z } from "zod";

import type { DelegateGaiaStatus } from "../tools/delegate-gaia.js";
import type { PlanGateState, PlanRiskLevel } from "./plan-gates.js";

const VcsContextSchema = z.object({
  provider: z.enum(["git", "jj", "none"]),
  ref_name: z.string().min(1).optional(),
  revision_id: z.string().min(1).optional(),
});

const RuntimeJournalBaseSchema = z.object({
  event_version: z.literal("1.0"),
  timestamp: z.string().min(1),
  session_id: z.string().min(1),
  work_unit: z.string().min(1),
  stream_id: z.string().min(1).default("default"),
  vcs_context: VcsContextSchema.optional(),
});

const DelegationCompletedEventSchema = RuntimeJournalBaseSchema.extend({
  event_type: z.literal("delegation_completed"),
  status: z.enum(["ok", "retry_succeeded", "parse_failed"]),
  attempt_count: z.number().int().positive(),
  model_used: z.string().min(1),
  parse_error: z.string().nullable(),
});

const RejectionFeedbackRequestedEventSchema = RuntimeJournalBaseSchema.extend({
  event_type: z.literal("rejection_feedback_requested"),
  owner_agent: z.literal("gaia"),
  paused_agent: z.string().min(1),
  question: z.string().min(1),
  reason: z.string().min(1),
});

const PlanGateEvaluatedEventSchema = RuntimeJournalBaseSchema.extend({
  event_type: z.literal("plan_gate_evaluated"),
  risk_level: z.enum(["low", "medium", "high"]),
  operator_approved: z.boolean(),
  allowed: z.boolean(),
  gate: z.enum(["auto_approved", "operator_approved", "needs_operator_approval"]),
  reason: z.string().min(1),
});

const ArtifactsWrittenEventSchema = RuntimeJournalBaseSchema.extend({
  event_type: z.literal("artifacts_written"),
  plan_path: z.string().min(1),
  log_path: z.string().min(1),
  decisions_path: z.string().min(1),
});

const WorkUnitsPrunedEventSchema = RuntimeJournalBaseSchema.extend({
  event_type: z.literal("work_units_pruned"),
  archived_work_units: z.array(z.string()),
});

const RuntimeJournalEventSchema = z.discriminatedUnion("event_type", [
  PlanGateEvaluatedEventSchema,
  DelegationCompletedEventSchema,
  RejectionFeedbackRequestedEventSchema,
  ArtifactsWrittenEventSchema,
  WorkUnitsPrunedEventSchema,
]);

export type RuntimeJournalEvent = z.infer<typeof RuntimeJournalEventSchema>;

export interface AppendRuntimeJournalEventArgs {
  repoRoot: string;
  event: RuntimeJournalEvent;
}

export interface ReadRuntimeJournalEventsArgs {
  repoRoot: string;
  sessionId: string;
  workUnit: string;
}

export interface RuntimeJournalState {
  session_id: string;
  work_unit: string;
  stream_id: string;
  vcs_context: z.infer<typeof VcsContextSchema> | null;
  latest_risk_level: PlanRiskLevel | null;
  latest_gate: PlanGateState | null;
  gate_allowed: boolean | null;
  gate_reason: string | null;
  latest_status: DelegateGaiaStatus | null;
  last_model_used: string | null;
  last_error: string | null;
  rejection_feedback_pending: boolean;
  rejection_feedback: {
    owner_agent: "gaia";
    paused_agent: string;
    question: string;
    reason: string;
  } | null;
  artifacts_written: boolean;
  artifact_paths: {
    plan_path: string;
    log_path: string;
    decisions_path: string;
  } | null;
  archived_work_units: string[];
  event_count: number;
  last_event_at: string | null;
}

function runtimeJournalPath(repoRoot: string, sessionId: string, workUnit: string): string {
  return join(repoRoot, ".gaia", "runtime", sessionId, `${workUnit}.ndjson`);
}

export async function appendRuntimeJournalEvent(args: AppendRuntimeJournalEventArgs): Promise<void> {
  const event = RuntimeJournalEventSchema.parse(args.event);
  const path = runtimeJournalPath(args.repoRoot, event.session_id, event.work_unit);
  await mkdir(join(args.repoRoot, ".gaia", "runtime", event.session_id), { recursive: true });
  await appendFile(path, `${JSON.stringify(event)}\n`, "utf8");
}

export async function readRuntimeJournalEvents(
  args: ReadRuntimeJournalEventsArgs,
): Promise<RuntimeJournalEvent[]> {
  const path = runtimeJournalPath(args.repoRoot, args.sessionId, args.workUnit);
  let content = "";
  try {
    content = await readFile(path, "utf8");
  } catch {
    return [];
  }

  const lines = content
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter((line) => line.length > 0);

  return lines.map((line, index) => {
    try {
      const parsed = JSON.parse(line) as unknown;
      return RuntimeJournalEventSchema.parse(parsed);
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      throw new Error(`Invalid runtime journal event at line ${index + 1}: ${message}`);
    }
  });
}

export function reduceRuntimeJournal(events: RuntimeJournalEvent[]): RuntimeJournalState {
  const first = events[0];
  if (!first) {
    throw new Error("Cannot reduce empty runtime journal");
  }

  const state: RuntimeJournalState = {
    session_id: first.session_id,
    work_unit: first.work_unit,
    stream_id: first.stream_id,
    vcs_context: first.vcs_context ?? null,
    latest_risk_level: null,
    latest_gate: null,
    gate_allowed: null,
    gate_reason: null,
    latest_status: null,
    last_model_used: null,
    last_error: null,
    rejection_feedback_pending: false,
    rejection_feedback: null,
    artifacts_written: false,
    artifact_paths: null,
    archived_work_units: [],
    event_count: events.length,
    last_event_at: null,
  };

  for (const event of events) {
    if (event.session_id !== state.session_id || event.work_unit !== state.work_unit) {
      throw new Error("Runtime journal reducer received mixed session or work unit events");
    }

    if (event.stream_id !== state.stream_id) {
      throw new Error("Runtime journal reducer received mixed stream ids");
    }

    state.last_event_at = event.timestamp;
    if (event.vcs_context) {
      state.vcs_context = event.vcs_context;
    }
    if (event.event_type === "plan_gate_evaluated") {
      state.latest_risk_level = event.risk_level;
      state.latest_gate = event.gate;
      state.gate_allowed = event.allowed;
      state.gate_reason = event.reason;
      continue;
    }

    if (event.event_type === "delegation_completed") {
      state.latest_status = event.status;
      state.last_model_used = event.model_used;
      state.last_error = event.parse_error;

      if (event.status === "ok" || event.status === "retry_succeeded") {
        state.rejection_feedback_pending = false;
        state.rejection_feedback = null;
      }

      continue;
    }

    if (event.event_type === "rejection_feedback_requested") {
      state.rejection_feedback_pending = true;
      state.rejection_feedback = {
        owner_agent: event.owner_agent,
        paused_agent: event.paused_agent,
        question: event.question,
        reason: event.reason,
      };
      continue;
    }

    if (event.event_type === "artifacts_written") {
      state.artifacts_written = true;
      state.artifact_paths = {
        plan_path: event.plan_path,
        log_path: event.log_path,
        decisions_path: event.decisions_path,
      };
      continue;
    }

    state.archived_work_units = [...state.archived_work_units, ...event.archived_work_units];
  }

  return state;
}
