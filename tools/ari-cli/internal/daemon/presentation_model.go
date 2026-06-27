package daemon

import (
	"strings"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
)

type PresentationStatus string

const (
	PresentationStatusReady     PresentationStatus = "ready"
	PresentationStatusRunning   PresentationStatus = "running"
	PresentationStatusNeedsAuth PresentationStatus = "needs_auth"
	PresentationStatusBlocked   PresentationStatus = "blocked"
	PresentationStatusFailed    PresentationStatus = "failed"
	PresentationStatusStopped   PresentationStatus = "stopped"
	PresentationStatusUnknown   PresentationStatus = "unknown"
)

type PresentationBadge struct {
	Label string `json:"label"`
	Tone  string `json:"tone,omitempty"`
}

type PresentationSource struct {
	Kind         string            `json:"kind,omitempty"`
	ID           string            `json:"id,omitempty"`
	Harness      string            `json:"harness,omitempty"`
	Provider     string            `json:"provider,omitempty"`
	Model        string            `json:"model,omitempty"`
	NativeKind   string            `json:"native_kind,omitempty"`
	NativeStatus string            `json:"native_status,omitempty"`
	Fields       map[string]string `json:"fields,omitempty"`
}

type Presentation struct {
	Label       string              `json:"label,omitempty"`
	ShortLabel  string              `json:"short_label,omitempty"`
	Status      PresentationStatus  `json:"status,omitempty"`
	StatusLabel string              `json:"status_label,omitempty"`
	Summary     string              `json:"summary,omitempty"`
	Detail      string              `json:"detail,omitempty"`
	NextStep    string              `json:"next_step,omitempty"`
	Badges      []PresentationBadge `json:"badges,omitempty"`
	Source      *PresentationSource `json:"source,omitempty"`
}

func presentationForStatus(raw string) Presentation {
	status, label := normalizePresentationStatus(raw)
	return Presentation{Status: status, StatusLabel: label, Source: nativeStatusSource(raw)}
}

func presentationWithLabel(label, rawStatus string) Presentation {
	p := presentationForStatus(rawStatus)
	p.Label = strings.TrimSpace(label)
	if p.Summary == "" {
		p.Summary = p.Label
	}
	return p
}

func normalizePresentationStatus(raw string) (PresentationStatus, string) {
	switch strings.TrimSpace(raw) {
	case "ready", "waiting", "passed", "authenticated", "ok", "active":
		return PresentationStatusReady, "Ready"
	case "running", "in_progress", "auth_in_progress", "attempted", "pending":
		return PresentationStatusRunning, "Running"
	case "auth_required", "needs_auth", "not_installed":
		return PresentationStatusNeedsAuth, "Needs auth"
	case "action-required", "blocked", "reattach_required", "requires_approval":
		return PresentationStatusBlocked, "Blocked"
	case "failed", "auth_failed", "lost", "expired", "timed_out":
		return PresentationStatusFailed, "Failed"
	case "stopped", "completed", "exited", "removed", "suspended", "inactive", "none":
		return PresentationStatusStopped, "Stopped"
	case "unknown", "":
		return PresentationStatusUnknown, "Unknown"
	default:
		return PresentationStatusUnknown, titleFromIdentifier(raw)
	}
}

func nativeStatusSource(raw string) *PresentationSource {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	return &PresentationSource{NativeStatus: raw}
}

func titleFromIdentifier(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "Unknown"
	}
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == '_' || r == '-' || r == '.' })
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}

func displayOrID(label, id string) string {
	label = strings.TrimSpace(label)
	if label != "" {
		return label
	}
	return strings.TrimSpace(id)
}

func (d *Daemon) sessionPresentation(sessionID, name, harness, usage, rawStatus string) Presentation {
	p := presentationWithLabel(displayOrID(name, sessionID), rawStatus)
	p.Summary = p.Label
	if usage == globaldb.HarnessSessionUsageEphemeral && p.Status == PresentationStatusRunning {
		p.Summary = "Ephemeral call is running"
		p.Badges = append(p.Badges, PresentationBadge{Label: "Ephemeral call", Tone: "info"})
	} else if usage == globaldb.HarnessSessionUsageSticky {
		p.Badges = append(p.Badges, PresentationBadge{Label: "Sticky session", Tone: "info"})
	}
	if p.Source == nil {
		p.Source = &PresentationSource{}
	}
	p.Source.Kind = "harness_session"
	p.Source.ID = strings.TrimSpace(sessionID)
	p.Source.Harness = strings.TrimSpace(harness)
	p.Source.Provider = strings.TrimSpace(harness)
	return p
}

