export type DelegateGaiaStatus = "ok" | "retry_succeeded" | "parse_failed";

export interface DelegateGaiaArgs<TParsed> {
  sessionId: string;
  modelUsed: string;
  responseText: string;
  parse: (input: unknown) => TParsed;
  retry?: () => Promise<string>;
}

export interface RejectionFeedbackRequest {
  owner_agent: "gaia";
  paused_agent: string | null;
  question: string;
  reason: string;
}

export interface DelegateGaiaResult<TParsed> {
  session_id: string;
  model_used: string;
  parsed_json: TParsed | null;
  parse_error: string | null;
  status: DelegateGaiaStatus;
  attempt_count: number;
  rejection_feedback_request: RejectionFeedbackRequest | null;
}

const PERMISSION_DENIED_PATTERN = /permission denied|blocked|not allowed|forbidden/i;
const REJECTION_SIGNAL_PATTERN = /\breject(?:ed|ion)?\b|\bdeclin(?:ed|e)\b|\bdisapprov(?:e|ed)\b|\bapproval denied\b/i;
const REJECTION_FEEDBACK_QUESTION = "What should GAIA change after this rejection?";

function parseErrorMessage(error: unknown): string {
  return error instanceof Error ? error.message : "Unknown parse error";
}

function hasPermissionDeniedSignal(responseText: string): boolean {
  return PERMISSION_DENIED_PATTERN.test(responseText);
}

function hasRejectionSignal(responseText: string): boolean {
  return REJECTION_SIGNAL_PATTERN.test(responseText);
}

function withPermissionGuidance(errorMessage: string): string {
  return [
    errorMessage,
    "Permission denied indicates policy guardrail.",
    "Do not retry blocked mutation actions.",
    "Delegate implementation to HEPHAESTUS or return a checkpoint decision.",
  ].join(" ");
}

function withRejectionGuidance(errorMessage: string): string {
  return [
    errorMessage,
    "Rejection signal detected from delegated response.",
    "GAIA must ask the Operator for rejection feedback.",
    "Specialist subagents must not ask follow-up rejection questions.",
    "Ask exactly: What should GAIA change after this rejection?",
    "Capture the answer as a rejection decision entry for DEMETER.",
  ].join(" ");
}

function buildRejectionFeedbackRequest(responseTexts: readonly string[]): RejectionFeedbackRequest | null {
  if (!responseTexts.some((text) => hasRejectionSignal(text))) {
    return null;
  }

  return {
    owner_agent: "gaia",
    paused_agent: null,
    question: REJECTION_FEEDBACK_QUESTION,
    reason: "Delegated response indicates rejection that requires GAIA-owned feedback handling.",
  };
}

function buildParseError(error: unknown, responseTexts: readonly string[]): string {
  let message = parseErrorMessage(error);

  if (responseTexts.some((text) => hasPermissionDeniedSignal(text))) {
    message = withPermissionGuidance(message);
  }

  if (responseTexts.some((text) => hasRejectionSignal(text))) {
    message = withRejectionGuidance(message);
  }

  return message;
}

function extractJsonCandidate(text: string): string {
  const trimmed = text.trim();
  if (trimmed.length === 0) {
    throw new Error("Response is empty");
  }

  const fencedMatch = trimmed.match(/```json\s*([\s\S]*?)\s*```/i);
  if (fencedMatch && fencedMatch[1]) {
    return fencedMatch[1].trim();
  }

  if (trimmed.startsWith("{") && trimmed.endsWith("}")) {
    return trimmed;
  }

  const firstOpen = trimmed.indexOf("{");
  if (firstOpen < 0) {
    throw new Error("No JSON object found in response");
  }

  let depth = 0;
  for (let index = firstOpen; index < trimmed.length; index += 1) {
    const char = trimmed[index];
    if (char === "{") {
      depth += 1;
    } else if (char === "}") {
      depth -= 1;
      if (depth === 0) {
        return trimmed.slice(firstOpen, index + 1);
      }
    }
  }

  throw new Error("Unterminated JSON object in response");
}

function parseDelegateOutput<TParsed>(responseText: string, parse: (input: unknown) => TParsed): TParsed {
  const jsonCandidate = extractJsonCandidate(responseText);
  const parsed = JSON.parse(jsonCandidate) as unknown;
  return parse(parsed);
}

export async function delegateGaia<TParsed>(
  args: DelegateGaiaArgs<TParsed>,
): Promise<DelegateGaiaResult<TParsed>> {
  const initialRejectionRequest = buildRejectionFeedbackRequest([args.responseText]);

  try {
    const first = parseDelegateOutput(args.responseText, args.parse);
    return {
      session_id: args.sessionId,
      model_used: args.modelUsed,
      parsed_json: first,
      parse_error: null,
      status: "ok",
      attempt_count: 1,
      rejection_feedback_request: initialRejectionRequest,
    };
  } catch (firstError) {
    if (!args.retry) {
      const rejectionFeedbackRequest = buildRejectionFeedbackRequest([args.responseText]);
      return {
        session_id: args.sessionId,
        model_used: args.modelUsed,
        parsed_json: null,
        parse_error: buildParseError(firstError, [args.responseText]),
        status: "parse_failed",
        attempt_count: 1,
        rejection_feedback_request: rejectionFeedbackRequest,
      };
    }

    let retryResponse = "";
    try {
      retryResponse = await args.retry();
      const second = parseDelegateOutput(retryResponse, args.parse);
      const rejectionFeedbackRequest = buildRejectionFeedbackRequest([args.responseText, retryResponse]);
      return {
        session_id: args.sessionId,
        model_used: args.modelUsed,
        parsed_json: second,
        parse_error: null,
        status: "retry_succeeded",
        attempt_count: 2,
        rejection_feedback_request: rejectionFeedbackRequest,
      };
    } catch (retryError) {
      const rejectionFeedbackRequest = buildRejectionFeedbackRequest([args.responseText, retryResponse]);
      return {
        session_id: args.sessionId,
        model_used: args.modelUsed,
        parsed_json: null,
        parse_error: buildParseError(retryError, [args.responseText, retryResponse]),
        status: "parse_failed",
        attempt_count: 2,
        rejection_feedback_request: rejectionFeedbackRequest,
      };
    }
  }
}
