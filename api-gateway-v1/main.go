package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

type Product struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Price       float64 `json:"price"`
	Description string  `json:"description"`
}

type ProductDetails struct {
	Product         Product   `json:"product"`
	Recommendations []Product `json:"recommendations"`
	Timestamp       string    `json:"timestamp"`
}

const (
	productServiceURL        = "http://product-service:8081"
	recommendationsServiceURL = "http://recommendations-service:8082"
)

var httpClient = &http.Client{
	Timeout: 30 * time.Second, // Long timeout that will cause cascading failure
}

func getProductDetails(productID string) (*Product, error) {
	resp, err := httpClient.Get(fmt.Sprintf("%s/product/%s", productServiceURL, productID))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("product service returned status %d", resp.StatusCode)
	}

	var product Product
	if err := json.NewDecoder(resp.Body).Decode(&product); err != nil {
		return nil, err
	}

	return &product, nil
}

func getRecommendations(productID string) ([]Product, error) {
	// This call will hang for 30 seconds when the service is in failure mode
	resp, err := httpClient.Get(fmt.Sprintf("%s/recommendations/%s", recommendationsServiceURL, productID))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("recommendations service returned status %d: %s", resp.StatusCode, string(body))
	}

	var recommendations []Product
	if err := json.NewDecoder(resp.Body).Decode(&recommendations); err != nil {
		return nil, err
	}

	return recommendations, nil
}

func productDetailsHandler(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	// Extract ID from path
	path := strings.TrimPrefix(r.URL.Path, "/product-details/")
	id := strings.TrimSpace(path)

	if id == "" {
		http.Error(w, "Product ID required", http.StatusBadRequest)
		return
	}

	// Get product details from product service
	product, err := getProductDetails(id)
	if err != nil {
		log.Printf("Error getting product: %v", err)
		http.Error(w, "Failed to get product details", http.StatusInternalServerError)
		return
	}

	// Get recommendations - THIS WILL HANG AND CAUSE CASCADING FAILURE
	recommendations, err := getRecommendations(id)
	if err != nil {
		log.Printf("Error getting recommendations: %v", err)
		// Without circuit breaker, we fail the entire request
		http.Error(w, "Failed to get recommendations", http.StatusInternalServerError)
		return
	}

	// Build response
	response := ProductDetails{
		Product:         *product,
		Recommendations: recommendations,
		Timestamp:       time.Now().Format(time.RFC3339),
	}

	duration := time.Since(startTime)
	log.Printf("Request completed in %v", duration)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func main() {
	http.HandleFunc("/product-details/", productDetailsHandler)
	http.HandleFunc("/health", healthHandler)

	log.Println("API Gateway (NO CIRCUIT BREAKER) starting on :8080")
	log.Println("⚠️  This version will crash when recommendations service fails!")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatal(err)
	}
}