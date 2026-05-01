package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/nkucht4/load_balancer/internal/balancer"
)

var lb *balancer.Balancer

func SetBalancer(b *balancer.Balancer) {
	lb = b
}

func Handle(w http.ResponseWriter, r *http.Request) {

	// -------- CREATE DATABASE --------
	if r.Method == "POST" && r.URL.Path == "/databases" {
		handleCreateDB(w, r)
		return
	}

	// -------- DB ROUTING --------
	dbID := extractDBID(r.URL.Path)
	if dbID != "" {
		node, err := lb.GetNodeForDB(dbID)
		if err != nil {
			http.Error(w, "db not found in registry", 404)
			return
		}

		forwardRequest(w, r, &node)
		return
	}

	// -------- FALLBACK (e.g. /health) --------
	node, err := lb.SelectNode()
	if err != nil {
		http.Error(w, "no nodes", 500)
		return
	}

	realNode := lb.GetNodePtr(node)
	forwardRequest(w, r, realNode)
}

// ================= CREATE DB =================

func handleCreateDB(w http.ResponseWriter, r *http.Request) {
	node, err := lb.SelectNode()
	if err != nil {
		http.Error(w, "no nodes", 500)
		return
	}

	body, _ := io.ReadAll(r.Body)

	resp, err := http.Post(node.URL+"/databases", "application/json", bytes.NewBuffer(body))
	if err != nil {
		http.Error(w, "node error", 502)
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	var data map[string]interface{}
	json.Unmarshal(respBody, &data)

	dbID, ok := data["db_id"].(string)
	if ok {
		lb.RegisterDB(dbID, node)
		log.Printf("[REGISTER] db=%s -> %s", dbID, node.URL)
	}

	copyResponse(w, resp, respBody)
}

// ================= HELPERS =================

func forwardRequest(w http.ResponseWriter, r *http.Request, node *balancer.Node) error {
	req, err := http.NewRequest(r.Method, node.URL+r.URL.Path, r.Body)
	if err != nil {
		return err
	}

	req.Header = r.Header

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	copyResponse(w, resp, body)

	return nil
}

func copyResponse(w http.ResponseWriter, resp *http.Response, body []byte) {
	for k, v := range resp.Header {
		for _, vv := range v {
			w.Header().Add(k, vv)
		}
	}
	w.WriteHeader(resp.StatusCode)
	w.Write(body)
}

func extractDBID(path string) string {
	parts := strings.Split(path, "/")
	if len(parts) >= 3 && parts[1] == "databases" {
		return parts[2]
	}
	return ""
}
