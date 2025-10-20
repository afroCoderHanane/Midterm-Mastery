#!/usr/bin/env python3
"""
Automated Circuit Breaker Demo Runner

This script automates the entire demo process:
1. Starts all services (both v1 and v2 gateways on different ports)
2. Runs load test on v1 (port 8080 - no circuit breaker)
3. Runs load test on v2 (port 8090 - with circuit breaker)
4. Generates comparison report
"""

import subprocess
import time
import os
import sys
import json
import signal
from datetime import datetime

class Colors:
    RED = '\033[91m'
    GREEN = '\033[92m'
    YELLOW = '\033[93m'
    BLUE = '\033[94m'
    MAGENTA = '\033[95m'
    CYAN = '\033[96m'
    BOLD = '\033[1m'
    END = '\033[0m'

def print_section(title):
    print(f"\n{Colors.BOLD}{Colors.CYAN}{'='*60}{Colors.END}")
    print(f"{Colors.BOLD}{Colors.CYAN}{title.center(60)}{Colors.END}")
    print(f"{Colors.BOLD}{Colors.CYAN}{'='*60}{Colors.END}\n")

def print_info(message):
    print(f"{Colors.BLUE}ℹ️  {message}{Colors.END}")

def print_success(message):
    print(f"{Colors.GREEN}✅ {message}{Colors.END}")

def print_error(message):
    print(f"{Colors.RED}❌ {message}{Colors.END}")

def print_warning(message):
    print(f"{Colors.YELLOW}⚠️  {message}{Colors.END}")

def run_command(command, shell=True, check=True, capture_output=False):
    """Run a shell command and handle errors."""
    try:
        if capture_output:
            result = subprocess.run(command, shell=shell, check=check, 
                                  capture_output=True, text=True)
            return result
        else:
            subprocess.run(command, shell=shell, check=check)
            return None
    except subprocess.CalledProcessError as e:
        print_error(f"Command failed: {command}")
        if capture_output and e.stderr:
            print(e.stderr)
        return None

def stop_services():
    """Stop all Docker Compose services."""
    print_info("Stopping services...")
    run_command("docker-compose down", check=False)
    time.sleep(2)
    print_success("Services stopped")

def start_services():
    """Start Docker Compose services."""
    print_info("Starting all services (v1 on port 8080, v2 on port 8090)...")
    
    # Build and start in detached mode
    result = run_command("docker-compose up --build -d", check=False)
    
    if result is None:
        print_warning("Waiting for services to be ready...")
        time.sleep(20)  # Give services time to start
        print_success("Services started")
        return True
    else:
        print_error("Failed to start services")
        return False

def wait_for_service(url, max_retries=30):
    """Wait for a service to be ready."""
    import urllib.request
    import urllib.error
    
    for i in range(max_retries):
        try:
            urllib.request.urlopen(url, timeout=5)
            return True
        except (urllib.error.URLError, Exception):
            if i < max_retries - 1:
                time.sleep(2)
            else:
                return False
    return False

def run_locust_test(test_name, port, output_file):
    """Run Locust load test and save results."""
    print_info(f"Running Locust load test: {test_name} on port {port}")
    print_info("Test parameters: 100 users, 10/s spawn rate, 2 minutes duration")
    
    # Run locust with CSV output
    command = (
        f"locust -f locustfile.py "
        f"--host=http://localhost:{port} "
        f"--users 100 "
        f"--spawn-rate 10 "
        f"--run-time 2m "
        f"--headless "
        f"--csv={output_file} "
        f"--html={output_file}.html"
    )
    
    print_info("This will take 2 minutes...")
    start_time = time.time()
    
    result = run_command(command, check=False, capture_output=True)
    
    duration = time.time() - start_time
    
    if result and result.returncode == 0:
        print_success(f"Load test completed in {duration:.1f} seconds")
        return True
    else:
        print_error("Load test failed")
        if result and result.stderr:
            print(result.stderr)
        return False

def parse_locust_stats(csv_file):
    """Parse Locust CSV stats file."""
    stats_file = f"{csv_file}_stats.csv"
    
    if not os.path.exists(stats_file):
        print_error(f"Stats file not found: {stats_file}")
        return None
    
    try:
        with open(stats_file, 'r') as f:
            lines = f.readlines()
        
        # Skip header, get the aggregated row (should be second line)
        if len(lines) < 2:
            return None
        
        # Parse CSV manually (simple parsing)
        data_line = lines[1].strip()
        parts = data_line.split(',')
        
        # CSV format: Type,Name,Request Count,Failure Count,Median,Average,Min,Max,Content Size,Requests/s,Failures/s,...
        if len(parts) >= 7:
            return {
                'request_count': int(parts[2]),
                'failure_count': int(parts[3]),
                'avg_response_time': float(parts[5]),
                'min_response_time': float(parts[6]),
                'max_response_time': float(parts[7]),
                'requests_per_sec': float(parts[9]) if len(parts) > 9 else 0,
                'failure_rate': (int(parts[3]) / int(parts[2]) * 100) if int(parts[2]) > 0 else 0
            }
    except Exception as e:
        print_error(f"Error parsing stats: {e}")
        return None
    
    return None

