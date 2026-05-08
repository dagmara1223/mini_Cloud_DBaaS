package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"

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

func Handle(w http.ResponseWriter, r *http.Request) {

	// CREATE DB
	if r.Method == "POST" && r.URL.Path == "/databases" {
		handleCreateDB(w, r)
		return
	}

	// ROUTING
	dbID := extractDBID(r.URL.Path)
	if dbID != "" {
		record, err := store.Get(dbID)
		if err != nil {
			http.Error(w, "db not found", 404)
			return
		}

		node := balancer.Node{
			URL: record.PrimaryNodeID,
		}

		forwardRequest(w, r, &node)
		return
	}

	// FALLBACK
	node, err := lb.SelectNode()
	if err != nil {
		http.Error(w, "no nodes", 500)
		return
	}

	realNode := lb.GetNodePtr(node)
	forwardRequest(w, r, realNode)
}

func handleCreateDB(w http.ResponseWriter, r *http.Request) {
	node, err := lb.SelectNode()
	if err != nil {
		http.Error(w, "no nodes", 500)
		return
	}

	body, _ := io.ReadAll(r.Body)

	resp, err := http.Post(node.URL+"/databases", "application/json", bytes.NewBuffer(body))
	if err != nil {
		log.Println("POST error:", err)
		http.Error(w, err.Error(), 502)
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	var data map[string]interface{}
	json.Unmarshal(respBody, &data)

	dbID, ok := data["db_id"].(string)
	if ok {
		store.Save(metadata.DBRecord{
			DBID:           dbID,
			PrimaryNodeID:  node.URL,
			ReplicaNodeIDs: []string{},
			Status:         "active",
		})
		log.Printf("[REGISTER] db=%s -> %s", dbID, node.URL)
	}

	copyResponse(w, resp, respBody)
}

func forwardRequest(w http.ResponseWriter, r *http.Request, node *balancer.Node) error {
	body, _ := io.ReadAll(r.Body)

	req, err := http.NewRequest(r.Method, node.URL+r.URL.Path, bytes.NewBuffer(body))
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

	respBody, _ := io.ReadAll(resp.Body)
	copyResponse(w, resp, respBody)

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
