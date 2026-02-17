import { mkdir, readFile, rm } from "node:fs/promises";
import { resolve } from "node:path";

import { forkStream, openStream, runGaiaWorkUnit } from "../src/index.js";

function parseArgPath(): string {
  const input = process.argv[2];
  if (input && input.trim().length > 0) {
    return resolve(input.trim());
  }

  return resolve(process.cwd(), "tmp", "gaia-stream-demo");
}

function printStep(step: string): void {
  process.stdout.write(`\n== ${step}\n`);
}

const responseText = '{"contract_version":"1.0","agent":"gaia"}';

async function main(): Promise<void> {
  const repoRoot = parseArgPath();

  await rm(repoRoot, { recursive: true, force: true });
  await mkdir(repoRoot, { recursive: true });

  printStep("Demo workspace");
  process.stdout.write(`${repoRoot}\n`);

  printStep("1) Start feature stream and complete normal work");
  await openStream({
    repoRoot,
    streamId: "feature-x",
    title: "Feature X",
    sessionId: "demo-session",
    setActive: true,
  });

  await runGaiaWorkUnit({
    repoRoot,
    streamId: "feature-x",
    workUnit: "feature-x-unit-1",
    sessionId: "demo-session",
    modelUsed: "openai/gpt-5.3-codex",
    responseText,
    parse: (input) => input,
    plan: "# Plan\n- implement feature x",
    log: "# Log\n- in progress",
    decisions: "# Decisions\n- proceed",
    riskLevel: "low",
  });

  printStep("2) Pivot: fork urgent bug stream");
  await forkStream({
    repoRoot,
    parentStreamId: "feature-x",
    childStreamId: "urgent-bug",
    title: "Urgent Production Bug",
    sessionId: "demo-session",
    vcsContext: {
      provider: "jj",
      ref_name: "bugfix/urgent-bug",
    },
  });

  printStep("3) Medium-risk bug work without approval (expected block)");
  try {
    await runGaiaWorkUnit({
      repoRoot,
      streamId: "urgent-bug",
      workUnit: "urgent-bug-unit-1",
      sessionId: "demo-session",
      modelUsed: "openai/gpt-5.3-codex",
      responseText,
      parse: (input) => input,
      plan: "# Plan\n- patch urgent bug",
      log: "# Log\n- blocked until approval",
      decisions: "# Decisions\n- request approval",
      riskLevel: "medium",
      operatorApproved: false,
      vcsContext: {
        provider: "jj",
        ref_name: "bugfix/urgent-bug",
      },
    });
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    process.stdout.write(`Blocked as expected: ${message}\n`);
  }

  printStep("4) Approve and continue urgent bug work");
  await runGaiaWorkUnit({
    repoRoot,
    streamId: "urgent-bug",
    workUnit: "urgent-bug-unit-1",
    sessionId: "demo-session",
    modelUsed: "openai/gpt-5.3-codex",
    responseText,
    parse: (input) => input,
    plan: "# Plan\n- patch urgent bug",
    log: "# Log\n- approved and applied",
    decisions: "# Decisions\n- ship hotfix",
    riskLevel: "medium",
    operatorApproved: true,
    vcsContext: {
      provider: "jj",
      ref_name: "bugfix/urgent-bug",
    },
  });

  printStep("5) Show resumable state outputs");
  const sessionStatePath = resolve(repoRoot, ".gaia", "runtime", "demo-session", "state.json");
  const streamIndexPath = resolve(repoRoot, ".gaia", "streams", "index.json");
  const statusDocPath = resolve(repoRoot, ".gaia", "plans", "session-demo-session-status.md");

  process.stdout.write(`Session state: ${sessionStatePath}\n`);
  process.stdout.write(`Stream index: ${streamIndexPath}\n`);
  process.stdout.write(`Session status doc: ${statusDocPath}\n`);

  const sessionState = await readFile(sessionStatePath, "utf8");
  const streamIndex = await readFile(streamIndexPath, "utf8");

  process.stdout.write("\nSession state snippet:\n");
  process.stdout.write(`${sessionState.slice(0, 600)}\n`);

  process.stdout.write("\nStream index snippet:\n");
  process.stdout.write(`${streamIndex.slice(0, 600)}\n`);

  process.stdout.write("\nDemo complete.\n");
}

main().catch((error) => {
  const message = error instanceof Error ? error.message : String(error);
  process.stderr.write(`${message}\n`);
  process.exit(1);
});