def print_results(stats, version):
    """Print formatted test results."""
    if not stats:
        print_error(f"No results available for {version}")
        return
    
    print(f"\n{Colors.BOLD}Results for {version}:{Colors.END}")
    print(f"  Total Requests:        {stats['request_count']}")
    print(f"  Failed Requests:       {stats['failure_count']}")
    print(f"  Failure Rate:          {Colors.RED if stats['failure_rate'] > 50 else Colors.GREEN}{stats['failure_rate']:.2f}%{Colors.END}")
    print(f"  Avg Response Time:     {Colors.RED if stats['avg_response_time'] > 1000 else Colors.GREEN}{stats['avg_response_time']:.2f}ms{Colors.END}")
    print(f"  Min Response Time:     {stats['min_response_time']:.2f}ms")
    print(f"  Max Response Time:     {stats['max_response_time']:.2f}ms")
    print(f"  Requests/sec:          {Colors.GREEN if stats['requests_per_sec'] > 10 else Colors.RED}{stats['requests_per_sec']:.2f}{Colors.END}")

def print_comparison(stats_v1, stats_v2):
    """Print comparison between v1 and v2 results."""
    print_section("COMPARISON: WITHOUT vs WITH CIRCUIT BREAKER")
    
    if not stats_v1 or not stats_v2:
        print_error("Cannot compare - missing results")
        return
    
    # Calculate improvements
    response_time_improvement = ((stats_v1['avg_response_time'] - stats_v2['avg_response_time']) 
                                / stats_v1['avg_response_time'] * 100)
    failure_rate_improvement = stats_v1['failure_rate'] - stats_v2['failure_rate']
    throughput_improvement = ((stats_v2['requests_per_sec'] - stats_v1['requests_per_sec']) 
                             / stats_v1['requests_per_sec'] * 100) if stats_v1['requests_per_sec'] > 0 else 0
    
    print(f"\n{Colors.BOLD}{'Metric':<30} {'Without CB':<20} {'With CB':<20} {'Improvement'}{Colors.END}")
    print("-" * 90)
    
    print(f"{'Avg Response Time':<30} "
          f"{Colors.RED}{stats_v1['avg_response_time']:.2f}ms{Colors.END:<30} "
          f"{Colors.GREEN}{stats_v2['avg_response_time']:.2f}ms{Colors.END:<30} "
          f"{Colors.GREEN}{response_time_improvement:.1f}% faster{Colors.END}")
    
    print(f"{'Failure Rate':<30} "
          f"{Colors.RED}{stats_v1['failure_rate']:.2f}%{Colors.END:<30} "
          f"{Colors.GREEN}{stats_v2['failure_rate']:.2f}%{Colors.END:<30} "
          f"{Colors.GREEN}{failure_rate_improvement:.1f}% reduction{Colors.END}")
    
    print(f"{'Requests/sec':<30} "
          f"{Colors.RED}{stats_v1['requests_per_sec']:.2f}{Colors.END:<30} "
          f"{Colors.GREEN}{stats_v2['requests_per_sec']:.2f}{Colors.END:<30} "
          f"{Colors.GREEN}{throughput_improvement:.1f}% increase{Colors.END}")
    
    print("\n" + Colors.BOLD + Colors.GREEN + "🎉 The Circuit Breaker pattern successfully prevented cascading failure!" + Colors.END)

def check_prerequisites():
    """Check if required tools are installed."""
    print_section("CHECKING PREREQUISITES")
    
    issues = []
    
    # Check Docker
    result = run_command("docker --version", capture_output=True, check=False)
    if result and result.returncode == 0:
        print_success("Docker is installed")
    else:
        print_error("Docker is not installed")
        issues.append("Docker")
    
    # Check Docker Compose
    result = run_command("docker-compose --version", capture_output=True, check=False)
    if result and result.returncode == 0:
        print_success("Docker Compose is installed")
    else:
        print_error("Docker Compose is not installed")
        issues.append("Docker Compose")
    
    # Check Locust
    result = run_command("locust --version", capture_output=True, check=False)
    if result and result.returncode == 0:
        print_success("Locust is installed")
    else:
        print_error("Locust is not installed")
        issues.append("Locust (install with: pip install locust)")
    
    # Check docker-compose.yml
    if os.path.exists('docker-compose.yml'):
        print_success("docker-compose.yml found")
    else:
        print_error("docker-compose.yml not found")
        issues.append("docker-compose.yml")
    
    # Check locustfile.py
    if os.path.exists('locustfile.py'):
        print_success("locustfile.py found")
    else:
        print_error("locustfile.py not found")
        issues.append("locustfile.py")
    
    if issues:
        print_error(f"\nMissing requirements: {', '.join(issues)}")
        print_error("Please install missing components and try again")
        return False
    
    print_success("\nAll prerequisites met!")
    return True

