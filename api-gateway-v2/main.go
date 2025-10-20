package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
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
	DegradedMode    bool      `json:"degraded_mode"`
}

const (
	productServiceURL         = "http://product-service:8081"
	recommendationsServiceURL = "http://recommendations-service:8082"
)

var httpClient = &http.Client{
	Timeout: 2 * time.Second, // Shorter timeout for fail-fast
}

// Circuit Breaker States
type State int

const (
	StateClosed State = iota
	StateOpen
	StateHalfOpen
)

func (s State) String() string {
	switch s {
	case StateClosed:
		return "CLOSED"
	case StateOpen:
		return "OPEN"
	case StateHalfOpen:
		return "HALF-OPEN"
	default:
		return "UNKNOWN"
	}
}

// CircuitBreaker implementation
type CircuitBreaker struct {
	mu              sync.Mutex
	state           State
	failureCount    int
	successCount    int
	lastFailureTime time.Time
	
	// Configuration
	maxFailures     int
	timeout         time.Duration
	halfOpenTimeout time.Duration
}

func NewCircuitBreaker() *CircuitBreaker {
	return &CircuitBreaker{
		state:           StateClosed,
		maxFailures:     3,           // Trip after 3 failures
		timeout:         10 * time.Second, // Stay open for 10 seconds
		halfOpenTimeout: 5 * time.Second,  // Allow retry after 5 seconds
	}
}

func (cb *CircuitBreaker) Execute(fn func() error) error {
	cb.mu.Lock()
	
	// Check if we should transition from OPEN to HALF-OPEN
	if cb.state == StateOpen {
		if time.Since(cb.lastFailureTime) > cb.timeout {
			log.Println("Circuit breaker transitioning to HALF-OPEN")
			cb.state = StateHalfOpen
			cb.successCount = 0
		} else {
			cb.mu.Unlock()
			return fmt.Errorf("circuit breaker is OPEN")
		}
	}
	
	currentState := cb.state
	cb.mu.Unlock()
	
	// If OPEN, fail immediately (fail fast!)
	if currentState == StateOpen {
		return fmt.Errorf("circuit breaker is OPEN")
	}
	
	// Try to execute the function
	err := fn()
	
	cb.mu.Lock()
	defer cb.mu.Unlock()
	
	if err != nil {
		cb.recordFailure()
		return err
	}
	
	cb.recordSuccess()
	return nil
}

func (cb *CircuitBreaker) recordFailure() {
	cb.failureCount++
	cb.lastFailureTime = time.Now()
	
	if cb.state == StateHalfOpen {
		log.Println("Circuit breaker: Failure in HALF-OPEN, transitioning to OPEN")
		cb.state = StateOpen
		cb.failureCount = 0
	} else if cb.failureCount >= cb.maxFailures {
		log.Printf("Circuit breaker: Failure threshold reached (%d), transitioning to OPEN", cb.maxFailures)
		cb.state = StateOpen
		cb.failureCount = 0
	}
}

func (cb *CircuitBreaker) recordSuccess() {
	cb.failureCount = 0
	
	if cb.state == StateHalfOpen {
		cb.successCount++
		if cb.successCount >= 2 {
			log.Println("Circuit breaker: Successes in HALF-OPEN, transitioning to CLOSED")
			cb.state = StateClosed
			cb.successCount = 0
		}
	}
}

func (cb *CircuitBreaker) GetState() string {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state.String()
}

// Global circuit breaker for recommendations service
var recommendationsCircuitBreaker = NewCircuitBreaker()

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
	resp, err := httpClient.Get(fmt.Sprintf("%s/recommendations/%s", recommendationsServiceURL, productID))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("recommendations service returned status %d", resp.StatusCode)
	}

	var recommendations []Product
	if err := json.NewDecoder(resp.Body).Decode(&recommendations); err != nil {
		return nil, err
	}

	return recommendations, nil
}

func getFallbackRecommendations() []Product {
	// Return empty list as fallback
	return []Product{}
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

	// Get recommendations through circuit breaker
	var recommendations []Product
	degradedMode := false

	// Wrap the recommendations call in circuit breaker
	err = recommendationsCircuitBreaker.Execute(func() error {
		recs, err := getRecommendations(id)
		if err != nil {
			return err
		}
		recommendations = recs
		return nil
	})

	if err != nil {
		// Circuit is OPEN or call failed - use fallback
		log.Printf("Circuit breaker %s or recommendation call failed: %v", 
			recommendationsCircuitBreaker.GetState(), err)
		recommendations = getFallbackRecommendations()
		degradedMode = true
	}

	// Build response - we ALWAYS succeed with graceful degradation
	response := ProductDetails{
		Product:         *product,
		Recommendations: recommendations,
		Timestamp:       time.Now().Format(time.RFC3339),
		DegradedMode:    degradedMode,
	}

	duration := time.Since(startTime)
	log.Printf("Request completed in %v (degraded: %v, circuit: %s)", 
		duration, degradedMode, recommendationsCircuitBreaker.GetState())

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func circuitStatusHandler(w http.ResponseWriter, r *http.Request) {
	status := map[string]string{
		"circuit_state": recommendationsCircuitBreaker.GetState(),
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

func main() {
	http.HandleFunc("/product-details/", productDetailsHandler)
	http.HandleFunc("/health", healthHandler)
	http.HandleFunc("/circuit-status", circuitStatusHandler)

	log.Println("API Gateway (WITH CIRCUIT BREAKER) starting on :8080")
	log.Println("✅ This version is resilient to recommendations service failures!")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatal(err)
	}
}