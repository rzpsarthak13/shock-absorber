#!/bin/bash

# Performance Test Script for Shock Absorber
# Sends 20 requests per second for 30 seconds (600 total requests)
# DB is configured to write at 1 request per second

API_URL="http://localhost:8080/payment"
REQUESTS_PER_SECOND=20
DURATION_SECONDS=30
TOTAL_REQUESTS=$((REQUESTS_PER_SECOND * DURATION_SECONDS))

echo "=========================================="
echo "Shock Absorber Performance Test"
echo "=========================================="
echo "API URL: $API_URL"
echo "Request Rate: $REQUESTS_PER_SECOND requests/second"
echo "Duration: $DURATION_SECONDS seconds"
echo "Total Requests: $TOTAL_REQUESTS"
echo "DB Write Rate: 1 request/second (configured)"
echo "Expected: All requests should be accepted immediately"
echo "Expected: DB writes will take ~$TOTAL_REQUESTS seconds to complete"
echo "=========================================="
echo ""

# Check if server is running
if ! curl -s "$API_URL" > /dev/null 2>&1; then
    echo "ERROR: Server is not running at $API_URL"
    echo "Please start the test server first: go run cmd/test-server/main.go"
    exit 1
fi

echo "Starting load test at $(date '+%Y-%m-%d %H:%M:%S')"
echo ""

# Counter for requests
REQUEST_COUNT=0
START_TIME=$(date +%s)

# Function to generate a unique payment payload
generate_payment() {
    local ref_id="perf_test_$(date +%s%N)_$REQUEST_COUNT"
    local payment_id="pay_$(date +%s%N)_$REQUEST_COUNT"
    
    cat <<EOF
{
  "id": "$payment_id",
  "upi_reference_id": "$ref_id",
  "amount": 10000,
  "currency": "INR",
  "upi_transaction_id": "txn_perf_$REQUEST_COUNT",
  "tenant_id": "tenant_001",
  "customer_id": "cust_001",
  "leg": "PAYER",
  "action": "PAY",
  "status": "PENDING",
  "payer": {
    "vpa": "user@upi",
    "name": "Test User $REQUEST_COUNT"
  },
  "payees": [
    {
      "vpa": "merchant@upi",
      "name": "Merchant",
      "amount": 10000
    }
  ],
  "meta": {
    "msg_id": "msg_perf_$REQUEST_COUNT",
    "note": "Performance test request #$REQUEST_COUNT"
  },
  "resp_meta": {
    "timestamp": $(date +%s)
  }
}
EOF
}

# Calculate interval between requests (in milliseconds)
# 20 requests/second = 1 request every 50ms
INTERVAL_MS=50
INTERVAL_SEC=$(echo "scale=3; $INTERVAL_MS / 1000" | bc)

echo "Sending requests at rate of $REQUESTS_PER_SECOND req/s (interval: ${INTERVAL_MS}ms)..."
echo ""

# Send requests
while [ $REQUEST_COUNT -lt $TOTAL_REQUESTS ]; do
    REQUEST_START=$(date +%s.%N)
    
    # Send request
    RESPONSE=$(curl -s -w "\n%{http_code}" -X POST "$API_URL" \
        -H "Content-Type: application/json" \
        -d "$(generate_payment)")
    
    HTTP_CODE=$(echo "$RESPONSE" | tail -n1)
    BODY=$(echo "$RESPONSE" | sed '$d')
    
    REQUEST_COUNT=$((REQUEST_COUNT + 1))
    
    # Log every 20 requests
    if [ $((REQUEST_COUNT % 20)) -eq 0 ]; then
        ELAPSED=$(date +%s)
        ELAPSED_TIME=$((ELAPSED - START_TIME))
        RATE=$(echo "scale=2; $REQUEST_COUNT / $ELAPSED_TIME" | bc)
        echo "[$(date '+%H:%M:%S')] Sent $REQUEST_COUNT/$TOTAL_REQUESTS requests (Rate: ${RATE} req/s, HTTP: $HTTP_CODE)"
    fi
    
    # Calculate sleep time to maintain rate
    REQUEST_END=$(date +%s.%N)
    REQUEST_DURATION=$(echo "$REQUEST_END - $REQUEST_START" | bc)
    SLEEP_TIME=$(echo "$INTERVAL_SEC - $REQUEST_DURATION" | bc)
    
    # Only sleep if we need to (don't sleep if request took longer than interval)
    if (( $(echo "$SLEEP_TIME > 0" | bc -l) )); then
        sleep "$SLEEP_TIME"
    fi
done

END_TIME=$(date +%s)
TOTAL_DURATION=$((END_TIME - START_TIME))
ACTUAL_RATE=$(echo "scale=2; $REQUEST_COUNT / $TOTAL_DURATION" | bc)

echo ""
echo "=========================================="
echo "Load Test Complete"
echo "=========================================="
echo "Finished at: $(date '+%Y-%m-%d %H:%M:%S')"
echo "Total Requests Sent: $REQUEST_COUNT"
echo "Total Duration: ${TOTAL_DURATION}s"
echo "Actual Rate: ${ACTUAL_RATE} requests/second"
echo ""
echo "Note: Check server logs to see:"
echo "  - Queue size growing as requests come in"
echo "  - DB writes happening at 1 RPS rate"
echo "  - Queue draining over time"
echo "=========================================="

