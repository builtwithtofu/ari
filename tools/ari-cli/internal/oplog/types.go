package oplog

import (
	"fmt"
	"sort"
)

type OperationState string

const (
	OperationStatePending         OperationState = "pending"
	OperationStateRunning         OperationState = "running"
	OperationStateWaitingApproval OperationState = "waiting_approval"
	OperationStateCompleted       OperationState = "completed"
	OperationStateFailed          OperationState = "failed"
	OperationStateRejected        OperationState = "rejected"
	OperationStateKilled          OperationState = "killed"
)

type OperationNode struct {
	OperationID string         `json:"operation_id"`
	State       OperationState `json:"state"`
	Goal        string         `json:"goal"`
	CreatedAt   string         `json:"created_at"`
	UpdatedAt   string         `json:"updated_at"`
}

type OperationEdge struct {
	FromOperationID string `json:"from_operation_id"`
	ToOperationID   string `json:"to_operation_id"`
}

func (n OperationNode) Validate() []error {
	errList := make([]error, 0)

	if n.OperationID == "" {
		errList = append(errList, fmt.Errorf("oplog.operation.operation_id is required"))
	}
	if n.State == "" {
		errList = append(errList, fmt.Errorf("oplog.operation.state is required"))
	}
	if n.Goal == "" {
		errList = append(errList, fmt.Errorf("oplog.operation.goal is required"))
	}
	if n.CreatedAt == "" {
		errList = append(errList, fmt.Errorf("oplog.operation.created_at is required"))
	}
	if n.UpdatedAt == "" {
		errList = append(errList, fmt.Errorf("oplog.operation.updated_at is required"))
	}

	if n.State != "" && !isValidOperationState(n.State) {
		errList = append(errList, fmt.Errorf("oplog.operation.state must be one of: pending, running, waiting_approval, completed, failed, rejected, killed"))
	}

	return errList
}

func (e OperationEdge) Validate() []error {
	errList := make([]error, 0)

	if e.FromOperationID == "" {
		errList = append(errList, fmt.Errorf("oplog.edge.from_operation_id is required"))
	}
	if e.ToOperationID == "" {
		errList = append(errList, fmt.Errorf("oplog.edge.to_operation_id is required"))
	}
	if e.FromOperationID != "" && e.ToOperationID != "" && e.FromOperationID == e.ToOperationID {
		errList = append(errList, fmt.Errorf("oplog.edge.self_references are not allowed"))
	}

	return errList
}

func sortOperationNodes(nodes []OperationNode) {
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].CreatedAt == nodes[j].CreatedAt {
			return nodes[i].OperationID < nodes[j].OperationID
		}
		return nodes[i].CreatedAt < nodes[j].CreatedAt
	})
}

func sortOperationEdges(edges []OperationEdge) {
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].FromOperationID == edges[j].FromOperationID {
			return edges[i].ToOperationID < edges[j].ToOperationID
		}
		return edges[i].FromOperationID < edges[j].FromOperationID
	})
}

func isValidOperationState(state OperationState) bool {
	switch state {
	case OperationStatePending,
		OperationStateRunning,
		OperationStateWaitingApproval,
		OperationStateCompleted,
		OperationStateFailed,
		OperationStateRejected,
		OperationStateKilled:
		return true
	default:
		return false
	}
}
