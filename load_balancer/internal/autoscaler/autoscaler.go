package autoscaler

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"time"
	"fmt"

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

	client *http.Client
}

var lastScaleUp = map[string]time.Time{}
var lastScaleDown = map[string]time.Time{}
var lastDBScaleUp = map[string]time.Time{}

func New(lb *balancer.Balancer, store *metadata.Store, threshold float64) *Autoscaler {
	return &Autoscaler{
		lb:               lb,
		store:            store,
		CPUHighThreshold: threshold,
		CPULowThreshold:  threshold * 0.4,
		MaxReplicasPerDB: 3,
		Interval:         10 * time.Second,
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
	for _, node := range a.lb.GetNodes() {
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

// ================= SCALE UP =================

func (a *Autoscaler) scaleUp(node balancer.Node) {
	if t, ok := lastScaleUp[node.URL]; ok && time.Since(t) < 10*time.Second {
		return
	}
	lastScaleUp[node.URL] = time.Now()

	dbs, err := a.store.GetDBsByNode(node.URL)
	if err != nil {
		log.Println("[AUTOSCALER] GetDBsByNode error:", err)
		return
	}

	for _, db := range dbs {

		if t, ok := lastDBScaleUp[db.DBID]; ok && time.Since(t) < 30*time.Second {
			continue
		}

		if len(db.ReplicaNodeIDs) >= a.MaxReplicasPerDB {
			continue
		}

		target, err := a.selectTargetNode(node.URL)
		if err != nil {
			log.Println("[AUTOSCALER] no target node:", err)
			return
		}

		// HARD SAFETY: prevent same-node replica
		if target.URL == node.URL {
			continue
		}

		if contains(db.ReplicaNodeIDs, target.URL) {
			continue
		}

		url := fmt.Sprintf("%s/databases/%s/replica", db.PrimaryNodeID, db.DBID)

		reqBody := map[string]string{
			"target_node": target.URL,
		}

		body, _ := json.Marshal(reqBody)

		resp, err := a.client.Post(url, "application/json", bytes.NewBuffer(body))
		if err != nil {
			log.Println("[AUTOSCALER] scale up failed:", err)
			continue
		}

		if resp.StatusCode >= 400 {
			log.Println("[AUTOSCALER] scale up rejected:", resp.Status)
			resp.Body.Close()
			continue
		}

		var result struct {
			ReplicaID string `json:"replica_id"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			resp.Body.Close()
			log.Println("[AUTOSCALER] decode failed:", err)
			continue
		}
		resp.Body.Close()

		if result.ReplicaID == "" {
			continue
		}

		record, err := a.store.Get(db.DBID)
		if err != nil {
			continue
		}

		if record.ReplicaMap == nil {
			record.ReplicaMap = map[string]string{}
		}

		record.ReplicaMap[target.URL] = result.ReplicaID

		if !contains(record.ReplicaNodeIDs, target.URL) {
			record.ReplicaNodeIDs = append(record.ReplicaNodeIDs, target.URL)
		}

		_ = a.store.Save(record)

		lastDBScaleUp[db.DBID] = time.Now()

		log.Printf("[AUTOSCALER] SCALE UP db=%s -> %s", db.DBID, target.URL)
	}
}

// ================= SCALE DOWN =================

func (a *Autoscaler) scaleDown(node balancer.Node) {
	if t, ok := lastScaleDown[node.URL]; ok && time.Since(t) < 5*time.Second {
		return
	}
	lastScaleDown[node.URL] = time.Now()

	dbsPrimary, _ := a.store.GetDBsByNode(node.URL)
	dbsReplica, _ := a.store.GetDBsByReplicaNode(node.URL)
	dbs := append(dbsPrimary, dbsReplica...)

	for _, db := range dbs {

		var replicaNode string
		for _, r := range db.ReplicaNodeIDs {
			if r == node.URL {
				replicaNode = r
				break
			}
		}

		if replicaNode == "" {
			continue
		}

		replicaID, ok := db.ReplicaMap[replicaNode]
		if !ok {
			continue
		}

		delURL := fmt.Sprintf("%s/databases/%s", replicaNode, replicaID)

		req, _ := http.NewRequest(http.MethodDelete, delURL, nil)

		resp, err := a.client.Do(req)
		if err != nil {
			log.Println("[AUTOSCALER] delete failed:", err)
			continue
		}

		if resp.StatusCode >= 400 {
			log.Println("[AUTOSCALER] delete rejected:", resp.Status)
			resp.Body.Close()
			continue
		}
		resp.Body.Close()

		delete(db.ReplicaMap, replicaNode)
		db.ReplicaNodeIDs = remove(db.ReplicaNodeIDs, replicaNode)

		_ = a.store.Save(db)

		log.Printf("[AUTOSCALER] SCALE DOWN db=%s replica=%s", db.DBID, replicaNode)
	}
}

// ================= NODE SELECTION =================

func (a *Autoscaler) selectTargetNode(exclude string) (balancer.Node, error) {
	nodes := a.lb.GetNodes()

	var best balancer.Node
	bestScore := 1e9

	for _, n := range nodes {
		if !n.Healthy {
			continue
		}

		if n.URL == exclude {
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

	// fallback: if cluster is broken, allow same node (prevents deadlock)
	if best.URL == "" {
		for _, n := range nodes {
			if n.Healthy {
				return n, nil
			}
		}
		return balancer.Node{}, ErrNoTargetNode
	}

	return best, nil
}

// ================= HELPERS =================

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