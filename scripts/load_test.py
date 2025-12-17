#!/usr/bin/env python3
"""
Performance Test Script for Shock Absorber
Sends 20 requests per second for 30 seconds (600 total requests)
Tracks how long it takes for all writes to complete in the primary database
"""

import requests
import time
import json
from datetime import datetime
import sys
import subprocess

API_URL = "http://localhost:8080/payment"
REQUESTS_PER_SECOND = 20
DURATION_SECONDS = 30
TOTAL_REQUESTS = REQUESTS_PER_SECOND * DURATION_SECONDS
INTERVAL = 1.0 / REQUESTS_PER_SECOND  # 50ms between requests
DB_CHECK_INTERVAL = 2  # Check DB every 2 seconds

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

def get_db_payment_count():
    """Get the count of payments in the MySQL database"""
    try:
        result = subprocess.run(
            ["docker", "exec", "mysql", "mysql", "-uroot", "-ppassword", "testdb", 
             "-e", "SELECT COUNT(*) as count FROM payment;"],
            capture_output=True,
            text=True,
            timeout=5
        )
        if result.returncode == 0:
            # Parse the output to get the count
            lines = result.stdout.strip().split('\n')
            if len(lines) >= 2:
                # Second line should contain the count
                count_str = lines[1].strip()
                try:
                    return int(count_str)
                except ValueError:
                    return 0
        return 0
    except Exception as e:
        return -1  # Error

def wait_for_all_db_writes(expected_count, start_time):
    """Wait until all writes are complete in the database"""
    print(f"\nWaiting for all {expected_count} writes to complete in database...")
    print("Checking database every 2 seconds...")
    
    last_count = 0
    check_count = 0
    
    while True:
        time.sleep(DB_CHECK_INTERVAL)
        check_count += 1
        current_count = get_db_payment_count()
        
        if current_count == -1:
            print(f"[Check {check_count}] Error checking database, retrying...")
            continue
        
        if current_count >= expected_count:
            elapsed = time.time() - start_time
            print(f"[Check {check_count}] ✓ All {expected_count} payments written to database!")
            print(f"  Current DB count: {current_count}")
            return elapsed
        
        if current_count != last_count:
            elapsed = time.time() - start_time
            print(f"[Check {check_count}] DB count: {current_count}/{expected_count} "
                  f"(+{current_count - last_count} since last check, elapsed: {elapsed:.1f}s)")
            last_count = current_count
        elif check_count % 5 == 0:
            # Print status every 5 checks even if no change
            elapsed = time.time() - start_time
            print(f"[Check {check_count}] DB count: {current_count}/{expected_count} "
                  f"(elapsed: {elapsed:.1f}s)")

def main():
    print("=" * 70)
    print("Shock Absorber Performance Test")
    print("=" * 70)
    print(f"API URL: {API_URL}")
    print(f"Request Rate: {REQUESTS_PER_SECOND} requests/second")
    print(f"Duration: {DURATION_SECONDS} seconds")
    print(f"Total Requests: {TOTAL_REQUESTS}")
    print(f"DB Write Rate: Configured in test-server (check config)")
    print(f"Expected: All requests should be accepted immediately")
    print(f"Expected: DB writes will be processed by background drainer")
    print("=" * 70)
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
    request_duration = end_time - start_time
    actual_rate = request_count / request_duration if request_duration > 0 else 0
    
    print()
    print("=" * 70)
    print("Request Phase Complete")
    print("=" * 70)
    print(f"Finished sending requests at: {datetime.now().strftime('%Y-%m-%d %H:%M:%S')}")
    print(f"Total Requests Sent: {request_count}")
    print(f"Successful: {success_count}")
    print(f"Errors: {error_count}")
    print(f"Request Duration: {request_duration:.2f}s")
    print(f"Actual Rate: {actual_rate:.2f} requests/second")
    print()
    
    # Now wait for all DB writes to complete
    if success_count > 0:
        db_write_duration = wait_for_all_db_writes(success_count, start_time)
        total_time = time.time() - start_time
        
        print()
        print("=" * 70)
        print("Final Results")
        print("=" * 70)
        print(f"Total Requests: {request_count}")
        print(f"Successful Requests: {success_count}")
        print(f"Request Phase Duration: {request_duration:.2f}s")
        print(f"DB Write Completion Time: {db_write_duration:.2f}s")
        print(f"Total Test Duration: {total_time:.2f}s")
        print()
        print(f"✓ All {success_count} payments written to primary database!")
        print(f"  Time to complete all DB writes: {db_write_duration:.2f} seconds")
        print(f"  Average DB write rate: {success_count/db_write_duration:.2f} writes/second")
        print("=" * 70)
    else:
        print("No successful requests to track in database.")
        print("=" * 70)

if __name__ == "__main__":
    main()

