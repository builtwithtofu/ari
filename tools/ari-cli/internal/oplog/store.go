package oplog

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

var (
	ErrOperationNotFound  = errors.New("operation not found")
	ErrOperationExists    = errors.New("operation already exists")
	ErrInvalidOperation   = errors.New("invalid operation")
	ErrInvalidEdge        = errors.New("invalid edge")
	ErrInvalidStateUpdate = errors.New("invalid state update")
)

type Store struct {
	mu    sync.RWMutex
	path  string
	nodes map[string]OperationNode
	edges map[string]OperationEdge
}

type persistedStore struct {
	Nodes []OperationNode `json:"nodes"`
	Edges []OperationEdge `json:"edges"`
}

func Open(path string) (*Store, error) {
	store := &Store{
		path:  path,
		nodes: make(map[string]OperationNode),
		edges: make(map[string]OperationEdge),
	}

	if err := store.load(); err != nil {
		return nil, err
	}

	return store, nil
}

func (s *Store) CreateOperation(node OperationNode) error {
	errList := node.Validate()
	if len(errList) > 0 {
		return fmt.Errorf("%w: %v", ErrInvalidOperation, errList[0])
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.nodes[node.OperationID]; exists {
		return fmt.Errorf("%w: %q", ErrOperationExists, node.OperationID)
	}

	s.nodes[node.OperationID] = node
	return s.persistLocked()
}

func (s *Store) LoadOperation(operationID string) (OperationNode, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	node, ok := s.nodes[operationID]
	if !ok {
		return OperationNode{}, fmt.Errorf("%w: %q", ErrOperationNotFound, operationID)
	}

	return node, nil
}

func (s *Store) UpdateOperationState(operationID string, next OperationState, updatedAt string) (OperationNode, error) {
	if next == "" || !isValidOperationState(next) {
		return OperationNode{}, fmt.Errorf("%w: state must be a known value", ErrInvalidStateUpdate)
	}
	if updatedAt == "" {
		return OperationNode{}, fmt.Errorf("%w: updated_at is required", ErrInvalidStateUpdate)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	node, ok := s.nodes[operationID]
	if !ok {
		return OperationNode{}, fmt.Errorf("%w: %q", ErrOperationNotFound, operationID)
	}

	node.State = next
	node.UpdatedAt = updatedAt
	s.nodes[operationID] = node

	if err := s.persistLocked(); err != nil {
		return OperationNode{}, err
	}

	return node, nil
}

func (s *Store) ListOperations() []OperationNode {
	s.mu.RLock()
	defer s.mu.RUnlock()

	nodes := make([]OperationNode, 0, len(s.nodes))
	for _, node := range s.nodes {
		nodes = append(nodes, node)
	}

	sortOperationNodes(nodes)
	return nodes
}

func (s *Store) CreateEdge(edge OperationEdge) error {
	errList := edge.Validate()
	if len(errList) > 0 {
		return fmt.Errorf("%w: %v", ErrInvalidEdge, errList[0])
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, fromExists := s.nodes[edge.FromOperationID]; !fromExists {
		return fmt.Errorf("%w: %q", ErrOperationNotFound, edge.FromOperationID)
	}
	if _, toExists := s.nodes[edge.ToOperationID]; !toExists {
		return fmt.Errorf("%w: %q", ErrOperationNotFound, edge.ToOperationID)
	}

	key := edgeKey(edge)
	s.edges[key] = edge

	return s.persistLocked()
}

func (s *Store) ListEdges() []OperationEdge {
	s.mu.RLock()
	defer s.mu.RUnlock()

	edges := make([]OperationEdge, 0, len(s.edges))
	for _, edge := range s.edges {
		edges = append(edges, edge)
	}

	sortOperationEdges(edges)
	return edges
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	var persisted persistedStore
	if err := json.Unmarshal(data, &persisted); err != nil {
		return fmt.Errorf("decode oplog store: %w", err)
	}

	for _, node := range persisted.Nodes {
		s.nodes[node.OperationID] = node
	}
	for _, edge := range persisted.Edges {
		s.edges[edgeKey(edge)] = edge
	}

	return nil
}

func (s *Store) persistLocked() error {
	state := persistedStore{
		Nodes: make([]OperationNode, 0, len(s.nodes)),
		Edges: make([]OperationEdge, 0, len(s.edges)),
	}

	for _, node := range s.nodes {
		state.Nodes = append(state.Nodes, node)
	}
	for _, edge := range s.edges {
		state.Edges = append(state.Edges, edge)
	}

	sortOperationNodes(state.Nodes)
	sortOperationEdges(state.Edges)

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}

	return os.WriteFile(s.path, data, 0o644)
}

func edgeKey(edge OperationEdge) string {
	return edge.FromOperationID + "->" + edge.ToOperationID
}
