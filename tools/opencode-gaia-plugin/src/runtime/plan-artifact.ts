import { z } from "zod";

const PlanArtifactSchema = z.object({
  objective: z.string().min(1),
  constraints: z.array(z.string().min(1)),
  done_when: z.array(z.string().min(1)),
  risk_level: z.enum(["low", "medium", "high"]),
  open_questions: z.array(z.string().min(1)),
  verification_steps: z.array(z.string().min(1)),
  non_goals: z.array(z.string().min(1)),
});

export type PlanArtifact = z.infer<typeof PlanArtifactSchema>;

function renderSection(title: string, items: string[]): string[] {
  if (items.length === 0) {
    return [`## ${title}`, "- none", ""];
  }

  return [`## ${title}`, ...items.map((item) => `- ${item}`), ""];
}

export function validatePlanArtifact(input: unknown): PlanArtifact {
  return PlanArtifactSchema.parse(input);
}

export function renderPlanArtifactMarkdown(input: unknown): string {
  const artifact = validatePlanArtifact(input);

  const lines = [
    "# Work Unit Plan Artifact",
    "",
    "## Objective",
    artifact.objective,
    "",
    ...renderSection("Constraints", artifact.constraints),
    ...renderSection("Done When", artifact.done_when),
    "## Risk Level",
    artifact.risk_level,
    "",
    ...renderSection("Open Questions", artifact.open_questions),
    ...renderSection("Verification Steps", artifact.verification_steps),
    ...renderSection("Non-Goals", artifact.non_goals),
  ];

  return `${lines.join("\n")}\n`;
}

export function buildPlanArtifactTemplateMarkdown(): string {
  return renderPlanArtifactMarkdown({
    objective: "Define the objective for this work unit.",
    constraints: ["List hard constraints that must hold."],
    done_when: ["List acceptance criteria for completion."],
    risk_level: "medium",
    open_questions: ["List unanswered decisions requiring input."],
    verification_steps: ["bun run typecheck", "bun test"],
    non_goals: ["List out-of-scope items."],
  });
}
