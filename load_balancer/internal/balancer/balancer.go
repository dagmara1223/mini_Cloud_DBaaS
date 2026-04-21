package balancer

import (
	"errors"
	"sync"
)

type Node struct {
	URL string
}

type Metrics struct {
	DBCount    int     `json:"db_count"`
	ActiveDBs  int     `json:"active_dbs"`
	CPUPercent float64 `json:"cpu_percent"`
	MemPercent float64 `json:"mem_percent"`
}

type Balancer struct {
	nodes []Node

	mu      sync.RWMutex
	counter int
}

func New(nodes []string) *Balancer {
	n := make([]Node, len(nodes))
	for i, url := range nodes {
		n[i] = Node{URL: url}
	}

	return &Balancer{
		nodes: n,
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
