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

	if r.Method == "POST" && r.URL.Path == "/databases" {
		handleCreateDB(w, r)
		return
	}

	dbID := extractDBID(r.URL.Path)
	if dbID != "" {
		handleDBRequest(w, r, dbID)
		return
	}

	node, err := lb.SelectNode()
	if err != nil {
		http.Error(w, "no nodes", http.StatusInternalServerError)
		return
	}

	realNode := lb.GetNodePtr(node)
	if err := forwardRequest(w, r, realNode); err != nil {
		http.Error(w, err.Error(), 500)
	}
}

func handleCreateDB(w http.ResponseWriter, r *http.Request) {
	node, err := lb.SelectNode()
	if err != nil {
		http.Error(w, "no nodes", http.StatusInternalServerError)
		return
	}

	body, _ := io.ReadAll(r.Body)

	resp, err := http.Post(
		node.URL+"/databases",
		"application/json",
		bytes.NewBuffer(body),
	)
	if err != nil {
		log.Println("POST error:", err)
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	var data map[string]interface{}
	_ = json.Unmarshal(respBody, &data)

	dbID, ok := data["db_id"].(string)
	if !ok {
		http.Error(w, "invalid response from node", 500)
		return
	}

	user := r.Context().Value(userKey).(string)

	store.Save(metadata.DBRecord{
		DBID:          dbID,
		PrimaryNodeID: node.URL,
		Status:        "active",
		Owner:         user,
	})

	log.Printf("[REGISTER] db=%s owner=%s -> %s", dbID, user, node.URL)

	copyResponse(w, resp, respBody)
}

func handleDBRequest(w http.ResponseWriter, r *http.Request, dbID string) {

	record, err := store.Get(dbID)
	if err != nil {
		http.Error(w, "db not found", http.StatusNotFound)
		return
	}

	user := r.Context().Value(userKey).(string)

	if record.Owner != user {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	body, _ := io.ReadAll(r.Body)

	isRead := isReadQuery(body)

	var nodeURL string

	if isRead && len(record.ReplicaNodeIDs) > 0 {
		node, err := lb.SelectReplica(record.ReplicaNodeIDs)
		if err != nil {
			http.Error(w, "no replica available", 500)
			return
		}
		nodeURL = node.URL

		r.URL.Path = strings.Replace(r.URL.Path, "/query", "/read_query", 1)

	} else {
		nodeURL = record.PrimaryNodeID
	}

	node := &balancer.Node{URL: nodeURL}

	if err := forwardRequestWithBody(w, r, node, body); err != nil {
		http.Error(w, err.Error(), 500)
	}
}

func forwardRequest(w http.ResponseWriter, r *http.Request, node *balancer.Node) error {

	body, _ := io.ReadAll(r.Body)

	req, err := http.NewRequest(r.Method, node.URL+r.URL.Path, bytes.NewBuffer(body))
	if err != nil {
		return err
	}

	req.Header = r.Header.Clone()

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

func isReadQuery(body []byte) bool {
	q := strings.TrimSpace(strings.ToUpper(string(body)))

	return strings.HasPrefix(q, "SELECT")
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

func forwardRequestWithBody(w http.ResponseWriter, r *http.Request, node *balancer.Node, body []byte) error {

	req, err := http.NewRequest(r.Method, node.URL+r.URL.Path, bytes.NewBuffer(body))
	if err != nil {
		return err
	}

	req.Header = r.Header.Clone()

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
