package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/nkucht4/load_balancer/internal/balancer"
	"github.com/nkucht4/load_balancer/internal/metadata"
)

var lb *balancer.Balancer
var store *metadata.Store

func SetBalancer(b *balancer.Balancer) { lb = b }
func SetStore(s *metadata.Store)       { store = s }

type StatusUpdate struct {
    DBID   string `json:"db_id"`
    Status string `json:"status"`
}

// ========================= ENTRY =========================

func Handle(w http.ResponseWriter, r *http.Request) {
	log.Printf("[PROXY] %s %s", r.Method, r.URL.Path)

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// ---------- cluster endpoints ----------
	if r.URL.Path == "/metrics" {
		HandleClusterMetrics(lb)(w, r)
		return
	}
	if r.URL.Path == "/health" {
		HandleHealth(w, r)
		return
	}

	// ---------- database collection ----------
	if r.Method == http.MethodPost && r.URL.Path == "/databases" {
		handleCreateDB(w, r)
		return
	}

	if r.Method == http.MethodGet && r.URL.Path == "/databases" {
		handleListAllDBs(w, r)
		return
	}

	// ---------- db actions ----------
	if handleDBActions(w, r) {
		return
	}

	// ---------- db resource ----------
	if dbID, ok := extractDBIDStrict(r.URL.Path); ok {
		handleDBRequest(w, r, dbID)
		return
	}

	// ---------- fallback ----------
	forwardToNode(w, r)
}

// ========================= DB ACTIONS =========================

func handleDBActions(w http.ResponseWriter, r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, "/databases/") {
		return false
	}

	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 3 {
		return false
	}

	dbID := parts[1]
	action := parts[2]

	var suffix string

	switch action {
	case "start":
		suffix = "/start"
	case "stop":
		suffix = "/stop"
	case "delete":
		suffix = "/delete"
	case "metrics":
		suffix = "/metrics"
	default:
		return false
	}

	forwardDBAction(w, r, dbID, suffix)
	return true
}

// ========================= CREATE DB =========================

func handleCreateDB(w http.ResponseWriter, r *http.Request) {
	node, err := lb.SelectNode()
	if err != nil {
		http.Error(w, "no nodes", 500)
		return
	}

	if !isNodeAlive(node.URL) {
		log.Printf("[WARN] dead node from balancer: %s", node.URL)

		// spróbuj ponownie (prosty retry)
		nodes := lb.GetNodes()
		for _, n := range nodes {
			if isNodeAlive(n.URL) {
				node = n
				break
			}
		}
	}
	if err != nil {
		http.Error(w, "no nodes", 500)
		return
	}

	body, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewBuffer(body))

	req, _ := http.NewRequest(http.MethodPost, node.URL+"/databases", bytes.NewReader(body))
	req.Header = r.Header.Clone()

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(w, "bad gateway", 502)
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		writeResponse(w, resp.StatusCode, resp.Header, respBody)
		return
	}

	var data map[string]any
	_ = json.Unmarshal(respBody, &data)

	dbID, _ := data["db_id"].(string)
	user, _ := r.Context().Value(userKey).(string)

	store.Save(metadata.DBRecord{
		DBID:          dbID,
		PrimaryNodeID: node.URL,
		Status:        "running",
		Owner:         user,
	})

	writeResponse(w, resp.StatusCode, resp.Header, respBody)
}

// ========================= LIST DBs =========================

func handleListAllDBs(w http.ResponseWriter, r *http.Request) {
	user, _ := r.Context().Value(userKey).(string)

	all, _ := store.GetAll()

	out := []metadata.DBRecord{}
	for _, db := range all {
		if db.Owner == user {
			out = append(out, db)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

// ========================= DB REQUEST =========================

func handleDBRequest(w http.ResponseWriter, r *http.Request, dbID string) {
	record, err := store.Get(dbID)
	if err != nil {
		http.Error(w, "db not found", 404)
		return
	}

	user, _ := r.Context().Value(userKey).(string)
	if record.Owner != user {
		http.Error(w, "forbidden", 403)
		return
	}

	body, _ := io.ReadAll(r.Body)

	nodeURL := record.PrimaryNodeID

	// IMPORTANT: do NOT block request if replica missing DB
	forward(w, r, nodeURL+r.URL.Path, body)
}

// ========================= DB ACTION FORWARD =========================

func forwardDBAction(w http.ResponseWriter, r *http.Request, dbID, suffix string) {
	record, err := store.Get(dbID)
	if err != nil {
		http.Error(w, "db not found", 404)
		return
	}

	user, _ := r.Context().Value(userKey).(string)
	if record.Owner != user {
		http.Error(w, "forbidden", 403)
		return
	}

	body, _ := io.ReadAll(r.Body)

	forward(w, r, record.PrimaryNodeID+"/databases/"+dbID+suffix, body)
}

// ========================= FORWARD =========================

func forward(w http.ResponseWriter, r *http.Request, url string, body []byte) {
	req, _ := http.NewRequest(r.Method, url, bytes.NewReader(body))
	req.Header = r.Header.Clone()

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, err.Error(), 502)
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	writeResponse(w, resp.StatusCode, resp.Header, respBody)
}

// ========================= FALLBACK =========================

func forwardToNode(w http.ResponseWriter, r *http.Request) {
	node, err := lb.SelectNode()
	if err != nil {
		http.Error(w, "no nodes", 500)
		return
	}

	if !isNodeAlive(node.URL) {
		log.Printf("[WARN] dead node from balancer: %s", node.URL)

		// spróbuj ponownie (prosty retry)
		nodes := lb.GetNodes()
		for _, n := range nodes {
			if isNodeAlive(n.URL) {
				node = n
				break
			}
		}
	}
	if err != nil {
		http.Error(w, "no nodes", 500)
		return
	}

	body, _ := io.ReadAll(r.Body)

	forward(w, r, node.URL+r.URL.Path, body)
}

// ========================= UTIL =========================

func extractDBIDStrict(path string) (string, bool) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 2 && parts[0] == "databases" {
		return parts[1], true
	}
	return "", false
}

// ========================= RESPONSE =========================

func writeResponse(w http.ResponseWriter, status int, header http.Header, body []byte) {
	for k, v := range header {
		if strings.HasPrefix(strings.ToLower(k), "access-control-allow-") {
			continue
		}
		for _, vv := range v {
			w.Header().Add(k, vv)
		}
	}

	if status == 0 {
		status = 200
	}

	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// ========================= METRICS =========================

func HandleClusterMetrics(lb *balancer.Balancer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		nodes := lb.GetNodes()

		client := &http.Client{Timeout: 2 * time.Second}
		results := []any{}

		for _, n := range nodes {
			resp, err := client.Get(n.URL + "/metrics")
			if err != nil {
				results = append(results, map[string]any{
					"node":  n.URL,
					"error": "unreachable",
				})
				continue
			}

			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()

			var m map[string]any
			_ = json.Unmarshal(body, &m)

			results = append(results, map[string]any{
				"node":    n.URL,
				"metrics": m,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(results)
	}
}

// ========================= HEALTH =========================

func HandleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func isNodeAlive(url string) bool {
	client := &http.Client{Timeout: 800 * time.Millisecond}

	resp, err := client.Get(url + "/health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == 200
}

func UpdateStatus(store *metadata.Store) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        var req StatusUpdate

        if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
            http.Error(w, "invalid json", 400)
            return
        }

        _, err := store.UpdateStatus(req.DBID, req.Status)
        if err != nil {
            http.Error(w, err.Error(), 500)
            return
        }

        w.WriteHeader(200)
    }
}