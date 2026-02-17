import { mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { afterEach, describe, expect, test } from "bun:test";

import { forkStream, openStream, setActiveStream, updateStreamProgress } from "./streams";

const tempDirs: string[] = [];

afterEach(async () => {
  await Promise.all(
    tempDirs.splice(0).map(async (directory) => {
      await rm(directory, { recursive: true, force: true });
    }),
  );
});

describe("streams", () => {
  test("opens stream and sets active pointer", async () => {
    const repoRoot = await mkdtemp(join(tmpdir(), "gaia-streams-"));
    tempDirs.push(repoRoot);

    const index = await openStream({
      repoRoot,
      streamId: "feature-x",
      title: "Feature X",
      sessionId: "s1",
    });

    expect(index.active_stream_id).toBe("feature-x");
    expect(index.streams["feature-x"]?.title).toBe("Feature X");
  });

  test("forks child stream from parent", async () => {
    const repoRoot = await mkdtemp(join(tmpdir(), "gaia-streams-"));
    tempDirs.push(repoRoot);

    await openStream({
      repoRoot,
      streamId: "main-work",
      title: "Main Work",
    });

    const index = await forkStream({
      repoRoot,
      parentStreamId: "main-work",
      childStreamId: "bugfix-urgent",
      title: "Urgent Bugfix",
      sessionId: "s2",
      vcsContext: {
        provider: "jj",
        ref_name: "book-urgent",
      },
    });

    expect(index.active_stream_id).toBe("bugfix-urgent");
    expect(index.streams["bugfix-urgent"]?.parent_stream_id).toBe("main-work");
    expect(index.streams["bugfix-urgent"]?.relation_type).toBe("forked_from");
  });

  test("updates stream progress and persists index", async () => {
    const repoRoot = await mkdtemp(join(tmpdir(), "gaia-streams-"));
    tempDirs.push(repoRoot);

    await openStream({
      repoRoot,
      streamId: "feature-y",
      title: "Feature Y",
    });

    await setActiveStream({
      repoRoot,
      streamId: "feature-y",
    });

    const index = await updateStreamProgress({
      repoRoot,
      streamId: "feature-y",
      status: "blocked",
      sessionId: "s3",
      workUnit: "unit-22",
      vcsContext: {
        provider: "git",
        ref_name: "feature/y",
        revision_id: "abc123",
      },
    });

    expect(index.streams["feature-y"]?.status).toBe("blocked");
    expect(index.streams["feature-y"]?.last_work_unit).toBe("unit-22");

    const persisted = JSON.parse(
      await readFile(join(repoRoot, ".gaia", "streams", "index.json"), "utf8"),
    ) as {
      active_stream_id: string;
      streams: Record<string, { status: string }>;
    };

    expect(persisted.active_stream_id).toBe("feature-y");
    expect(persisted.streams["feature-y"]?.status).toBe("blocked");
  });
});
