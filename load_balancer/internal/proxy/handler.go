package proxy

import (
	"io"
	"net/http"

	"github.com/nkucht4/load_balancer/internal/balancer"
)

var lb *balancer.Balancer

func SetBalancer(b *balancer.Balancer) {
	lb = b
}

func Handle(w http.ResponseWriter, r *http.Request) {
	node, err := lb.NextNode()
	if err != nil {
		http.Error(w, "no nodes", 500)
		return
	}

	target := node.URL

	req, _ := http.NewRequest(r.Method, target+r.URL.Path, r.Body)
	req.Header = r.Header

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "node error", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	for k, v := range resp.Header {
		for _, vv := range v {
			w.Header().Add(k, vv)
		}
	}

	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}
