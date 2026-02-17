export type AgentName = "athena" | "hephaestus" | "demeter";

export type AgentKey = "gaia" | AgentName;
export type LeanAgentKey = AgentKey;

export interface AgentEnvelope<TData, TAgent extends AgentKey = AgentName> {
  contract_version: "1.0";
  agent: TAgent;
  work_unit: string;
  session_id: string;
  ok: boolean;
  data: TData;
  errors: string[];
}

export interface GaiaData {
  next_actions: string[];
  delegations: string[];
  summary: string;
}

export interface AthenaData {
  repo_map: string;
  plan: string[];
  risk_list: string[];
  suggested_agents: string[];
}

export interface HephaestusData {
  diff_summary: string;
  files_modified: string[];
  revision_ids: string[];
  notes: string[];
  refactoring_done: string[];
  known_issues: string[];
}

export interface DemeterDecision {
  type: "question" | "rejection" | "mode_switch" | "pair_feedback";
  question: string;
  answer: string;
  rationale?: string;
  impact: string;
}

export interface DemeterStatusReport {
  active_work_units: string[];
  completed_work_units: string[];
  blocked_work_units: string[];
  upcoming_checkpoints: string[];
}

export interface DemeterData {
  log_entry: string;
  decisions: DemeterDecision[];
  learnings: string[];
  plan_updates: string[];
  session_summary: string;
  status_report: DemeterStatusReport;
}

export type GaiaOutput = AgentEnvelope<GaiaData, "gaia">;
export type AthenaOutput = AgentEnvelope<AthenaData, "athena">;
export type HephaestusOutput = AgentEnvelope<HephaestusData, "hephaestus">;
export type DemeterOutput = AgentEnvelope<DemeterData, "demeter">;

export interface AgentModelConfig {
  model: string;
  fallback: string[];
  temperature: number;
  reasoningEffort?: "low" | "medium" | "high";
  thinking?: {
    type: "enabled";
    budgetTokens: number;
  };
  maxTokens?: number;
}

export interface AgentRuntimeConfig {
  modelConfig: AgentModelConfig;
  prompt: string;
  disabled: boolean;
}
