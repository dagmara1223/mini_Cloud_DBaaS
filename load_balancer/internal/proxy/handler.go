package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"net"

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

	// WebSocket upgrade must be handled first
	if isWebSocketRequest(r) {
		handleWebSocket(w, r)
		return
	}

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	switch r.URL.Path {
	case "/metrics":
		HandleClusterMetrics(lb)(w, r)
		return
	case "/health":
		HandleHealth(w, r)
		return
	}

	if r.Method == http.MethodPost && r.URL.Path == "/databases" {
		handleCreateDB(w, r)
		return
	}

	if r.Method == http.MethodGet && r.URL.Path == "/databases" {
		handleListAllDBs(w, r)
		return
	}

	if handleDBActions(w, r) {
		return
	}

	if dbID, ok := extractDBIDStrict(r.URL.Path); ok {
		handleDBRequest(w, r, dbID)
		return
	}

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

	switch action {
	case "start":
		forwardDBAction(w, r, dbID, "/start")
		return true
	case "stop":
		forwardDBAction(w, r, dbID, "/stop")
		return true
	case "delete":
		record, err := store.Get(dbID)
		if err != nil {
			http.Error(w, "db not found", 404)
			return true
		}

		req, _ := http.NewRequest(http.MethodDelete,
			record.PrimaryNodeID+"/databases/"+dbID, nil)

		resp, err := http.DefaultClient.Do(req)
		if err != nil || resp.StatusCode >= 400 {
			http.Error(w, "node delete failed", 502)
			return true
		}
		defer resp.Body.Close()

		if err := store.Delete(dbID); err != nil {
			http.Error(w, "store delete failed", 500)
			return true
		}

		w.WriteHeader(200)
		w.Write([]byte(`{"status":"deleted"}`))
		return true

	case "metrics":
		forwardDBAction(w, r, dbID, "/metrics")
		return true
	}

	return false
}

// ========================= CREATE DB =========================

func handleCreateDB(w http.ResponseWriter, r *http.Request) {
	node := selectAliveNode()

	if node.URL == "" {
		http.Error(w, "no alive nodes", 500)
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

	_ = store.Save(metadata.DBRecord{
		DBID:          dbID,
		PrimaryNodeID: node.URL,
		Status:        "running",
		Owner:         user,
	})

	writeResponse(w, resp.StatusCode, resp.Header, respBody)
}

// ========================= LIST =========================

func handleListAllDBs(w http.ResponseWriter, r *http.Request) {
	user, _ := r.Context().Value(userKey).(string)

	nodes := lb.GetNodes()
	client := &http.Client{Timeout: 2 * time.Second}

	merged := make(map[string]metadata.DBRecord)

	for _, n := range nodes {
		req, err := http.NewRequest("GET", n.URL+"/databases", nil)
		if err != nil {
			continue
		}

		resp, err := client.Do(req)
		if err != nil {
			continue
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		var nodeDBs []metadata.DBRecord
		if err := json.Unmarshal(body, &nodeDBs); err != nil {
			continue
		}

		for _, db := range nodeDBs {
			if db.Owner == user {
				merged[db.DBID] = db
			}
		}
	}

	out := make([]metadata.DBRecord, 0, len(merged))
	for _, v := range merged {
		out = append(out, v)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
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
	forward(w, r, record.PrimaryNodeID+r.URL.Path, body)
}

// ========================= FORWARD ACTION =========================

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

// ========================= NODE SELECTION =========================

func selectAliveNode() balancer.Node {
	nodes := lb.GetNodes()

	for _, n := range nodes {
		if isNodeAlive(n.URL) {
			return n
		}
	}
	return balancer.Node{}
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
	node := selectAliveNode()

	if node.URL == "" {
		http.Error(w, "no alive nodes", 500)
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

// ========================= HEALTH =========================

func HandleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
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
		_ = json.NewEncoder(w).Encode(results)
	}
}

// ========================= NODE HEALTH =========================

func isNodeAlive(url string) bool {
	client := &http.Client{Timeout: 800 * time.Millisecond}

	resp, err := client.Get(url + "/health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == 200
}

// ========================= WEBSOCKETS =========================

func isWebSocketRequest(r *http.Request) bool {
	return strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade") &&
		strings.ToLower(r.Header.Get("Upgrade")) == "websocket"
}

func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	node := selectAliveNode()
	if node.URL == "" {
		http.Error(w, "no alive nodes", 500)
		return
	}

	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijacking not supported", 500)
		return
	}

	clientConn, _, err := hj.Hijack()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	targetURL := strings.Replace(node.URL, "http", "ws", 1) + r.URL.Path
	if r.URL.RawQuery != "" {
		targetURL += "?" + r.URL.RawQuery
	}

	backendConn, err := net.Dial("tcp", strings.TrimPrefix(targetURL, "ws://"))
	if err != nil {
		_ = clientConn.Close()
		return
	}

	done := make(chan struct{}, 2)

	go func() {
		_, _ = io.Copy(clientConn, backendConn)
		done <- struct{}{}
	}()

	go func() {
		_, _ = io.Copy(backendConn, clientConn)
		done <- struct{}{}
	}()

	<-done
	_ = clientConn.Close()
	_ = backendConn.Close()
}