func authStatusPresentation(status HarnessAuthStatus) Presentation {
	p := presentationWithLabel(authDisplayNameFromStatus(status), string(status.Status))
	p.Source = &PresentationSource{Kind: "auth_slot", ID: strings.TrimSpace(status.AuthSlotID), Harness: strings.TrimSpace(status.Harness), Provider: strings.TrimSpace(status.Harness), NativeStatus: string(status.Status)}
	switch status.Status {
	case HarnessAuthAuthenticated:
		p.Summary = "Auth is ready"
	case HarnessAuthRequired:
		p.Summary = "Auth is needed before this harness can run"
	case HarnessAuthInProgress:
		p.Summary = "Auth is in progress"
	case HarnessAuthFailed:
		p.Summary = "Auth failed and needs attention"
	case HarnessAuthNotInstalled:
		p.Summary = "Harness is not installed"
	default:
		p.Summary = "Auth status is unknown"
	}
	return p
}

func authDisplayNameFromStatus(status HarnessAuthStatus) string {
	if strings.TrimSpace(status.Name) != "" {
		return strings.TrimSpace(status.Name)
	}
	if strings.TrimSpace(status.AuthSlotID) == strings.TrimSpace(status.Harness)+"-default" {
		return "default"
	}
	return strings.TrimSpace(status.AuthSlotID)
}

func presentHarnessSession(session HarnessSession) HarnessSession {
	session.Presentation = (&Daemon{}).sessionPresentation(session.SessionID, "", session.Executor, session.Usage, session.Status)
	if session.Presentation.Label == "" {
		session.Presentation.Label = displayOrID(session.HarnessSessionID, session.ProviderSessionID)
		session.Presentation.Summary = session.Presentation.Label
	}
	if session.Presentation.Source == nil {
		session.Presentation.Source = &PresentationSource{}
	}
	session.Presentation.Source.Fields = map[string]string{
		"provider_session_id": strings.TrimSpace(session.ProviderSessionID),
		"provider_run_id":     strings.TrimSpace(session.ProviderRunID),
		"invocation_mode":     strings.TrimSpace(session.InvocationMode),
		"usage_bucket":        strings.TrimSpace(session.UsageBucket),
	}
	return session
}

func presentTimelineItem(item TimelineItem) TimelineItem {
	label := strings.TrimSpace(item.Text)
	if label == "" {
		label = titleFromIdentifier(item.Kind)
	}
	item.Presentation = presentationWithLabel(label, item.Status)
	item.Presentation.Source = &PresentationSource{Kind: strings.TrimSpace(item.SourceKind), ID: strings.TrimSpace(item.SourceID), NativeKind: strings.TrimSpace(item.Kind), NativeStatus: strings.TrimSpace(item.Status)}
	return item
}

func presentAuthStatus(status HarnessAuthStatus) HarnessAuthStatus {
	status.Presentation = authStatusPresentation(status)
	return status
}

func presentAuthSlot(slot AuthSlotResponse) AuthSlotResponse {
	status := HarnessAuthStatus{Harness: slot.Harness, Name: slot.Label, AuthSlotID: slot.AuthSlotID, Status: HarnessAuthState(slot.Status), AriSecretStorage: HarnessAriSecretStorageNone}
	slot.Presentation = authStatusPresentation(status)
	if strings.TrimSpace(slot.ProviderLabel) != "" {
		slot.Presentation.Detail = "Provider account: " + strings.TrimSpace(slot.ProviderLabel)
	}
	return slot
}

func (d *Daemon) presentAuthDiagnostic(diagnostic HarnessAuthDiagnostic) HarnessAuthDiagnostic {
	diagnostic.DefaultSlot = presentAuthStatus(diagnostic.DefaultSlot)
	for i := range diagnostic.NamedSlots {
		diagnostic.NamedSlots[i] = presentAuthSlot(diagnostic.NamedSlots[i])
	}
	diagnostic.Presentation = authStatusPresentation(diagnostic.DefaultSlot)
	diagnostic.Presentation.Label = d.harnessDisplayName(diagnostic.Harness)
	diagnostic.Presentation.ShortLabel = strings.TrimSpace(diagnostic.Harness)
	diagnostic.Presentation.NextStep = strings.TrimSpace(diagnostic.NextStep)
	if !diagnostic.Installed {
		diagnostic.Presentation.Status = PresentationStatusNeedsAuth
		diagnostic.Presentation.StatusLabel = "Needs auth"
		diagnostic.Presentation.Summary = "Install " + diagnostic.Presentation.Label
	}
	return diagnostic
}

