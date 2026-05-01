package main

import (
	"log"
	"net/http"

	"github.com/nkucht4/load_balancer/internal/balancer"
	"github.com/nkucht4/load_balancer/internal/proxy"
)

func main() {
	lb := balancer.New([]string{
		"http://localhost:8001",
		"http://localhost:8002",
	})

	proxy.SetBalancer(lb)

	http.HandleFunc("/", proxy.Handle)

	log.Println("LB running on :9000")
	log.Fatal(http.ListenAndServe(":9000", nil))
}
