package main

import (
	"log"
	"net/http"

	"github.com/nkucht4/load_balancer/internal/balancer"
	"github.com/nkucht4/load_balancer/internal/metadata"
	"github.com/nkucht4/load_balancer/internal/proxy"
)

func main() {
	lb := balancer.New([]string{
		//"http://192.168.8.119:8000",
		//"http://192.168.8.117:8000",
		"http://localhost:8001",
		"http://localhost:8002",
	}, balancer.RoundRobin)

	// START METRICS
	go lb.StartMetricsRefresh()

	// SQLite file
	store, err := metadata.New("metadata.db")
	if err != nil {
		log.Fatal(err)
	}

	proxy.SetBalancer(lb)
	proxy.SetStore(store)

	http.HandleFunc("/", proxy.Handle)

	log.Println("LB running on :9000")
	log.Fatal(http.ListenAndServe(":9000", nil))
}