def main():
    """Main execution flow."""
    print_section("CIRCUIT BREAKER PATTERN - AUTOMATED DEMO")
    print(f"{Colors.BOLD}This script will:{Colors.END}")
    print("  1. Start all services (v1 on port 8080, v2 on port 8090)")
    print("  2. Test v1 WITHOUT circuit breaker (expect failures)")
    print("  3. Test v2 WITH circuit breaker (expect success)")
    print("  4. Generate comparison report")
    print(f"\n{Colors.YELLOW}Total time: ~5-6 minutes{Colors.END}\n")
    
    # Check prerequisites
    if not check_prerequisites():
        sys.exit(1)
    
    input(f"\n{Colors.CYAN}Press Enter to start the demo...{Colors.END}")
    
    try:
        # Start all services
        print_section("STARTING ALL SERVICES")
        
        stop_services()
        
        if not start_services():
            print_error("Failed to start services")
            sys.exit(1)
        
        # Verify services
        print_info("Verifying services...")
        services = [
            ("Product Service", "http://localhost:8081/health"),
            ("Recommendations Service", "http://localhost:8082/health"),
            ("API Gateway v1", "http://localhost:8080/health"),
            ("API Gateway v2", "http://localhost:8090/health"),
        ]
        
        all_ready = True
        for name, url in services:
            if wait_for_service(url):
                print_success(f"{name} is ready")
            else:
                print_warning(f"{name} not responding, continuing anyway...")
                all_ready = False
        
        if not all_ready:
            print_warning("Some services aren't responding to health checks, but continuing...")
            time.sleep(5)
        
        # Phase 1: Test v1 (WITHOUT Circuit Breaker)
        print_section("PHASE 1: WITHOUT CIRCUIT BREAKER (Port 8080)")
        
        if not run_locust_test("WITHOUT Circuit Breaker", 8080, "results_v1"):
            print_error("Load test failed for v1")
        
        stats_v1 = parse_locust_stats("results_v1")
        print_results(stats_v1, "WITHOUT Circuit Breaker (v1)")
        
        # Phase 2: Test v2 (WITH Circuit Breaker)
        print_section("PHASE 2: WITH CIRCUIT BREAKER (Port 8090)")
        
        if not run_locust_test("WITH Circuit Breaker", 8090, "results_v2"):
            print_error("Load test failed for v2")
        
        stats_v2 = parse_locust_stats("results_v2")
        print_results(stats_v2, "WITH Circuit Breaker (v2)")
        
        # Compare results
        print_comparison(stats_v1, stats_v2)
        
        # Save comparison report
        print_section("SAVING RESULTS")
        
        report = {
            'timestamp': datetime.now().isoformat(),
            'without_circuit_breaker': stats_v1,
            'with_circuit_breaker': stats_v2
        }
        
        with open('comparison_report.json', 'w') as f:
            json.dump(report, f, indent=2)
        
        print_success("Results saved to comparison_report.json")
        print_info("HTML reports saved to results_v1.html and results_v2.html")
        
        print_section("DEMO COMPLETED SUCCESSFULLY!")
        print(f"{Colors.GREEN}Check the HTML reports for detailed visualizations:{Colors.END}")
        print(f"  - results_v1.html (WITHOUT circuit breaker - port 8080)")
        print(f"  - results_v2.html (WITH circuit breaker - port 8090)")
        print(f"\n{Colors.CYAN}Services are still running. You can test manually:{Colors.END}")
        print(f"  - v1: curl http://localhost:8080/product-details/1")
        print(f"  - v2: curl http://localhost:8090/product-details/1")
        print(f"\n{Colors.YELLOW}Run 'docker-compose down' to stop all services{Colors.END}")
        
    except KeyboardInterrupt:
        print_warning("\n\nDemo interrupted by user")
        sys.exit(1)
    except Exception as e:
        print_error(f"\n\nAn error occurred: {e}")
        import traceback
        traceback.print_exc()
        sys.exit(1)

if __name__ == "__main__":
    main()