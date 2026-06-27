package daemon

import (
	"context"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	aritool "github.com/builtwithtofu/ari/tools/ari-cli/internal/tool"
)

const (
	ariToolTrustApprovedOnce        = aritool.TrustApprovedOnce
	ariToolTrustScopedSourceSession = aritool.TrustScopedSourceSession
)

type (
	AriToolListRequest   = aritool.ListRequest
	AriToolListResponse  = aritool.ListResponse
	AriToolSchema        = aritool.Schema
	AriToolCallRequest   = aritool.CallRequest
	AriToolScope         = aritool.Scope
	AriToolApproval      = aritool.Approval
	AriToolApprovalScope = aritool.ApprovalScope
	AriToolCallResponse  = aritool.CallResponse
	storedAriApproval    = aritool.StoredApproval
)

func HashAriToolRequest(name string, input any) (string, error) {
	return aritool.HashRequest(name, input)
}

func storeAriApproval(ctx context.Context, store *globaldb.Store, approval aritool.StoredApproval) error {
	return aritool.StoreApproval(ctx, store, approval)
}

func validateAndConsumeAriApproval(ctx context.Context, store *globaldb.Store, req aritool.CallRequest) error {
	return aritool.ValidateAndConsumeApproval(ctx, store, req)
}
