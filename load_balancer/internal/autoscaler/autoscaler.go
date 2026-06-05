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

	CPUThreshold float64
	Interval     time.Duration
	client       *http.Client
}

var lastScale = map[string]time.Time{}

func New(lb *balancer.Balancer, store *metadata.Store, threshold float64) *Autoscaler {
	return &Autoscaler{
		lb:           lb,
		store:        store,
		CPUThreshold: threshold,
		Interval:     10 * time.Second,
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

		if node.CPUUsage > a.CPUThreshold {
			log.Printf("[AUTOSCALER] overload detected on %s (cpu=%.2f)", node.URL, node.CPUUsage)
			a.scaleUp(node)
		}
	}
}

func (a *Autoscaler) scaleUp(node balancer.Node) {

	// ---------------- COOLDOWN GUARD ----------------
	if t, ok := lastScale[node.URL]; ok {
		if time.Since(t) < 60*time.Second {
			log.Printf("[AUTOSCALER] cooldown active for %s", node.URL)
			return
		}
	}

	// ustaw timestamp BEFORE scaling (ważne!)
	lastScale[node.URL] = time.Now()

	// ---------------- FETCH DBS ----------------
	dbs, err := a.store.GetDBsByNode(node.URL)
	if err != nil {
		log.Println("[AUTOSCALER] failed to fetch DBs:", err)
		return
	}

	if len(dbs) == 0 {
		return
	}

	// ---------------- SCALE EACH DB ----------------
	for _, db := range dbs {

		target, err := a.selectTargetNode(node.URL)
		if err != nil {
			log.Println("[AUTOSCALER] no target node:", err)
			return
		}

		log.Printf(
			"[AUTOSCALER] scaling db=%s from %s -> %s",
			db.DBID, node.URL, target.URL,
		)

		replicaURL := node.URL + "/databases/" + db.DBID + "/replica"

		payload := map[string]string{
			"target_node": target.URL,
		}

		body, _ := json.Marshal(payload)

		resp, err := a.client.Post(
			replicaURL,
			"application/json",
			bytes.NewBuffer(body),
		)
		if err != nil {
			log.Println("[AUTOSCALER] replica create failed:", err)
			continue
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			log.Println("[AUTOSCALER] replica creation failed:", resp.Status)
			continue
		}

		// ---------------- STORE METADATA ----------------
		err = a.store.AddReplica(db.DBID, target.URL)
		if err != nil {
			log.Println("[AUTOSCALER] failed to save replica:", err)
			continue
		}

		log.Printf("[AUTOSCALER] replica added db=%s -> %s", db.DBID, target.URL)
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

var ErrNoTargetNode = &AutoscalerError{"no target node available"}

type AutoscalerError struct {
	msg string
}

func (e *AutoscalerError) Error() string {
	return e.msg
}