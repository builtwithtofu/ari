import { mkdir, readFile, writeFile } from "node:fs/promises";
import { join } from "node:path";

const STREAM_ID_PATTERN = /^[A-Za-z0-9][A-Za-z0-9._-]*$/;

export type StreamStatus = "active" | "paused" | "blocked" | "done" | "archived";
export type StreamRelationType = "forked_from" | "stacked_on" | "split_from" | "parallel_to";
export type VcsProvider = "git" | "jj" | "none";

export interface StreamVcsContext {
  provider: VcsProvider;
  ref_name?: string;
  revision_id?: string;
}

export interface StreamRecord {
  stream_id: string;
  title: string;
  status: StreamStatus;
  parent_stream_id: string | null;
  relation_type: StreamRelationType | null;
  depends_on_stream_ids: string[];
  created_at: string;
  updated_at: string;
  last_session_id: string | null;
  last_work_unit: string | null;
  vcs_context: StreamVcsContext;
}

export interface StreamIndex {
  version: "1.0";
  active_stream_id: string | null;
  streams: Record<string, StreamRecord>;
}

export interface OpenStreamArgs {
  repoRoot: string;
  streamId: string;
  title: string;
  status?: StreamStatus;
  parentStreamId?: string;
  relationType?: StreamRelationType;
  dependsOnStreamIds?: string[];
  sessionId?: string;
  workUnit?: string;
  vcsContext?: StreamVcsContext;
  setActive?: boolean;
}

export interface ForkStreamArgs {
  repoRoot: string;
  parentStreamId: string;
  childStreamId: string;
  title: string;
  sessionId?: string;
  vcsContext?: StreamVcsContext;
}

export interface SetActiveStreamArgs {
  repoRoot: string;
  streamId: string;
}

export interface UpdateStreamProgressArgs {
  repoRoot: string;
  streamId: string;
  status: StreamStatus;
  sessionId: string;
  workUnit: string;
  vcsContext?: StreamVcsContext;
  setActive?: boolean;
}

function validateStreamId(streamId: string): void {
  if (!STREAM_ID_PATTERN.test(streamId)) {
    throw new Error(`Invalid stream id: ${streamId}`);
  }
}

function streamsRoot(repoRoot: string): string {
  return join(repoRoot, ".gaia", "streams");
}

function streamIndexPath(repoRoot: string): string {
  return join(streamsRoot(repoRoot), "index.json");
}

function nowIso(): string {
  return new Date().toISOString();
}

function defaultVcsContext(): StreamVcsContext {
  return { provider: "none" };
}

function mergeVcsContext(current: StreamVcsContext, next: StreamVcsContext | undefined): StreamVcsContext {
  if (!next) {
    return current;
  }

  return {
    provider: next.provider,
    ...(next.ref_name ? { ref_name: next.ref_name } : {}),
    ...(next.revision_id ? { revision_id: next.revision_id } : {}),
  };
}

function defaultStreamIndex(): StreamIndex {
  return {
    version: "1.0",
    active_stream_id: null,
    streams: {},
  };
}

export async function readStreamIndex(repoRoot: string): Promise<StreamIndex> {
  const indexPath = streamIndexPath(repoRoot);
  let raw = "";
  try {
    raw = await readFile(indexPath, "utf8");
  } catch {
    return defaultStreamIndex();
  }

  const parsed = JSON.parse(raw) as StreamIndex;
  return {
    version: "1.0",
    active_stream_id: parsed.active_stream_id ?? null,
    streams: parsed.streams ?? {},
  };
}

export async function writeStreamIndex(repoRoot: string, index: StreamIndex): Promise<void> {
  await mkdir(streamsRoot(repoRoot), { recursive: true });
  await writeFile(streamIndexPath(repoRoot), `${JSON.stringify(index, null, 2)}\n`, "utf8");
}

function ensureStreamExists(index: StreamIndex, streamId: string): StreamRecord {
  const stream = index.streams[streamId];
  if (!stream) {
    throw new Error(`Unknown stream: ${streamId}`);
  }

  return stream;
}

export async function openStream(args: OpenStreamArgs): Promise<StreamIndex> {
  validateStreamId(args.streamId);
  for (const dependency of args.dependsOnStreamIds ?? []) {
    validateStreamId(dependency);
  }

  const index = await readStreamIndex(args.repoRoot);
  const existing = index.streams[args.streamId];
  const timestamp = nowIso();

  index.streams[args.streamId] = {
    stream_id: args.streamId,
    title: args.title,
    status: args.status ?? existing?.status ?? "active",
    parent_stream_id: args.parentStreamId ?? existing?.parent_stream_id ?? null,
    relation_type: args.relationType ?? existing?.relation_type ?? null,
    depends_on_stream_ids: args.dependsOnStreamIds ?? existing?.depends_on_stream_ids ?? [],
    created_at: existing?.created_at ?? timestamp,
    updated_at: timestamp,
    last_session_id: args.sessionId ?? existing?.last_session_id ?? null,
    last_work_unit: args.workUnit ?? existing?.last_work_unit ?? null,
    vcs_context: mergeVcsContext(existing?.vcs_context ?? defaultVcsContext(), args.vcsContext),
  };

  if (args.setActive ?? true) {
    index.active_stream_id = args.streamId;
  }

  await writeStreamIndex(args.repoRoot, index);
  return index;
}

export async function forkStream(args: ForkStreamArgs): Promise<StreamIndex> {
  const index = await readStreamIndex(args.repoRoot);
  ensureStreamExists(index, args.parentStreamId);

  return openStream({
    repoRoot: args.repoRoot,
    streamId: args.childStreamId,
    title: args.title,
    parentStreamId: args.parentStreamId,
    relationType: "forked_from",
    ...(args.sessionId ? { sessionId: args.sessionId } : {}),
    ...(args.vcsContext ? { vcsContext: args.vcsContext } : {}),
    setActive: true,
  });
}

export async function setActiveStream(args: SetActiveStreamArgs): Promise<StreamIndex> {
  const index = await readStreamIndex(args.repoRoot);
  ensureStreamExists(index, args.streamId);
  index.active_stream_id = args.streamId;
  await writeStreamIndex(args.repoRoot, index);
  return index;
}

export async function updateStreamProgress(args: UpdateStreamProgressArgs): Promise<StreamIndex> {
  const index = await readStreamIndex(args.repoRoot);
  const stream = ensureStreamExists(index, args.streamId);

  index.streams[args.streamId] = {
    ...stream,
    status: args.status,
    updated_at: nowIso(),
    last_session_id: args.sessionId,
    last_work_unit: args.workUnit,
    vcs_context: mergeVcsContext(stream.vcs_context, args.vcsContext),
  };

  if (args.setActive ?? true) {
    index.active_stream_id = args.streamId;
  }

  await writeStreamIndex(args.repoRoot, index);
  return index;
}
