import { mkdir, readdir, writeFile } from "node:fs/promises";
import { join } from "node:path";

import { readRuntimeJournalEvents, reduceRuntimeJournal, type RuntimeJournalState } from "./runtime-journal.js";
import { openStream, readStreamIndex, writeStreamIndex, type StreamIndex, type StreamStatus } from "./streams.js";

export interface AggregateSessionRuntimeStateArgs {
  session_id: string;
  work_units: Record<string, RuntimeJournalState>;
}

export interface SessionStreamSummary {
  stream_id: string;
  active_work_units: string[];
  completed_work_units: string[];
  blocked_work_units: string[];
  last_event_at: string | null;
}

export interface SessionRuntimeState {
  session_id: string;
  current_stream_id: string | null;
  active_work_units: string[];
  completed_work_units: string[];
  blocked_work_units: string[];
  work_units: Record<string, RuntimeJournalState>;
  streams: Record<string, SessionStreamSummary>;
}

export interface RefreshSessionRuntimeStateArgs {
  repoRoot: string;
  sessionId: string;
}

interface WorkUnitClassification {
  state: "active" | "completed" | "blocked";
  streamId: string;
}

function classifyWorkUnit(state: RuntimeJournalState): WorkUnitClassification {
  const streamId = state.stream_id;
  if (
    state.gate_allowed === false
    || state.latest_gate === "needs_operator_approval"
    || state.rejection_feedback_pending
  ) {
    return {
      state: "blocked",
      streamId,
    };
  }

  if (state.artifacts_written && (state.latest_status === "ok" || state.latest_status === "retry_succeeded")) {
    return {
      state: "completed",
      streamId,
    };
  }

  return {
    state: "active",
    streamId,
  };
}

function sortStable(items: string[]): string[] {
  return [...items].sort((left, right) => left.localeCompare(right));
}

export function aggregateSessionRuntimeState(args: AggregateSessionRuntimeStateArgs): SessionRuntimeState {
  const active: string[] = [];
  const completed: string[] = [];
  const blocked: string[] = [];

  const streamMap: Record<string, SessionStreamSummary> = {};
  let currentStreamId: string | null = null;
  let currentStreamLastEventAt: string | null = null;

  for (const [workUnit, workUnitState] of Object.entries(args.work_units)) {
    const classification = classifyWorkUnit(workUnitState);

    let streamSummary = streamMap[classification.streamId];
    if (!streamSummary) {
      streamSummary = {
        stream_id: classification.streamId,
        active_work_units: [],
        completed_work_units: [],
        blocked_work_units: [],
        last_event_at: null,
      };

      streamMap[classification.streamId] = streamSummary;
    }

    if (classification.state === "active") {
      active.push(workUnit);
      streamSummary.active_work_units.push(workUnit);
    } else if (classification.state === "completed") {
      completed.push(workUnit);
      streamSummary.completed_work_units.push(workUnit);
    } else {
      blocked.push(workUnit);
      streamSummary.blocked_work_units.push(workUnit);
    }

    const lastEvent = workUnitState.last_event_at;
    if (lastEvent && (!streamSummary.last_event_at || lastEvent > streamSummary.last_event_at)) {
      streamSummary.last_event_at = lastEvent;
    }

    if (lastEvent && (!currentStreamLastEventAt || lastEvent > currentStreamLastEventAt)) {
      currentStreamLastEventAt = lastEvent;
      currentStreamId = classification.streamId;
    }
  }

  for (const streamSummary of Object.values(streamMap)) {
    streamSummary.active_work_units = sortStable(streamSummary.active_work_units);
    streamSummary.completed_work_units = sortStable(streamSummary.completed_work_units);
    streamSummary.blocked_work_units = sortStable(streamSummary.blocked_work_units);
  }

  return {
    session_id: args.session_id,
    current_stream_id: currentStreamId,
    active_work_units: sortStable(active),
    completed_work_units: sortStable(completed),
    blocked_work_units: sortStable(blocked),
    work_units: args.work_units,
    streams: streamMap,
  };
}

function renderSessionStatusMarkdown(state: SessionRuntimeState): string {
  const lines = [
    `# Session Status: ${state.session_id}`,
    "",
    `Current stream: ${state.current_stream_id ?? "none"}`,
    "",
    "## Active Work Units",
    ...(state.active_work_units.length > 0 ? state.active_work_units.map((item) => `- ${item}`) : ["- none"]),
    "",
    "## Completed Work Units",
    ...(state.completed_work_units.length > 0
      ? state.completed_work_units.map((item) => `- ${item}`)
      : ["- none"]),
    "",
    "## Blocked Work Units",
    ...(state.blocked_work_units.length > 0 ? state.blocked_work_units.map((item) => `- ${item}`) : ["- none"]),
  ];

  return `${lines.join("\n")}\n`;
}

