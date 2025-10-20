from locust import HttpUser, task, between
import random

class EcommerceUser(HttpUser):
    """
    Simulates users browsing product details pages.
    This will stress test the /product-details endpoint.
    """
    
    # Wait 1-2 seconds between requests
    wait_time = between(1, 2)
    
    # Available product IDs
    product_ids = ["1", "2", "3", "4", "5"]
    
    @task
    def view_product_details(self):
        """
        Main task: Request product details for a random product.
        This simulates a user browsing different products.
        """
        product_id = random.choice(self.product_ids)
        
        with self.client.get(
            f"/product-details/{product_id}",
            catch_response=True,
            name="/product-details/[id]"
        ) as response:
            if response.status_code == 200:
                response.success()
            else:
                response.failure(f"Failed with status {response.status_code}")
    
    def on_start(self):
        """Called when a simulated user starts."""
        pass


"""
USAGE INSTRUCTIONS:

1. Install Locust:
   pip install locust

2. Run the load test:
   
   For testing WITHOUT circuit breaker (should crash):
   locust -f locustfile.py --host=http://localhost:8080 --users 100 --spawn-rate 10 --run-time 2m --headless

   For testing WITH circuit breaker (should be resilient):
   locust -f locustfile.py --host=http://localhost:8080 --users 100 --spawn-rate 10 --run-time 2m --headless

3. Or run with Web UI (recommended for visualization):
   locust -f locustfile.py --host=http://localhost:8080
   
   Then open http://localhost:8089 in your browser and configure:
   - Number of users: 100
   - Spawn rate: 10 users/second
   - Host: http://localhost:8080

EXPECTED RESULTS:

WITHOUT Circuit Breaker (v1):
- Average response time: ~25,000ms (25 seconds)
- Failure rate: ~98%
- RPS (requests per second): Very low (~0.5)

WITH Circuit Breaker (v2):
- Average response time: ~45ms
- Failure rate: 0%
- RPS: High (~50-100)
- System degrades gracefully with empty recommendations

The difference is dramatic - the circuit breaker prevents cascading failure!
"""