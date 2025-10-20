package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
)

type Product struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Price       float64 `json:"price"`
	Description string  `json:"description"`
}

var products = map[string]Product{
	"1": {ID: "1", Name: "Laptop", Price: 999.99, Description: "High-performance laptop"},
	"2": {ID: "2", Name: "Mouse", Price: 29.99, Description: "Wireless mouse"},
	"3": {ID: "3", Name: "Keyboard", Price: 79.99, Description: "Mechanical keyboard"},
	"4": {ID: "4", Name: "Monitor", Price: 299.99, Description: "4K display"},
	"5": {ID: "5", Name: "Headphones", Price: 149.99, Description: "Noise-cancelling headphones"},
}

func getProductHandler(w http.ResponseWriter, r *http.Request) {
	// Extract ID from path
	path := strings.TrimPrefix(r.URL.Path, "/product/")
	id := strings.TrimSpace(path)

	product, exists := products[id]
	if !exists {
		http.Error(w, "Product not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(product)
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func main() {
	http.HandleFunc("/product/", getProductHandler)
	http.HandleFunc("/health", healthHandler)

	log.Println("Product Service starting on :8081")
	if err := http.ListenAndServe(":8081", nil); err != nil {
		log.Fatal(err)
	}
}