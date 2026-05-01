package balancer

import (
	"errors"
	"sync"
)

type Node struct {
	URL string
}

type DBLocation struct {
	NodeURL string
}

type Balancer struct {
	nodes []Node

	mu      sync.RWMutex
	counter int

	dbMap map[string]DBLocation
}

func New(nodes []string) *Balancer {
	n := make([]Node, len(nodes))
	for i, url := range nodes {
		n[i] = Node{URL: url}
	}

	return &Balancer{
		nodes: n,
		dbMap: make(map[string]DBLocation),
	}
}

func (b *Balancer) NextNode() (Node, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if len(b.nodes) == 0 {
		return Node{}, errors.New("no nodes available")
	}

	node := b.nodes[b.counter%len(b.nodes)]
	b.counter++

	return node, nil
}

func (b *Balancer) RegisterDB(dbID string, node Node) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.dbMap[dbID] = DBLocation{
		NodeURL: node.URL,
	}
}

func (b *Balancer) GetNodeForDB(dbID string) (Node, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	loc, ok := b.dbMap[dbID]
	if !ok {
		return Node{}, errors.New("db not found")
	}

	return Node{URL: loc.NodeURL}, nil
}