function renderStreamStatusMarkdown(stream: SessionStreamSummary): string {
  const lines = [
    `# Stream Status: ${stream.stream_id}`,
    "",
    `Last event: ${stream.last_event_at ?? "unknown"}`,
    "",
    "## Active Work Units",
    ...(stream.active_work_units.length > 0
      ? stream.active_work_units.map((item) => `- ${item}`)
      : ["- none"]),
    "",
    "## Completed Work Units",
    ...(stream.completed_work_units.length > 0
      ? stream.completed_work_units.map((item) => `- ${item}`)
      : ["- none"]),
    "",
    "## Blocked Work Units",
    ...(stream.blocked_work_units.length > 0
      ? stream.blocked_work_units.map((item) => `- ${item}`)
      : ["- none"]),
  ];

  return `${lines.join("\n")}\n`;
}

async function syncStreamIndex(repoRoot: string, state: SessionRuntimeState): Promise<StreamIndex> {
  let index = await readStreamIndex(repoRoot);

  for (const [streamId, streamState] of Object.entries(state.streams)) {
    if (!index.streams[streamId]) {
      index = await openStream({
        repoRoot,
        streamId,
        title: streamId,
        sessionId: state.session_id,
        ...(streamState.active_work_units[0]
          || streamState.completed_work_units[0]
          || streamState.blocked_work_units[0]
          ? {
              workUnit:
                streamState.active_work_units[0]
                ?? streamState.completed_work_units[0]
                ?? streamState.blocked_work_units[0],
            }
          : {}),
        setActive: false,
      });
    }

    const stream = index.streams[streamId];
    if (!stream) {
      continue;
    }

    let status: StreamStatus = "active";
    if (streamState.blocked_work_units.length > 0) {
      status = "blocked";
    } else if (streamState.active_work_units.length === 0 && streamState.completed_work_units.length > 0) {
      status = "done";
    }

    index.streams[streamId] = {
      ...stream,
      status,
      updated_at: new Date().toISOString(),
      last_session_id: state.session_id,
      last_work_unit:
        streamState.active_work_units[0]
        ?? streamState.blocked_work_units[0]
        ?? streamState.completed_work_units[0]
        ?? stream.last_work_unit,
    };
  }

  index.active_stream_id = state.current_stream_id;
  await writeStreamIndex(repoRoot, index);
  return index;
}

async function listSessionWorkUnits(repoRoot: string, sessionId: string): Promise<string[]> {
  const runtimeDir = join(repoRoot, ".gaia", "runtime", sessionId);
  let entries: string[] = [];
  try {
    entries = await readdir(runtimeDir);
  } catch {
    return [];
  }

  return entries
    .filter((entry) => entry.endsWith(".ndjson"))
    .map((entry) => entry.slice(0, -".ndjson".length))
    .sort((left, right) => left.localeCompare(right));
}

export async function refreshSessionRuntimeState(args: RefreshSessionRuntimeStateArgs): Promise<SessionRuntimeState> {
  const workUnits = await listSessionWorkUnits(args.repoRoot, args.sessionId);

  const reducedEntries = await Promise.all(
    workUnits.map(async (workUnit) => {
      const events = await readRuntimeJournalEvents({
        repoRoot: args.repoRoot,
        sessionId: args.sessionId,
        workUnit,
      });

      return [workUnit, reduceRuntimeJournal(events)] as const;
    }),
  );

  const workUnitStates = Object.fromEntries(reducedEntries);
  const sessionState = aggregateSessionRuntimeState({
    session_id: args.sessionId,
    work_units: workUnitStates,
  });

  await mkdir(join(args.repoRoot, ".gaia", "runtime", args.sessionId), { recursive: true });
  await writeFile(
    join(args.repoRoot, ".gaia", "runtime", args.sessionId, "state.json"),
    `${JSON.stringify(sessionState, null, 2)}\n`,
    "utf8",
  );

  await mkdir(join(args.repoRoot, ".gaia", "plans"), { recursive: true });
  await writeFile(
    join(args.repoRoot, ".gaia", "plans", `session-${args.sessionId}-status.md`),
    renderSessionStatusMarkdown(sessionState),
    "utf8",
  );

  for (const streamState of Object.values(sessionState.streams)) {
    const streamRoot = join(args.repoRoot, ".gaia", "streams", streamState.stream_id);
    await mkdir(streamRoot, { recursive: true });
    await writeFile(join(streamRoot, "status.md"), renderStreamStatusMarkdown(streamState), "utf8");
    await writeFile(join(streamRoot, "state.json"), `${JSON.stringify(streamState, null, 2)}\n`, "utf8");
  }

  await syncStreamIndex(args.repoRoot, sessionState);
  return sessionState;
}
