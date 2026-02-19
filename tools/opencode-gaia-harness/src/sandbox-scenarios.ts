import { mkdir, writeFile } from "node:fs/promises";
import { dirname, join } from "node:path";

interface ScenarioFile {
  relativePath: string;
  content: string;
}

const SCENARIO_FILES: readonly ScenarioFile[] = [
  {
    relativePath: "go-hello-planning/README.md",
    content: `# Scenario: Go Hello World Planning

Goal: practice start-plan -> execute-plan behavior on a small Go task.

Suggested flow:
- Start in planning mode and ask clarifying questions.
- Propose a small plan to add a --name flag for greeting output.
- Implement after plan approval and add tests.

Acceptance target:
- \`go run ./cmd/hello --name Gaia\` prints \`Hello, Gaia!\`.
- \`go test ./...\` passes.
`,
  },
  {
    relativePath: "go-hello-planning/go.mod",
    content: `module example.com/go-hello-planning

go 1.22.0
`,
  },
  {
    relativePath: "go-hello-planning/cmd/hello/main.go",
    content: `package main

import "fmt"

func main() {
  fmt.Println("Hello, world!")
}
`,
  },
  {
    relativePath: "planning-challenge/README.md",
    content: `# Scenario: Complex Planning Sandbox

Goal: produce a staged implementation plan before coding.

Context:
- Current docs describe a fragile payments service with limited observability.
- Team wants a phased migration with minimal downtime and explicit rollback options.

Suggested prompts:
- "Draft a phased plan with risk gates and rollout criteria."
- "Ask me targeted questions before proposing final sequence."
- "Expand only phase 2 with concrete validation steps."
`,
  },
  {
    relativePath: "planning-challenge/docs/current-state.md",
    content: `# Current State

- Payment writes and read models are coupled in one service process.
- Retry policy uses fixed retry with no jitter.
- Deployment is single region with manual rollback.
- Mean checkout latency: 410ms p95, with spikes above 900ms during promotions.
`,
  },
  {
    relativePath: "planning-challenge/docs/targets.md",
    content: `# Targets

- Reduce p95 checkout latency under 250ms.
- Add safe progressive rollout for payment code changes.
- Add recoverable checkpoints to continue after operator interruption.
- Keep incident rollback under 10 minutes.
`,
  },
  {
    relativePath: "planning-challenge/docs/constraints.md",
    content: `# Constraints

- No downtime window available.
- Keep API response schema stable for the mobile app.
- Team can only run one migration wave per week.
- Compliance team requires explicit audit records for payment routing changes.
`,
  },
  {
    relativePath: "refactor-sandbox/README.md",
    content: `# Scenario: Refactoring Sandbox

Goal: improve code readability and reuse without changing behavior.

Refactor tasks:
- Remove duplicated normalization logic in \`internal/legacy/formatter.go\`.
- Keep behavior stable with exact assertions.

Acceptance target:
- \`go test ./...\` remains green.
- No API or output behavior changes.
`,
  },
  {
    relativePath: "refactor-sandbox/go.mod",
    content: `module example.com/refactor-sandbox

go 1.22.0
`,
  },
  {
    relativePath: "refactor-sandbox/internal/legacy/formatter.go",
    content: `package legacy

import "strings"

func FormatUserName(input string) string {
  normalized := strings.TrimSpace(input)
  normalized = strings.ToLower(normalized)
  normalized = strings.ReplaceAll(normalized, "_", "-")
  return normalized
}

func FormatServiceName(input string) string {
  normalized := strings.TrimSpace(input)
  normalized = strings.ToLower(normalized)
  normalized = strings.ReplaceAll(normalized, "_", "-")
  return normalized
}
`,
  },
  {
    relativePath: "refactor-sandbox/internal/legacy/formatter_test.go",
    content: `package legacy

import "testing"

func TestFormatUserName(t *testing.T) {
  got := FormatUserName("  Alice_Admin  ")
  if got != "alice-admin" {
    t.Fatalf("expected alice-admin, got %s", got)
  }
}

func TestFormatServiceName(t *testing.T) {
  got := FormatServiceName("  Billing_API  ")
  if got != "billing-api" {
    t.Fatalf("expected billing-api, got %s", got)
  }
}
`,
  },
  {
    relativePath: "bug-hunt/README.md",
    content: `# Scenario: Find and Fix Bug Sandbox

Goal: triage a real bug report, add a reproducer test first, then fix.

Workflow expectation:
- Read \`bug-report.md\`.
- Add failing reproducer test with exact assertions.
- Implement the smallest fix.
- Run \`go test ./...\` and summarize regression protection.
`,
  },
  {
    relativePath: "bug-hunt/go.mod",
    content: `module example.com/bug-hunt

go 1.22.0
`,
  },
  {
    relativePath: "bug-hunt/bug-report.md",
    content: `# Bug Report: Loyalty discount ignored for large carts

Observed behavior:
- Orders over 1000 with loyalty tier discounts are billed without discount.

Repro steps:
1. Run checkout discount calculation with total=1500 and loyaltyTier=2.
2. Observe returned total remains 1500.

Expected:
- A 10% loyalty discount applies.
- Expected total: 1350.

Notes:
- Existing tests pass; likely missing edge-case coverage.
`,
  },
  {
    relativePath: "bug-hunt/internal/checkout/discount.go",
    content: `package checkout

func ApplyLoyaltyDiscount(total int, loyaltyTier int) int {
  if loyaltyTier <= 0 {
    return total
  }

  discountPercent := loyaltyTier * 5
  if discountPercent > 25 {
    discountPercent = 25
  }

  discounted := total - (total*discountPercent)/100

  if total > 1000 {
    discounted = total // BUG: discount gets dropped for large orders
  }

  if discounted < 0 {
    return 0
  }

  return discounted
}
`,
  },
  {
    relativePath: "bug-hunt/internal/checkout/discount_test.go",
    content: `package checkout

import "testing"

func TestApplyLoyaltyDiscount_NoTier(t *testing.T) {
  got := ApplyLoyaltyDiscount(200, 0)
  if got != 200 {
    t.Fatalf("expected 200, got %d", got)
  }
}

func TestApplyLoyaltyDiscount_CappedTier(t *testing.T) {
  got := ApplyLoyaltyDiscount(200, 10)
  if got != 150 {
    t.Fatalf("expected 150, got %d", got)
  }
}
`,
  },
];

export async function seedSandboxScenarios(workspacePath: string): Promise<void> {
  for (const file of SCENARIO_FILES) {
    const targetPath = join(workspacePath, file.relativePath);
    await mkdir(dirname(targetPath), { recursive: true });
    await writeFile(targetPath, file.content, "utf8");
  }
}
