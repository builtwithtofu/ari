import { mkdir, writeFile } from "node:fs/promises";
import { join } from "node:path";

export interface CreateManualWorkspaceArgs {
  repoRoot: string;
  label?: string;
  now?: Date;
}

export interface ManualTuiArgs {
  label?: string;
  model?: string;
}

export interface ManualWorkspace {
  workspaceId: string;
  workspacePath: string;
}

function formatTimestamp(now: Date): string {
  const iso = now.toISOString();
  const ymd = iso.slice(0, 10).replaceAll("-", "");
  const hms = iso.slice(11, 19).replaceAll(":", "");
  return `${ymd}-${hms}`;
}

function sanitizeLabel(label: string | undefined): string {
  const normalized = (label ?? "sandbox-work")
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .replace(/-+/g, "-");

  return normalized.length > 0 ? normalized : "sandbox-work";
}

function workspaceReadme(workspaceId: string): string {
  return [
    "# GAIA Manual TUI Workspace",
    "",
    `This is a temporary manual TUI testing workspace: ${workspaceId}.`,
    "",
    "Suggested prompts to try in OpenCode:",
    "- I am working on feature-x and need a quick work-unit plan.",
    "- A critical production bug happened; pivot to urgent-bug flow.",
    "- Show current stream status and what is blocked vs completed.",
    "",
    "Suggested stream ids:",
    "- feature-x",
    "- urgent-bug",
    "",
  ].join("\n");
}

export async function createManualWorkspace(args: CreateManualWorkspaceArgs): Promise<ManualWorkspace> {
  const timestamp = formatTimestamp(args.now ?? new Date());
  const slug = sanitizeLabel(args.label);
  const workspaceId = `${timestamp}-${slug}`;
  const workspacePath = join(args.repoRoot, ".sandbox", "workspaces", workspaceId);

  await mkdir(workspacePath, { recursive: true });
  await writeFile(join(workspacePath, "README.md"), workspaceReadme(workspaceId), "utf8");

  return {
    workspaceId,
    workspacePath,
  };
}

export function parseManualTuiArgs(args: string[]): ManualTuiArgs {
  const labelParts: string[] = [];
  let model: string | undefined;

  for (let index = 0; index < args.length; index += 1) {
    const token = args[index];
    if (!token) {
      continue;
    }

    if (token === "--model") {
      const value = args[index + 1];
      if (!value || value.startsWith("--")) {
        throw new Error("manual-tui requires a model value after --model");
      }

      model = value;
      index += 1;
      continue;
    }

    labelParts.push(token);
  }

  const label = labelParts.join(" ").trim();
  return {
    ...(label.length > 0 ? { label } : {}),
    ...(model ? { model } : {}),
  };
}