func (d *Daemon) presentWorkspaceStatus(response WorkspaceStatusResponse) WorkspaceStatusResponse {
	response.Presentation = presentationWithLabel(displayOrID(response.WorkspaceName, response.WorkspaceID), response.Attention.Level)
	response.Presentation.Source = &PresentationSource{Kind: "workspace", ID: response.WorkspaceID, NativeStatus: response.Attention.Level}
	response.VCS = presentDiffSummary(response.VCS)
	response.Attention = presentAttention(response.Attention)
	for i := range response.Processes {
		response.Processes[i] = presentProcessActivity(response.Processes[i])
	}
	for i := range response.Sessions {
		response.Sessions[i] = d.presentSessionActivity(response.Sessions[i])
	}
	for i := range response.AgentMessages {
		response.AgentMessages[i] = presentAgentMessageActivity(response.AgentMessages[i])
	}
	for i := range response.FanoutMembers {
		response.FanoutMembers[i] = presentFanoutMemberActivity(response.FanoutMembers[i])
	}
	for i := range response.Inbox {
		response.Inbox[i] = presentInboxActivity(response.Inbox[i])
	}
	for i := range response.Proofs {
		response.Proofs[i] = presentProofResult(response.Proofs[i])
	}
	for i := range response.RecentOperations {
		response.RecentOperations[i] = presentOperationActivity(response.RecentOperations[i])
	}
	for i := range response.RecentTimeline {
		response.RecentTimeline[i] = presentTimelineItem(response.RecentTimeline[i])
	}
	return response
}

func presentDiffSummary(diff DiffSummary) DiffSummary {
	label := "No VCS"
	if strings.TrimSpace(diff.Backend) != "" && diff.Backend != "none" {
		label = strings.ToUpper(diff.Backend)
	}
	diff.Presentation = Presentation{Label: label, ShortLabel: strings.TrimSpace(diff.Backend), Status: PresentationStatusReady, StatusLabel: "Ready", Summary: label, Source: &PresentationSource{Kind: "vcs", NativeKind: strings.TrimSpace(diff.Backend)}}
	if diff.Error != "" {
		diff.Presentation.Status = PresentationStatusFailed
		diff.Presentation.StatusLabel = "Failed"
		diff.Presentation.Detail = diff.Error
	}
	return diff
}

func presentAttention(attention AttentionSummary) AttentionSummary {
	attention.Presentation = presentationWithLabel("Attention", attention.Level)
	switch attention.Level {
	case "none":
		attention.Presentation.Summary = "Nothing needs attention"
	case "auth":
		attention.Presentation.Status = PresentationStatusNeedsAuth
		attention.Presentation.StatusLabel = "Needs auth"
		attention.Presentation.Summary = "Auth is needed"
	case "running":
		attention.Presentation.Summary = "Work is running"
	case "action-required":
		attention.Presentation.Status = PresentationStatusBlocked
		attention.Presentation.StatusLabel = "Blocked"
		attention.Presentation.Summary = "Action is required"
	}
	for i := range attention.Items {
		attention.Items[i].Presentation = presentationWithLabel(attention.Items[i].Message, attention.Items[i].Kind)
		attention.Items[i].Presentation.Source = &PresentationSource{Kind: "attention", ID: attention.Items[i].SourceID, NativeKind: attention.Items[i].Kind}
	}
	return attention
}

func presentProcessActivity(activity ProcessActivity) ProcessActivity {
	activity.Presentation = presentationWithLabel(activity.Label, activity.Status)
	activity.Presentation.Source = &PresentationSource{Kind: strings.TrimSpace(activity.Kind), ID: strings.TrimSpace(activity.ID), NativeStatus: strings.TrimSpace(activity.Status)}
	if activity.OutputSummary != "" {
		activity.Presentation.Detail = activity.OutputSummary
	}
	return activity
}

