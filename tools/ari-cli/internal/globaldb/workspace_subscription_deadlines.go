package globaldb

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb/dbsqlc"
)

const workspaceTimerPurposeSubscriptionTimeout = "subscription-timeout"

func createSubscriptionDeadlineTimerWithQueries(ctx context.Context, queries *dbsqlc.Queries, subscription EventSubscription) error {
	if subscription.TimeoutAt == nil {
		return nil
	}
	payload, err := json.Marshal(map[string]string{
		"reason":                 "subscription_timeout",
		"subscription_id":        strings.TrimSpace(subscription.SubscriptionID),
		"target_subscription_id": strings.TrimSpace(subscription.SubscriptionID),
	})
	if err != nil {
		return err
	}
	_, err = createWorkspaceTimerWithQueries(ctx, queries, WorkspaceTimer{
		TimerID:              subscriptionDeadlineTimerID(subscription.SubscriptionID),
		WorkspaceID:          subscription.WorkspaceID,
		OwnerSessionID:       subscription.OwnerSessionID,
		TargetSubscriptionID: subscription.SubscriptionID,
		SubjectType:          "event_subscription",
		SubjectID:            subscription.SubscriptionID,
		Purpose:              workspaceTimerPurposeSubscriptionTimeout,
		FireAt:               subscription.TimeoutAt.UTC(),
		PayloadJSON:          string(payload),
		CreatedAt:            subscription.CreatedAt,
		UpdatedAt:            subscription.UpdatedAt,
	})
	return err
}

func cancelSubscriptionDeadlineTimersWithQueries(ctx context.Context, queries *dbsqlc.Queries, subscriptionID string, updatedAt time.Time) error {
	subscriptionID = strings.TrimSpace(subscriptionID)
	if subscriptionID == "" {
		return nil
	}
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}
	_, err := queries.CancelWorkspaceTimersByTargetSubscription(ctx, dbsqlc.CancelWorkspaceTimersByTargetSubscriptionParams{UpdatedAt: updatedAt.UTC().Format(time.RFC3339Nano), TargetSubscriptionID: optionalString(subscriptionID)})
	return err
}

func subscriptionDeadlineTimerID(subscriptionID string) string {
	return "timer-subscription-timeout-" + strings.TrimSpace(subscriptionID)
}

func isSubscriptionDeadlineTimerEvent(event WorkspaceEvent) bool {
	if strings.TrimSpace(event.EventType) != WorkspaceEventTimerFired {
		return false
	}
	payload := WorkspaceEventStringPayload(event.PayloadJSON)
	targetSubscriptionID := strings.TrimSpace(payload["target_subscription_id"])
	if targetSubscriptionID == "" {
		return false
	}
	return strings.TrimSpace(event.SubjectID) == subscriptionDeadlineTimerID(targetSubscriptionID) &&
		strings.TrimSpace(payload["timer_id"]) == subscriptionDeadlineTimerID(targetSubscriptionID) &&
		strings.TrimSpace(payload["reason"]) == "subscription_timeout" &&
		strings.TrimSpace(payload["subject_type"]) == "event_subscription" &&
		strings.TrimSpace(payload["subject_id"]) == targetSubscriptionID
}
