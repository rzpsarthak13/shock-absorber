#!/bin/bash
# Script to create a test payment and then show how it's stored in Redis

echo "=== Creating Test Payment ==="
echo ""

# Create a test payment
PAYLOAD='{
  "upi_reference_id": "ref_test_001",
  "amount": 10000,
  "currency": "INR",
  "upi_transaction_id": "txn_test_001",
  "tenant_id": "tenant_001",
  "customer_id": "cust_001",
  "leg": "PAYER",
  "action": "PAY",
  "status": "PENDING",
  "payer": {"vpa": "user@upi", "name": "John Doe"},
  "payees": [{"vpa": "merchant@upi", "name": "Merchant", "amount": 10000}],
  "meta": {"msg_id": "msg_001", "note": "Test payment"},
  "resp_meta": {"timestamp": 1234567890}
}'

echo "Sending POST request to create payment..."
RESPONSE=$(curl -s -X POST http://localhost:8080/payment \
  -H "Content-Type: application/json" \
  -d "$PAYLOAD")

echo "Response: $RESPONSE"
echo ""

# Wait a moment for Redis write
sleep 1

echo "=== Redis Keys ==="
echo "Listing all payment keys:"
redis-cli KEYS 'payment:*'
echo ""

echo "=== Redis Key-Value Pairs ==="
echo "Showing how data is stored in Redis:"
echo ""

for key in $(redis-cli KEYS 'payment:*'); do
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "Key: $key"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "Value (JSON):"
    redis-cli GET "$key" | python3 -m json.tool 2>/dev/null || redis-cli GET "$key"
    echo ""
    echo "Raw Value:"
    redis-cli GET "$key"
    echo ""
    echo "TTL (Time To Live):"
    redis-cli TTL "$key"
    echo ""
done

echo "=== WAL (Write-Ahead Log) Keys ==="
echo "Listing WAL keys:"
redis-cli KEYS 'wal:*' | head -5
echo ""

echo "=== Summary ==="
echo "Total Redis keys: $(redis-cli DBSIZE)"
echo "Payment keys: $(redis-cli KEYS 'payment:*' | wc -l | tr -d ' ')"
echo ""

