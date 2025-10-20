package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

type Product struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Price       float64 `json:"price"`
	Description string  `json:"description"`
}

var recommendations = map[string][]Product{
	"1": {
		{ID: "3", Name: "Keyboard", Price: 79.99, Description: "Mechanical keyboard"},
		{ID: "2", Name: "Mouse", Price: 29.99, Description: "Wireless mouse"},
	},
	"2": {
		{ID: "1", Name: "Laptop", Price: 999.99, Description: "High-performance laptop"},
		{ID: "4", Name: "Monitor", Price: 299.99, Description: "4K display"},
	},
	"3": {
		{ID: "1", Name: "Laptop", Price: 999.99, Description: "High-performance laptop"},
		{ID: "2", Name: "Mouse", Price: 29.99, Description: "Wireless mouse"},
	},
	"4": {
		{ID: "1", Name: "Laptop", Price: 999.99, Description: "High-performance laptop"},
		{ID: "5", Name: "Headphones", Price: 149.99, Description: "Noise-cancelling headphones"},
	},
	"5": {
		{ID: "4", Name: "Monitor", Price: 299.99, Description: "4K display"},
		{ID: "1", Name: "Laptop", Price: 999.99, Description: "High-performance laptop"},
	},
}

func getRecommendationsHandler(w http.ResponseWriter, r *http.Request) {
	// Check if we should simulate failure
	failureMode := os.Getenv("SIMULATE_FAILURE")
	log.Printf("SIMULATE_FAILURE env var: '%s'", failureMode)
	
	if failureMode == "true" {
		log.Println("⚠️  Simulating failure - hanging for 30 seconds...")
		// Simulate a stuck database query or downstream service timeout
		time.Sleep(30 * time.Second)
		log.Println("⚠️  Timeout complete, returning error")
		http.Error(w, "Service timeout", http.StatusRequestTimeout)
		return
	}

	// Extract ID from path
	path := strings.TrimPrefix(r.URL.Path, "/recommendations/")
	id := strings.TrimSpace(path)

	recs, exists := recommendations[id]
	if !exists {
		// Return empty list if no recommendations
		recs = []Product{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(recs)
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func main() {
	failureMode := os.Getenv("SIMULATE_FAILURE")
	if failureMode == "true" {
		log.Println("⚠️  RUNNING IN FAILURE MODE - Will timeout on all requests")
	} else {
		log.Println("Running in normal mode")
	}

	http.HandleFunc("/recommendations/", getRecommendationsHandler)
	http.HandleFunc("/health", healthHandler)

	log.Println("Recommendations Service starting on :8082")
	if err := http.ListenAndServe(":8082", nil); err != nil {
		log.Fatal(err)
	}
}