func (d *Daemon) presentSessionActivity(activity SessionActivity) SessionActivity {
	activity.Presentation = d.sessionPresentation(activity.ID, activity.Name, activity.Executor, activity.Usage, activity.Status)
	if activity.OutputSummary != "" {
		activity.Presentation.Detail = activity.OutputSummary
	}
	return activity
}

func presentAgentMessageActivity(activity AgentMessageActivity) AgentMessageActivity {
	activity.Presentation = presentationWithLabel("Session message", activity.Status)
	activity.Presentation.Source = &PresentationSource{Kind: "agent_message", ID: activity.AgentMessageID, NativeStatus: activity.Status}
	return activity
}

func presentFanoutMemberActivity(activity FanoutMemberActivity) FanoutMemberActivity {
	activity.Presentation = presentationWithLabel("Fanout worker", activity.Status)
	activity.Presentation.Source = &PresentationSource{Kind: "fanout_member", ID: activity.FanoutMemberID, NativeStatus: activity.Status}
	return activity
}

func presentInboxActivity(activity InboxActivity) InboxActivity {
	label := activity.Summary
	if label == "" {
		label = titleFromIdentifier(activity.Kind)
	}
	activity.Presentation = presentationWithLabel(label, activity.Status)
	activity.Presentation.Source = &PresentationSource{Kind: "inbox", ID: activity.InboxItemID, NativeKind: activity.Kind, NativeStatus: activity.Status}
	return activity
}

func presentProofResult(proof ProofResultSummary) ProofResultSummary {
	proof.Presentation = presentationWithLabel(proof.Command, proof.Status)
	proof.Presentation.Source = &PresentationSource{Kind: proof.SourceKind, ID: proof.SourceID, NativeStatus: proof.Status}
	if proof.LogSummary != "" {
		proof.Presentation.Detail = proof.LogSummary
	}
	return proof
}

func presentOperationActivity(activity OperationActivity) OperationActivity {
	activity.Presentation = presentationWithLabel(activity.RequestSummary, activity.Status)
	activity.Presentation.Source = &PresentationSource{Kind: activity.OperationType, ID: activity.OperationID, NativeStatus: activity.Status}
	return activity
}

func presentWorkspaceSummary(summary WorkspaceSummary) WorkspaceSummary {
	summary.Presentation = presentationWithLabel(displayOrID(summary.Name, summary.WorkspaceID), summary.Status)
	summary.Presentation.Source = &PresentationSource{Kind: "workspace", ID: summary.WorkspaceID, NativeStatus: summary.Status}
	return summary
}

func presentWorkspaceGet(response WorkspaceGetResponse) WorkspaceGetResponse {
	response.Presentation = presentationWithLabel(displayOrID(response.Name, response.WorkspaceID), response.Status)
	response.Presentation.Source = &PresentationSource{Kind: "workspace", ID: response.WorkspaceID, NativeStatus: response.Status}
	return response
}

func presentCommandSummary(summary CommandSummary) CommandSummary {
	summary.Presentation = presentationWithLabel(summary.Command, summary.Status)
	summary.Presentation.Source = &PresentationSource{Kind: "command", ID: summary.CommandID, NativeStatus: summary.Status}
	return summary
}

func presentCommandRun(response CommandRunResponse) CommandRunResponse {
	response.Presentation = presentationWithLabel("Command", response.Status)
	response.Presentation.Source = &PresentationSource{Kind: "command", ID: response.CommandID, NativeStatus: response.Status}
	return response
}

func presentCommandGet(response CommandGetResponse) CommandGetResponse {
	response.Presentation = presentationWithLabel(response.Command, response.Status)
	response.Presentation.Source = &PresentationSource{Kind: "command", ID: response.CommandID, NativeStatus: response.Status}
	return response
}

func presentCommandStop(response CommandStopResponse) CommandStopResponse {
	response.Presentation = presentationWithLabel("Command", response.Status)
	response.Presentation.Source = &PresentationSource{Kind: "command", NativeStatus: response.Status}
	return response
}

func presentFinalResponse(response FinalResponseResponse) FinalResponseResponse {
	response.Presentation = presentationWithLabel("Final response", response.Status)
	response.Presentation.Source = &PresentationSource{Kind: "final_response", ID: response.FinalResponseID, NativeStatus: response.Status}
	return response
}
