package main

import (
	"log"
	"net/http"

	"github.com/nkucht4/load_balancer/internal/balancer"
	"github.com/nkucht4/load_balancer/internal/metadata"
	"github.com/nkucht4/load_balancer/internal/proxy"
	"github.com/nkucht4/load_balancer/internal/autoscaler"
)

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		allowedOrigin := "http://localhost:3000" // CHANGE to your frontend URL

		origin := r.Header.Get("Origin")

		// Only allow known origin
		if origin == allowedOrigin {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}

		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		origin := r.Header.Get("Origin")
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Vary", "Origin")
		}

		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func main() {
	lb := balancer.New([]string{
		 "http://localhost:8001",
		 "https://amuser-scotch-symptom.ngrok-free.dev",
		//"http://localhost:8000",
	}, balancer.RoundRobin)

	go lb.StartMetricsRefresh()

	store, err := metadata.New("metadata.db")
	if err != nil {
		log.Fatal(err)
	}

	a := autoscaler.New(lb, store, 0.7)
	go a.Start()

	proxy.SetBalancer(lb)
	proxy.SetStore(store)

	http.HandleFunc("/login", proxy.LoginHandler)
	http.HandleFunc("/metrics", proxy.AuthMiddleware(proxy.HandleClusterMetrics(lb)))
	http.HandleFunc("/health", proxy.AuthMiddleware(proxy.HandleHealth))
	http.HandleFunc("/", proxy.AuthMiddleware(proxy.Handle))

	log.Println("LB running on :9000")
	log.Fatal(http.ListenAndServe(":9000", withCORS(http.DefaultServeMux)))
}
// func main() {
// 	lb := balancer.New([]string{
// 		//"http://192.168.8.119:8000",
// 		//"http://192.168.8.117:8000",
// 		"http://localhost:8001",
// 		"http://localhost:8002",
// 	}, balancer.RoundRobin)

// 	// START METRICS
// 	//go lb.StartMetricsRefresh()

// 	// SQLite file
// 	store, err := metadata.New("metadata.db")
// 	if err != nil {
// 		log.Fatal(err)
// 	}

// 	autoscaler := autoscaler.New(lb, store, 0.7)
// 	go autoscaler.Start()

// 	proxy.SetBalancer(lb)
// 	proxy.SetStore(store)

// 	http.HandleFunc("/login", proxy.LoginHandler)
// 	http.HandleFunc("/metrics", proxy.AuthMiddleware(proxy.HandleClusterMetrics(lb)))
// 	http.HandleFunc("/health", proxy.AuthMiddleware(proxy.HandleHealth))
// 	http.HandleFunc("/", proxy.AuthMiddleware(proxy.Handle))

// 	handler := withCORS(http.DefaultServeMux)
// 	log.Fatal(http.ListenAndServe(":9000", handler))

// 	log.Println("LB running on :9000")
// 	log.Fatal(http.ListenAndServe(":9000", handler))

// 	// http.HandleFunc("/login", proxy.LoginHandler)
// 	// http.HandleFunc("/", proxy.AuthMiddleware(proxy.Handle))

// 	// added for frontend ------------
// 	// handler := corsMiddleware(http.DefaultServeMux)
// 	// log.Fatal(http.ListenAndServe(":9000", handler))

// 	// log.Println("LB running on :9000")
// 	// //log.Fatal(http.ListenAndServe(":9000", nil))
// 	// log.Fatal(http.ListenAndServe(":9000", corsMiddleware(http.DefaultServeMux)))
// }
