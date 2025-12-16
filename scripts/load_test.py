#!/usr/bin/env python3
"""
Performance Test Script for Shock Absorber
Sends 20 requests per second for 30 seconds (600 total requests)
DB is configured to write at 1 request per second
"""

import requests
import time
import json
from datetime import datetime
import sys

API_URL = "http://localhost:8080/payment"
REQUESTS_PER_SECOND = 20
DURATION_SECONDS = 30
TOTAL_REQUESTS = REQUESTS_PER_SECOND * DURATION_SECONDS
INTERVAL = 1.0 / REQUESTS_PER_SECOND  # 50ms between requests

def generate_payment(request_count):
    """Generate a unique payment payload"""
    timestamp = int(time.time() * 1000000)  # microseconds
    ref_id = f"perf_test_{timestamp}_{request_count}"
    payment_id = f"pay_{timestamp}_{request_count}"
    
    return {
        "id": payment_id,
        "upi_reference_id": ref_id,
        "amount": 10000,
        "currency": "INR",
        "upi_transaction_id": f"txn_perf_{request_count}",
        "tenant_id": "tenant_001",
        "customer_id": "cust_001",
        "leg": "PAYER",
        "action": "PAY",
        "status": "PENDING",
        "payer": {
            "vpa": "user@upi",
            "name": f"Test User {request_count}"
        },
        "payees": [
            {
                "vpa": "merchant@upi",
                "name": "Merchant",
                "amount": 10000
            }
        ],
        "meta": {
            "msg_id": f"msg_perf_{request_count}",
            "note": f"Performance test request #{request_count}"
        },
        "resp_meta": {
            "timestamp": int(time.time())
        }
    }

def main():
    print("=" * 50)
    print("Shock Absorber Performance Test")
    print("=" * 50)
    print(f"API URL: {API_URL}")
    print(f"Request Rate: {REQUESTS_PER_SECOND} requests/second")
    print(f"Duration: {DURATION_SECONDS} seconds")
    print(f"Total Requests: {TOTAL_REQUESTS}")
    print(f"DB Write Rate: 1 request/second (configured)")
    print(f"Expected: All requests should be accepted immediately")
    print(f"Expected: DB writes will take ~{TOTAL_REQUESTS} seconds to complete")
    print("=" * 50)
    print()
    
    # Check if server is running
    try:
        response = requests.get(f"{API_URL.replace('/payment', '/health')}", timeout=2)
        if response.status_code != 200:
            print(f"WARNING: Health check returned {response.status_code}")
    except requests.exceptions.RequestException as e:
        print(f"ERROR: Server is not running at {API_URL}")
        print(f"Please start the test server first: go run cmd/test-server/main.go")
        sys.exit(1)
    
    print(f"Starting load test at {datetime.now().strftime('%Y-%m-%d %H:%M:%S')}")
    print()
    
    request_count = 0
    success_count = 0
    error_count = 0
    start_time = time.time()
    next_request_time = start_time
    
    print(f"Sending requests at rate of {REQUESTS_PER_SECOND} req/s (interval: {INTERVAL*1000:.1f}ms)...")
    print()
    
    try:
        while request_count < TOTAL_REQUESTS:
            # Wait until it's time for the next request
            current_time = time.time()
            if current_time < next_request_time:
                time.sleep(next_request_time - current_time)
            
            request_start = time.time()
            request_count += 1
            
            try:
                payload = generate_payment(request_count)
                response = requests.post(
                    API_URL,
                    json=payload,
                    headers={"Content-Type": "application/json"},
                    timeout=5
                )
                
                if response.status_code in [200, 201]:
                    success_count += 1
                else:
                    error_count += 1
                    print(f"[{datetime.now().strftime('%H:%M:%S')}] Request {request_count}: HTTP {response.status_code}")
                
            except requests.exceptions.RequestException as e:
                error_count += 1
                print(f"[{datetime.now().strftime('%H:%M:%S')}] Request {request_count}: ERROR - {e}")
            
            # Log progress every 20 requests
            if request_count % 20 == 0:
                elapsed = time.time() - start_time
                rate = request_count / elapsed if elapsed > 0 else 0
                print(f"[{datetime.now().strftime('%H:%M:%S')}] Sent {request_count}/{TOTAL_REQUESTS} requests "
                      f"(Rate: {rate:.2f} req/s, Success: {success_count}, Errors: {error_count})")
            
            # Schedule next request
            request_duration = time.time() - request_start
            next_request_time = max(next_request_time + INTERVAL, time.time())
            
    except KeyboardInterrupt:
        print("\nTest interrupted by user")
    
    end_time = time.time()
    total_duration = end_time - start_time
    actual_rate = request_count / total_duration if total_duration > 0 else 0
    
    print()
    print("=" * 50)
    print("Load Test Complete")
    print("=" * 50)
    print(f"Finished at: {datetime.now().strftime('%Y-%m-%d %H:%M:%S')}")
    print(f"Total Requests Sent: {request_count}")
    print(f"Successful: {success_count}")
    print(f"Errors: {error_count}")
    print(f"Total Duration: {total_duration:.2f}s")
    print(f"Actual Rate: {actual_rate:.2f} requests/second")
    print()
    print("Note: Check server logs to see:")
    print("  - Queue size growing as requests come in")
    print("  - DB writes happening at 1 RPS rate")
    print("  - Queue draining over time")
    print("=" * 50)

if __name__ == "__main__":
    main()

