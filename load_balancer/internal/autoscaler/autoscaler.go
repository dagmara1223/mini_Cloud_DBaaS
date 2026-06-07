package autoscaler

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/nkucht4/load_balancer/internal/balancer"
	"github.com/nkucht4/load_balancer/internal/metadata"
)

type Autoscaler struct {
	lb    *balancer.Balancer
	store *metadata.Store

	CPUHighThreshold float64
	CPULowThreshold  float64

	MaxReplicasPerDB int
	Interval         time.Duration
	client           *http.Client
}

var lastScaleUp = map[string]time.Time{}
var lastScaleDown = map[string]time.Time{}

func New(lb *balancer.Balancer, store *metadata.Store, threshold float64) *Autoscaler {
	return &Autoscaler{
		lb:                lb,
		store:             store,
		CPUHighThreshold:  threshold,
		CPULowThreshold:   threshold * 0.4,
		MaxReplicasPerDB:  3,
		Interval:          10 * time.Second,
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

func (a *Autoscaler) Start() {
	log.Println("[AUTOSCALER] started")

	ticker := time.NewTicker(a.Interval)
	for range ticker.C {
		a.runOnce()
	}
}

func (a *Autoscaler) runOnce() {
	nodes := a.lb.GetNodes()

	for _, node := range nodes {
		if !node.Healthy {
			continue
		}

		if node.CPUUsage > a.CPUHighThreshold {
			a.scaleUp(node)
		} else if node.CPUUsage < a.CPULowThreshold {
			a.scaleDown(node)
		}
	}
}

func (a *Autoscaler) scaleUp(node balancer.Node) {
	if t, ok := lastScaleUp[node.URL]; ok && time.Since(t) < 60*time.Second {
		return
	}
	lastScaleUp[node.URL] = time.Now()

	dbs, err := a.store.GetDBsByNode(node.URL)
	if err != nil {
		log.Println("[AUTOSCALER] GetDBsByNode error:", err)
		return
	}

	for _, db := range dbs {

		if len(db.ReplicaNodeIDs) >= a.MaxReplicasPerDB {
			continue
		}

		target, err := a.selectTargetNode(node.URL)
		if err != nil {
			log.Println("[AUTOSCALER] no target node:", err)
			return
		}

		if contains(db.ReplicaNodeIDs, target.URL) {
			continue
		}

		reqBody := map[string]string{
			"target_node": target.URL,
		}

		body, _ := json.Marshal(reqBody)

		url := node.URL + "/databases/" + db.DBID + "/replica"

		resp, err := a.client.Post(url, "application/json", bytes.NewBuffer(body))
		if err != nil {
			log.Println("[AUTOSCALER] scale up failed:", err)
			continue
		}
		resp.Body.Close()

		if resp.StatusCode >= 400 {
			log.Println("[AUTOSCALER] scale up rejected:", resp.Status)
			continue
		}

		_ = a.store.AddReplica(db.DBID, target.URL)

		log.Printf("[AUTOSCALER] SCALE UP db=%s -> %s", db.DBID, target.URL)
	}
}

func (a *Autoscaler) scaleDown(node balancer.Node) {
	if t, ok := lastScaleDown[node.URL]; ok && time.Since(t) < 90*time.Second {
		return
	}
	lastScaleDown[node.URL] = time.Now()

	dbs, err := a.store.GetDBsByNode(node.URL)
	if err != nil {
		log.Println("[AUTOSCALER] GetDBsByNode error:", err)
		return
	}

	for _, db := range dbs {

		if len(db.ReplicaNodeIDs) == 0 {
			continue
		}

		replicaNode := db.ReplicaNodeIDs[len(db.ReplicaNodeIDs)-1]

		// IMPORTANT: we assume replica DBID == primary DBID naming scheme
		// If your system differs, adjust this mapping
		replicaDBID := db.DBID

		log.Printf("[AUTOSCALER] SCALE DOWN db=%s replicaNode=%s", db.DBID, replicaNode)

		delURL := replicaNode + "/databases/" + replicaDBID

		req, _ := http.NewRequest(http.MethodDelete, delURL, nil)
		resp, err := a.client.Do(req)
		if err != nil {
			log.Println("[AUTOSCALER] delete replica failed:", err)
			continue
		}
		resp.Body.Close()

		if resp.StatusCode >= 400 {
			log.Println("[AUTOSCALER] delete rejected:", resp.Status)
			continue
		}

		db.ReplicaNodeIDs = remove(db.ReplicaNodeIDs, replicaNode)

		_ = a.store.Save(db)

		log.Printf("[AUTOSCALER] SCALE DOWN complete db=%s replica=%s", db.DBID, replicaNode)
	}
}

func (a *Autoscaler) selectTargetNode(exclude string) (balancer.Node, error) {
	nodes := a.lb.GetNodes()

	var best balancer.Node
	bestScore := 1e9

	for _, n := range nodes {
		if !n.Healthy || n.URL == exclude {
			continue
		}

		score := n.CPUUsage*50 +
			n.MemoryUsage*30 +
			float64(n.DBCount)*20

		if score < bestScore {
			bestScore = score
			best = n
		}
	}

	if best.URL == "" {
		return balancer.Node{}, ErrNoTargetNode
	}

	return best, nil
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func remove(list []string, v string) []string {
	out := []string{}
	for _, x := range list {
		if x != v {
			out = append(out, x)
		}
	}
	return out
}

var ErrNoTargetNode = &AutoscalerError{"no target node available"}

type AutoscalerError struct {
	msg string
}

func (e *AutoscalerError) Error() string {
	return e.msg
}