# Midterm-Mastery

## Part 1:

[2 Pages Write up](https://docs.google.com/document/d/1GSnO-Dlk_kpZQNGaIsJE1hQ8-Jkrf6TZSvRNVRuSXAM/edit?usp=sharing)

# Part II: Crashing and Recovering - Circuit Breaker Pattern Demo

## 🎯 Project Overview

This demo showcases a **cascading failure** in a microservices e-commerce application and demonstrates how the **Circuit Breaker pattern** prevents system-wide outages.

---

## 📐 System Architecture

```
┌─────────┐
│  User   │
└────┬────┘
     │
     ▼
┌─────────────────┐
│  API Gateway    │ (Entry Point)
│  Port: 8080/8090│
└────┬────────────┘
     │
     ├──────────────────────┬──────────────────────┐
     ▼                      ▼                      ▼
┌──────────────┐    ┌──────────────┐    ┌──────────────────────┐
│   Product    │    │Recommendations│    │  Recommendations     │
│   Service    │    │   Service     │    │   Service (Faulty)   │
│  (Healthy)   │    │  (Healthy)    │    │   SIMULATE_FAILURE   │
│  Port: 8081  │    │  Port: 8082   │    │   = true             │
└──────────────┘    └──────────────┘    └──────────────────────┘
```

### Services:
1. **Product Service** (Port 8081) - Returns basic product information (name, price, description)
2. **Recommendations Service** (Port 8082) - Returns related product recommendations
3. **API Gateway v1** (Port 8080) - **WITHOUT** Circuit Breaker
4. **API Gateway v2** (Port 8090) - **WITH** Circuit Breaker

---

## 🔴 1: The Problematic Deployment

### The Failure Scenario

The Recommendations Service has a bug that causes it to **hang for 30 seconds** before timing out. This simulates real-world issues like:
- Slow database queries
- Unresponsive downstream services
- Network congestion
- Database connection pool exhaustion

### Code: Simulating the Failure

```go
// recommendations-service/main.go
func getRecommendationsHandler(w http.ResponseWriter, r *http.Request) {
    if os.Getenv("SIMULATE_FAILURE") == "true" {
        log.Println("⚠️  Simulating failure - hanging for 30 seconds...")
        time.Sleep(30 * time.Second)  // Simulates stuck operation
        http.Error(w, "Service timeout", http.StatusRequestTimeout)
        return
    }
    // Normal operation...
}
```

### API Gateway v1: No Protection

```go
// api-gateway-v1/main.go
var httpClient = &http.Client{
    Timeout: 30 * time.Second,  // Long timeout - waits the full 30s
}

func productDetailsHandler(w http.ResponseWriter, r *http.Request) {
    // Get product details (works fine)
    product, err := getProductDetails(id)
    
    // Get recommendations - THIS BLOCKS FOR 30 SECONDS!
    recommendations, err := getRecommendations(id)
    if err != nil {
        // ENTIRE REQUEST FAILS if recommendations fail
        http.Error(w, "Failed to get recommendations", http.StatusInternalServerError)
        return  // ❌ User gets NOTHING
    }
    
    // Only succeeds if BOTH services work
    response := ProductDetails{
        Product:         *product,
        Recommendations: recommendations,
    }
    json.NewEncoder(w).Encode(response)
}
```

### Demonstrating the Issue

**Start the problematic system:**
```bash
docker-compose up --build
```

**Manual Test:**
```bash
# This hangs for 30 seconds, then fails
time curl --max-time 35 http://localhost:8080/product-details/1
```

**Load Test with Locust:**
```bash
locust -f locustfile.py \
  --host=http://localhost:8080 \
  --users 100 \
  --spawn-rate 10 \
  --run-time 2m \
  --headless \
  --csv=results_v1
```

### 📊 Metrics: The Problem

| Metric | Result | Status |
|--------|--------|--------|
| **Average Response Time** | ~30,000ms (30 seconds!) | 🔴 TERRIBLE |
| **Failure Rate** | 100% | 🔴 BROKEN |
| **Requests/Second** | ~2-3 | 🔴 UNUSABLE |
| **User Experience** | Can't access ANY product info | 🔴 SYSTEM DOWN |

### The Impact: Cascading Failure

```
Request Timeline (WITHOUT Circuit Breaker):
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Request 1: ████████████████████████████████ (30s wait) → TIMEOUT ❌
Request 2: ████████████████████████████████ (30s wait) → TIMEOUT ❌
Request 3: ████████████████████████████████ (30s wait) → TIMEOUT ❌
Request 4: ████████████████████████████████ (30s wait) → TIMEOUT ❌

Result: System is COMPLETELY UNUSABLE 💀
```

**Key Problems:**
- ❌ Non-critical service (recommendations) brings down the entire system
- ❌ Gateway threads get exhausted waiting for timeouts
- ❌ Users can't get basic product information
- ❌ Even though Product Service is healthy, users see failures
- ❌ Classic **cascading failure** - one failure propagates throughout the system

---

## 🟢 2: The Fix - Circuit Breaker Pattern

### What is a Circuit Breaker?

The Circuit Breaker pattern prevents cascading failures by monitoring for faults and "opening" the circuit when failures exceed a threshold, causing subsequent calls to fail immediately.

### Circuit Breaker States

```
┌─────────────────┐
│     CLOSED      │ ← Normal operation
│  All requests   │   Monitoring for failures
│   pass through  │
└────────┬────────┘
         │
         │ After 3 consecutive failures
         ▼
┌─────────────────┐
│      OPEN       │ ← Fail Fast!
│  All requests   │   No calls to faulty service
│  fail instantly │   Return fallback immediately
└────────┬────────┘
         │
         │ After 10 second cooldown
         ▼
┌─────────────────┐
│   HALF-OPEN     │ ← Testing recovery
│  Allow 1 test   │   If success → CLOSED
│    request      │   If failure → OPEN
└─────────────────┘
```

### Implementation: Circuit Breaker Code

```go
// api-gateway-v2/main.go

// Circuit Breaker Structure
type CircuitBreaker struct {
    mu              sync.Mutex
    state           State  // CLOSED, OPEN, or HALF-OPEN
    failureCount    int
    successCount    int
    lastFailureTime time.Time
    maxFailures     int           // Trip after 3 failures
    timeout         time.Duration  // Stay open for 10s
}

func (cb *CircuitBreaker) Execute(fn func() error) error {
    cb.mu.Lock()
    
    // Check if we should transition from OPEN to HALF-OPEN
    if cb.state == StateOpen {
        if time.Since(cb.lastFailureTime) > cb.timeout {
            log.Println("Circuit breaker transitioning to HALF-OPEN")
            cb.state = StateHalfOpen
        } else {
            cb.mu.Unlock()
            return fmt.Errorf("circuit breaker is OPEN")  // FAIL FAST!
        }
    }
    
    cb.mu.Unlock()
    
    // If OPEN, fail immediately without calling service
    if cb.state == StateOpen {
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
    } else if cb.failureCount >= cb.maxFailures {
        log.Printf("Circuit breaker: Threshold reached (%d), transitioning to OPEN", cb.maxFailures)
        cb.state = StateOpen
        cb.failureCount = 0
    }
}
```

### API Gateway v2: Protected with Circuit Breaker

```go
// api-gateway-v2/main.go

var httpClient = &http.Client{
    Timeout: 5 * time.Second,  // Fail fast after 5 seconds (not 30!)
}

var recommendationsCircuitBreaker = NewCircuitBreaker()

func NewCircuitBreaker() *CircuitBreaker {
    return &CircuitBreaker{
        state:           StateClosed,
        maxFailures:     3,            // Trip after 3 failures
        timeout:         5 * time.Second,  // Stay open for 5 seconds
        halfOpenTimeout: 3 * time.Second,  // Test recovery after 3 seconds
    }
}

func productDetailsHandler(w http.ResponseWriter, r *http.Request) {
    // Get product details (same as v1)
    product, err := getProductDetails(id)
    if err != nil {
        http.Error(w, "Failed to get product details", http.StatusInternalServerError)
        return
    }
    
    // Get recommendations WITH CIRCUIT BREAKER PROTECTION
    var recommendations []Product
    degradedMode := false
    
    // Wrap the call in circuit breaker
    err = recommendationsCircuitBreaker.Execute(func() error {
        recs, err := getRecommendations(id)
        if err != nil {
            return err
        }
        recommendations = recs
        return nil
    })
    
    if err != nil {
        // Circuit is OPEN or call failed
        // Use fallback - DON'T FAIL THE ENTIRE REQUEST!
        log.Printf("Circuit breaker %s or call failed: %v", 
            recommendationsCircuitBreaker.GetState(), err)
        recommendations = getFallbackRecommendations()  // Empty list
        degradedMode = true
    }
    
    // ALWAYS succeed with graceful degradation
    response := ProductDetails{
        Product:         *product,
        Recommendations: recommendations,  // Might be empty
        DegradedMode:    degradedMode,     // Flag for monitoring
        Timestamp:       time.Now().Format(time.RFC3339),
    }
    
    json.NewEncoder(w).Encode(response)  // ✅ User ALWAYS gets product info
}

func getFallbackRecommendations() []Product {
    return []Product{}  // Return empty list as fallback
}
```

### Key Improvements

1. **Fail Fast** - 2-second timeout instead of 30 seconds
2. **Circuit Opens** - After 3 failures, stop calling the faulty service
3. **Fallback Response** - Return empty recommendations instead of crashing
4. **Graceful Degradation** - Core functionality (product details) still works
5. **Self-Healing** - Circuit automatically tests for recovery

---

## ✅ 3: The Improved System

### Testing the Fixed System

**Manual Test:**
```bash
# Returns instantly with empty recommendations
curl http://localhost:8090/product-details/1

# Check circuit breaker status
curl http://localhost:8090/circuit-status
```

**Load Test:**
```bash
locust -f locustfile.py \
  --host=http://localhost:8090 \
  --users 100 \
  --spawn-rate 10 \
  --run-time 2m \
  --headless \
  --csv=results_v2
```

### 📊 Metrics: The Fix

| Metric | v1 (No CB) | v2 (With CB) | Improvement |
|--------|------------|--------------|-------------|
| **Avg Response Time** | 30,011ms | 5ms | **99.98% faster** ⚡ |
| **Failure Rate** | 100% | 100%* | **Graceful degradation** ✅ |
| **Requests/Second** | 2.93 | 64.22 | **2,088% increase** 📈 |
| **User Experience** | Broken | Works | **Users get product info** 🎯 |

**Note:** Both show 100% "failure" because Locust marks requests without recommendations as failures. However, v2 returns valid product data instantly (degraded mode), while v1 times out completely. This is the difference between **graceful degradation** (working with reduced features) and **cascading failure** (complete system outage).

### Request Timeline Comparison

```
WITHOUT Circuit Breaker (v1 - Port 8080):
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Request 1: ████████████████████████████████ (30s) → TIMEOUT ❌
Request 2: ████████████████████████████████ (30s) → TIMEOUT ❌
Request 3: ████████████████████████████████ (30s) → TIMEOUT ❌
Request 4: ████████████████████████████████ (30s) → TIMEOUT ❌
System: COMPLETELY UNUSABLE 💀
Throughput: 2.93 req/s


WITH Circuit Breaker (v2 - Port 8090):
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Request 1: █████ (5s timeout) → FAIL (count: 1) - Empty recommendations
Request 2: █████ (5s timeout) → FAIL (count: 2) - Empty recommendations
Request 3: █████ (5s timeout) → FAIL (count: 3) - Empty recommendations
🔴 CIRCUIT OPENS! 🔴
Request 4: ▌(5ms) → SUCCESS ✅ Product info + empty recommendations
Request 5: ▌(5ms) → SUCCESS ✅ Product info + empty recommendations
Request 6: ▌(5ms) → SUCCESS ✅ Product info + empty recommendations
Request 7: ▌(5ms) → SUCCESS ✅ Product info + empty recommendations
...hundreds more instant requests...
System: WORKS PERFECTLY with graceful degradation! 🎉
Throughput: 64.22 req/s (21x faster!)
```

### Observing Circuit Breaker in Action

Watch the circuit state transitions in real-time:

```bash
# Terminal 1: Watch circuit status
watch -n 1 'curl -s http://localhost:8090/circuit-status'

# Terminal 2: Make requests
for i in {1..10}; do
  echo "Request $i:"
  curl http://localhost:8090/product-details/1
  echo ""
  sleep 1
done
```

**You'll see:**
1. Circuit starts CLOSED (normal operation)
2. First 3 requests take ~5 seconds each and fail
3. Circuit trips to OPEN after 3rd failure
4. Subsequent requests return in **~5 milliseconds** with product info
5. After 5 seconds, circuit goes to HALF-OPEN (testing recovery)
6. If service still fails, back to OPEN (fail fast continues)
7. If service recovers, back to CLOSED (normal operation resumes)

---

## 🚀 Running the Complete Demo

### Prerequisites

```bash
# Install required tools
pip install locust

# Verify Docker is running
docker --version
docker-compose --version
```

### Project Structure

```
circuit-breaker-demo/
├── product-service/
│   ├── main.go
│   ├── Dockerfile
│   └── go.mod
├── recommendations-service/
│   ├── main.go           # Has SIMULATE_FAILURE toggle
│   ├── Dockerfile
│   └── go.mod
├── api-gateway-v1/       # WITHOUT circuit breaker
│   ├── main.go
│   ├── Dockerfile
│   └── go.mod
├── api-gateway-v2/       # WITH circuit breaker
│   ├── main.go
│   ├── Dockerfile
│   └── go.mod
├── docker-compose.yml    # Runs ALL services simultaneously
├── locustfile.py         # Load testing script
└── run_demo.py           # Automated demo script
```

### Automated Demo (Recommended)

```bash
# One command to run the entire demo
python3 run_demo.py
```

**The script will:**
1. ✅ Check prerequisites
2. 🚀 Start all services (v1 on 8080, v2 on 8090)
3. 🔴 Test v1 (expect 25s response times, 98% failures)
4. 🟢 Test v2 (expect 45ms response times, 0% failures)
5. 📊 Generate comparison report and HTML visualizations
6. 💾 Save results to `results_v1.html`, `results_v2.html`, and `comparison_report.json`

**Total Time:** ~5-6 minutes

### Manual Demo Steps

**Step 1: Start All Services**
```bash
docker-compose up --build
```

**Step 2: Test v1 (Without Circuit Breaker)**
```bash
# See the failure (hangs 30s)
time curl --max-time 35 http://localhost:8080/product-details/1

# Run load test
locust -f locustfile.py --host=http://localhost:8080 --users 100 --spawn-rate 10 --run-time 2m --headless
```

**Step 3: Test v2 (With Circuit Breaker)**
```bash
# See it work instantly
curl http://localhost:8090/product-details/1

# Check circuit status
curl http://localhost:8090/circuit-status

# Run load test
locust -f locustfile.py --host=http://localhost:8090 --users 100 --spawn-rate 10 --run-time 2m --headless
```

---

## 🎓 Key Takeaways

### The Problem: Cascading Failure
- ❌ One failing service brings down the entire system
- ❌ Users can't access ANY features, even healthy ones
- ❌ Thread exhaustion as all workers wait for timeouts
- ❌ Non-critical service failure causes critical system outage

### The Solution: Circuit Breaker Pattern
- ✅ **Fail Fast** - Stop waiting for timeouts (2s instead of 30s)
- ✅ **Isolation** - Prevent one service failure from cascading
- ✅ **Graceful Degradation** - Core features still work
- ✅ **Automatic Recovery** - Self-healing when service recovers
- ✅ **Observable** - Circuit state indicates system health

### Sam Newman's Resilience Patterns Applied

1. **Fail Fast** ⚡
   - Short timeouts (2s vs 30s)
   - Don't wait for doomed operations

2. **Circuit Breaker** 🔌
   - Monitor failures
   - Open circuit after threshold
   - Fail immediately when open

3. **Bulkhead** 🚢 (Bonus)
   - Could isolate thread pools per service
   - Prevent one slow service from exhausting all threads

### Real-World Applications

This pattern is used by:
- **Netflix Hystrix** - Original circuit breaker library
- **Resilience4j** - Modern resilience library for Java
- **Polly** - .NET resilience library
- **Go-resilience** - Go circuit breaker implementations

---

## 📊 Demo Results Summary

### WITHOUT Circuit Breaker (Port 8080)
```
┌────────────────────────────────────────┐
│ System Status: 🔴 CRITICAL FAILURE     │
├────────────────────────────────────────┤
│ Avg Response Time:    30,011ms         │
│ Failure Rate:         100%             │
│ Requests/Second:      2.93             │
│ User Impact:          Cannot use site  │
│ Cause:                30s timeouts     │
└────────────────────────────────────────┘
```

### WITH Circuit Breaker (Port 8090)
```
┌────────────────────────────────────────┐
│ System Status: 🟢 OPERATIONAL          │
├────────────────────────────────────────┤
│ Avg Response Time:    5ms              │
│ Failure Rate:         100%*            │
│ Requests/Second:      64.22            │
│ User Impact:          Works perfectly  │
│ Degraded Features:    Recommendations  │
│ *Graceful degradation - not true fail  │
└────────────────────────────────────────┘
```

### Improvement
- **Response Time:** 99.98% faster (30,011ms → 5ms)
- **Throughput:** 2,088% increase (2.93 → 64.22 req/s)
- **User Experience:** Broken → Functional
- **Key Insight:** Users get product info in 5ms instead of waiting 30s for nothing

---

## 📝 Conclusion

The Circuit Breaker pattern is essential for building resilient distributed systems. By detecting failures early and failing fast, we:
- Prevent cascading failures
- Enable graceful degradation
- Improve user experience
- Allow systems to self-heal

**The result:** A system that stays up even when components fail. 🎉

## Part III:

Presentation
