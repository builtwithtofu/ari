export interface PromptQualityCheck {
  name:
    | "file-mutation-block"
    | "direct-tool-block"
    | "permission-denied-guidance"
    | "no-retry-blocked-mutations"
    | "delegate-to-hephaestus";
  ok: boolean;
  detail: string;
}

export interface PromptQualityResult {
  ok: boolean;
  checks: PromptQualityCheck[];
}

interface RequiredPhrase {
  name: PromptQualityCheck["name"];
  phrase: string;
  detail: string;
}

const REQUIRED_PHRASES: readonly RequiredPhrase[] = [
  {
    name: "file-mutation-block",
    phrase: "Do not edit or write files directly",
    detail: "Prompt blocks direct file mutation",
  },
  {
    name: "direct-tool-block",
    phrase: "Never call edit or write tools directly",
    detail: "Prompt blocks direct tool-level edit/write calls",
  },
  {
    name: "permission-denied-guidance",
    phrase: "permission-denied",
    detail: "Prompt explains permission-denied behavior",
  },
  {
    name: "no-retry-blocked-mutations",
    phrase: "Do not retry blocked mutation actions",
    detail: "Prompt prevents repeated blocked mutation retries",
  },
  {
    name: "delegate-to-hephaestus",
    phrase: "delegate implementation to HEPHAESTUS",
    detail: "Prompt routes implementation work to HEPHAESTUS",
  },
] as const;

export function evaluateGaiaPromptQuality(prompt: string): PromptQualityResult {
  const checks = REQUIRED_PHRASES.map((required) => ({
    name: required.name,
    ok: prompt.includes(required.phrase),
    detail: required.detail,
  } satisfies PromptQualityCheck));

  return {
    ok: checks.every((check) => check.ok),
    checks,
  };
}
