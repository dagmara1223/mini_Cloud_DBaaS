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

func SetBalancer(b *balancer.Balancer) {
	lb = b
}

func SetStore(s *metadata.Store) {
	store = s
}

// ========================= ENTRY =========================

func Handle(w http.ResponseWriter, r *http.Request) {

	log.Printf("[PROXY] %s %s", r.Method, r.URL.Path)

	switch {
	case r.Method == http.MethodOptions:
		w.WriteHeader(http.StatusNoContent)
		return

	case r.Method == http.MethodPost && r.URL.Path == "/databases":
		handleCreateDB(w, r)
		return

	case r.Method == http.MethodGet && r.URL.Path == "/databases":
		handleListAllDBs(w, r)
		return
	}

	if dbID := extractDBID(r.URL.Path); dbID != "" {
		handleDBRequest(w, r, dbID)
		return
	}

	forwardToNode(w, r)
}

// ========================= CREATE DB =========================

func handleCreateDB(w http.ResponseWriter, r *http.Request) {

	node, err := lb.SelectNode()
	if err != nil {
		http.Error(w, "no nodes", 500)
		return
	}

	body, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewBuffer(body))

	req, err := http.NewRequest(http.MethodPost, node.URL+"/databases", bytes.NewReader(body))
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	req.Header = r.Header.Clone()
	req.ContentLength = int64(len(body))

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "bad gateway", 502)
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		writeResponse(w, r, resp.StatusCode, resp.Header, respBody)
		return
	}

	var data map[string]interface{}
	_ = json.Unmarshal(respBody, &data)

	dbID, ok := data["db_id"].(string)
	if !ok {
		http.Error(w, "invalid node response", 500)
		return
	}

	user := r.Context().Value(userKey).(string)

	store.Save(metadata.DBRecord{
		DBID:          dbID,
		PrimaryNodeID: node.URL,
		Status:        "running",
		Owner:         user,
	})

	log.Printf("[REGISTER] db=%s owner=%s node=%s", dbID, user, node.URL)

	writeResponse(w, r, resp.StatusCode, resp.Header, respBody)
}

// ========================= LIST DBs =========================

func handleListAllDBs(w http.ResponseWriter, r *http.Request) {

	log.Printf("[GET /databases] start")

	user, ok := r.Context().Value(userKey).(string)
	if !ok {
		http.Error(w, "unauthorized", 401)
		return
	}

	all, err := store.GetAll()
	if err != nil {
		http.Error(w, "db error", 500)
		return
	}

	filtered := make([]metadata.DBRecord, 0, len(all))

	for _, db := range all {
		if db.Owner == user {
			filtered = append(filtered, db)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	_ = json.NewEncoder(w).Encode(filtered)

	log.Printf("[GET /databases] returned %d items", len(filtered))
}

// ========================= DB REQUEST ROUTING =========================

func handleDBRequest(w http.ResponseWriter, r *http.Request, dbID string) {

	record, err := store.Get(dbID)
	if err != nil {
		http.Error(w, "db not found", 404)
		return
	}

	user := r.Context().Value(userKey).(string)
	if record.Owner != user {
		http.Error(w, "forbidden", 403)
		return
	}

	body, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewBuffer(body))

	isRead := isReadQuery(body)

	nodeURL := record.PrimaryNodeID

	if isRead && len(record.ReplicaNodeIDs) > 0 {
		node, err := lb.SelectReplica(record.ReplicaNodeIDs)
		if err == nil {
			nodeURL = node.URL
		}
	}

	req, err := http.NewRequest(r.Method, nodeURL+r.URL.Path, bytes.NewReader(body))
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	req.Header = r.Header.Clone()
	req.ContentLength = int64(len(body))

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, err.Error(), 502)
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	writeResponse(w, r, resp.StatusCode, resp.Header, respBody)
}

// ========================= FORWARD =========================

func forwardToNode(w http.ResponseWriter, r *http.Request) {

	node, err := lb.SelectNode()
	if err != nil {
		http.Error(w, "no nodes", 500)
		return
	}

	body, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewBuffer(body))

	req, err := http.NewRequest(r.Method, node.URL+r.URL.Path, bytes.NewReader(body))
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	req.Header = r.Header.Clone()
	req.ContentLength = int64(len(body))

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, err.Error(), 502)
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	writeResponse(w, r, resp.StatusCode, resp.Header, respBody)
}

// ========================= CORS =========================

func addCORS(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if origin != "" {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Vary", "Origin")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
	}

	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
}

// ========================= UTIL =========================

func isReadQuery(body []byte) bool {
	q := strings.TrimSpace(strings.ToUpper(string(body)))
	return strings.HasPrefix(q, "SELECT")
}

func extractDBID(path string) string {
	parts := strings.Split(path, "/")
	if len(parts) >= 3 && parts[1] == "databases" {
		return parts[2]
	}
	return ""
}

func writeResponse(w http.ResponseWriter, r *http.Request, status int, header http.Header, body []byte) {

	for k, v := range header {
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

func HandleClusterMetrics(lb *balancer.Balancer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		nodes := lb.GetNodes()

		client := &http.Client{Timeout: 2 * time.Second}

		results := make([]any, 0, len(nodes))

		for _, n := range nodes {

			resp, err := client.Get(n.URL + "/metrics")
			if err != nil {
				results = append(results, map[string]any{
					"node": n.URL,
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

func HandleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
	})
}