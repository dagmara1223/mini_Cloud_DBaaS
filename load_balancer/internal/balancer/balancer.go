package balancer

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"sync"
	"time"
)

type Strategy int

const (
	RoundRobin Strategy = iota
	LeastLoaded
)

type Node struct {
	URL     string
	Healthy bool

	DBCount     int64
	CPUUsage    float64
	MemoryUsage float64
}

type DBLocation struct {
	NodeURL string
}

type Balancer struct {
	nodes []Node

	mu      sync.RWMutex
	counter int

	strategy Strategy

	dbMap map[string]DBLocation
}

func New(nodes []string, strategy Strategy) *Balancer {
	n := make([]Node, len(nodes))
	for i, url := range nodes {
		n[i] = Node{
			URL:     url,
			Healthy: true,
		}
	}

	return &Balancer{
		nodes:    n,
		dbMap:    make(map[string]DBLocation),
		strategy: strategy,
	}
}

func (b *Balancer) GetNodePtr(n Node) *Node {
	for i := range b.nodes {
		if b.nodes[i].URL == n.URL {
			return &b.nodes[i]
		}
	}
	return nil
}

func (b *Balancer) SelectNode() (Node, error) {
	switch b.strategy {
	case RoundRobin:
		return b.nextRoundRobin()
	case LeastLoaded:
		return b.bestByMetrics()
	default:
		return Node{}, errors.New("unknown strategy")
	}
}

func (b *Balancer) refreshMetrics() {
	ticker := time.NewTicker(2 * time.Second)

	client := &http.Client{Timeout: 2 * time.Second}

	for range ticker.C {
		b.mu.Lock()

		for i := range b.nodes {
			n := &b.nodes[i]

			resp, err := client.Get(n.URL + "/metrics")
			if err != nil {
				n.Healthy = false
				continue
			}

			var m struct {
				DBCount     int64   `json:"db_count"`
				CPUUsage    float64 `json:"cpu_usage"`
				MemoryUsage float64 `json:"memory_usage"`
			}

			if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
				resp.Body.Close()
				n.Healthy = false
				continue
			}
			resp.Body.Close()

			n.DBCount = m.DBCount
			n.CPUUsage = m.CPUUsage
			n.MemoryUsage = m.MemoryUsage
			n.Healthy = true
		}

		b.mu.Unlock()
	}
}

func (b *Balancer) nextRoundRobin() (Node, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if len(b.nodes) == 0 {
		return Node{}, errors.New("no nodes available")
	}

	node := b.nodes[b.counter%len(b.nodes)]
	b.counter++

	return node, nil
}

func (b *Balancer) bestByMetrics() (Node, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if len(b.nodes) == 0 {
		return Node{}, errors.New("no nodes available")
	}

	var best Node
	bestScore := math.MaxFloat64

	for _, n := range b.nodes {
		if !n.Healthy {
			continue
		}

		score :=
			n.CPUUsage*50 +
				n.MemoryUsage*30 +
				float64(n.DBCount)*20

		if score < bestScore {
			bestScore = score
			best = n
		}
	}

	if best.URL == "" {
		return Node{}, errors.New("no healthy nodes")
	}

	return best, nil
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
