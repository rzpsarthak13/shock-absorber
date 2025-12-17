#!/bin/bash
# Comprehensive test script for Redis KV Store and Redis Queue
# This script tests the Shock Absorber with Redis as both the NoSQL KV store and write-back queue

set -e

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}╔════════════════════════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║   Shock Absorber - Redis KV Store & Queue Test Suite     ║${NC}"
echo -e "${BLUE}╚════════════════════════════════════════════════════════════╝${NC}"
echo ""

# Step 1: Check prerequisites
echo -e "${YELLOW}Step 1: Checking prerequisites...${NC}"

# Check Redis
if redis-cli ping > /dev/null 2>&1; then
    echo -e "${GREEN}✓ Redis is running${NC}"
    REDIS_INFO=$(redis-cli INFO server | grep redis_version | cut -d: -f2 | tr -d '\r')
    echo -e "  Redis version: $REDIS_INFO"
else
    echo -e "${RED}✗ Redis is not running${NC}"
    echo -e "${YELLOW}  Please start Redis:${NC}"
    echo -e "    brew services start redis"
    echo -e "    or"
    echo -e "    redis-server"
    exit 1
fi

# Check MySQL
if mysql -u root -ppassword -e "SELECT 1;" > /dev/null 2>&1 || mysql -u root -e "SELECT 1;" > /dev/null 2>&1; then
    echo -e "${GREEN}✓ MySQL is running${NC}"
else
    echo -e "${YELLOW}⚠ MySQL connection check failed (may still work)${NC}"
fi

# Check if test server is running
if curl -s http://localhost:8080/health > /dev/null 2>&1; then
    echo -e "${GREEN}✓ Test server is running${NC}"
    SERVER_RUNNING=true
else
    echo -e "${YELLOW}⚠ Test server is not running${NC}"
    echo -e "${YELLOW}  You may need to start it in another terminal:${NC}"
    echo -e "    ${BLUE}./scripts/start_local.sh${NC}"
    echo -e "    or"
    echo -e "    ${BLUE}go run cmd/test-server/main.go${NC}"
    SERVER_RUNNING=false
fi

echo ""

# Step 2: Clear Redis data (optional)
echo -e "${YELLOW}Step 2: Clearing old Redis data (optional)...${NC}"
read -p "Clear all Redis data? (y/N): " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    redis-cli FLUSHDB
    echo -e "${GREEN}✓ Redis data cleared${NC}"
else
    echo -e "${YELLOW}  Skipping Redis data clear${NC}"
fi
echo ""

# Step 3: Test Redis KV Store
echo -e "${YELLOW}Step 3: Testing Redis KV Store...${NC}"
echo -e "  This tests Redis as the NoSQL database for caching"

if [ "$SERVER_RUNNING" = false ]; then
    echo -e "${RED}✗ Cannot test - server is not running${NC}"
    echo -e "${YELLOW}  Please start the server first${NC}"
    exit 1
fi

# Create a test payment
echo -e "  Creating test payment..."
PAYLOAD='{
  "upi_reference_id": "ref_redis_test_001",
  "amount": 10000,
  "currency": "INR",
  "upi_transaction_id": "txn_redis_test_001",
  "tenant_id": "tenant_001",
  "customer_id": "cust_001",
  "leg": "PAYER",
  "action": "PAY",
  "status": "PENDING",
  "payer": {"vpa": "user@upi", "name": "John Doe"},
  "payees": [{"vpa": "merchant@upi", "name": "Merchant", "amount": 10000}],
  "meta": {"msg_id": "msg_001", "note": "Redis KV Store Test"},
  "resp_meta": {"timestamp": 1234567890}
}'

RESPONSE=$(curl -s -X POST http://localhost:8080/payment \
  -H "Content-Type: application/json" \
  -d "$PAYLOAD")

if echo "$RESPONSE" | grep -q "created successfully"; then
    echo -e "${GREEN}✓ Payment created successfully${NC}"
else
    echo -e "${RED}✗ Failed to create payment${NC}"
    echo "Response: $RESPONSE"
    exit 1
fi

# Wait a moment for Redis write
sleep 1

# Check if data is in Redis
echo -e "  Checking Redis KV Store..."
PAYMENT_KEYS=$(redis-cli KEYS 'payment:*' | wc -l | tr -d ' ')
if [ "$PAYMENT_KEYS" -gt 0 ]; then
    echo -e "${GREEN}✓ Found $PAYMENT_KEYS payment key(s) in Redis${NC}"
    
    # Show first payment key
    FIRST_KEY=$(redis-cli KEYS 'payment:*' | head -1)
    echo -e "  Sample key: ${BLUE}$FIRST_KEY${NC}"
    
    # Check TTL
    TTL=$(redis-cli TTL "$FIRST_KEY")
    if [ "$TTL" -gt 0 ]; then
        echo -e "  TTL: ${BLUE}${TTL}s${NC} (expires in $TTL seconds)"
    elif [ "$TTL" -eq -1 ]; then
        echo -e "  TTL: ${YELLOW}No expiration${NC}"
    fi
else
    echo -e "${RED}✗ No payment keys found in Redis${NC}"
fi

echo ""

# Step 4: Test Redis Queue
echo -e "${YELLOW}Step 4: Testing Redis Queue (Write-Back Queue)...${NC}"
echo -e "  This tests Redis as the queue for write-back operations"

# Check for queue keys
QUEUE_KEYS=$(redis-cli KEYS 'wbq:*' | wc -l | tr -d ' ')
if [ "$QUEUE_KEYS" -gt 0 ]; then
    echo -e "${GREEN}✓ Found $QUEUE_KEYS queue key(s) in Redis${NC}"
    
    # Show queue keys
    echo -e "  Queue keys:"
    redis-cli KEYS 'wbq:*' | while read key; do
        QUEUE_LENGTH=$(redis-cli LLEN "$key")
        echo -e "    ${BLUE}$key${NC}: ${GREEN}$QUEUE_LENGTH${NC} items"
    done
    
    # Show sample queue item
    GLOBAL_QUEUE_KEY="wbq:global"
    if redis-cli EXISTS "$GLOBAL_QUEUE_KEY" | grep -q "1"; then
        echo -e "  Sample queue item from ${BLUE}$GLOBAL_QUEUE_KEY${NC}:"
        SAMPLE_ITEM=$(redis-cli LRANGE "$GLOBAL_QUEUE_KEY" 0 0 | head -1)
        if [ -n "$SAMPLE_ITEM" ]; then
            echo "$SAMPLE_ITEM" | python3 -m json.tool 2>/dev/null || echo "$SAMPLE_ITEM"
        fi
    fi
else
    echo -e "${YELLOW}⚠ No queue keys found (may have been processed already)${NC}"
    echo -e "  This is normal if the drainer has already processed the queue"
fi

echo ""

# Step 5: Test Read Operation
echo -e "${YELLOW}Step 5: Testing Read Operation (from Redis KV Store)...${NC}"

# Read the payment we just created
echo -e "  Reading payment by reference ID..."
READ_RESPONSE=$(curl -s http://localhost:8080/payment/reference/ref_redis_test_001)

if echo "$READ_RESPONSE" | grep -q "upi_reference_id"; then
    echo -e "${GREEN}✓ Payment read successfully from Redis${NC}"
    echo -e "  Response preview:"
    echo "$READ_RESPONSE" | python3 -m json.tool 2>/dev/null | head -10 || echo "$READ_RESPONSE" | head -10
else
    echo -e "${RED}✗ Failed to read payment${NC}"
    echo "Response: $READ_RESPONSE"
fi

echo ""

# Step 6: Summary
echo -e "${BLUE}╔════════════════════════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║                      Test Summary                         ║${NC}"
echo -e "${BLUE}╚════════════════════════════════════════════════════════════╝${NC}"
echo ""

# Redis KV Store Stats
echo -e "${YELLOW}Redis KV Store (NoSQL DB) Stats:${NC}"
TOTAL_KEYS=$(redis-cli DBSIZE)
PAYMENT_KEYS=$(redis-cli KEYS 'payment:*' | wc -l | tr -d ' ')
WAL_KEYS=$(redis-cli KEYS 'wal:*' | wc -l | tr -d ' ')
echo -e "  Total keys in Redis: ${BLUE}$TOTAL_KEYS${NC}"
echo -e "  Payment keys: ${BLUE}$PAYMENT_KEYS${NC}"
echo -e "  WAL keys: ${BLUE}$WAL_KEYS${NC}"

# Redis Queue Stats
echo ""
echo -e "${YELLOW}Redis Queue (Write-Back Queue) Stats:${NC}"
QUEUE_KEYS=$(redis-cli KEYS 'wbq:*' | wc -l | tr -d ' ')
if [ "$QUEUE_KEYS" -gt 0 ]; then
    echo -e "  Queue keys: ${BLUE}$QUEUE_KEYS${NC}"
    redis-cli KEYS 'wbq:*' | while read key; do
        QUEUE_LENGTH=$(redis-cli LLEN "$key")
        echo -e "    ${BLUE}$key${NC}: ${GREEN}$QUEUE_LENGTH${NC} items"
    done
else
    echo -e "  Queue keys: ${GREEN}0${NC} (all items processed)"
fi

echo ""
echo -e "${GREEN}✓ Test completed!${NC}"
echo ""
echo -e "${YELLOW}Useful commands:${NC}"
echo -e "  View all Redis keys: ${BLUE}redis-cli KEYS '*'${NC}"
echo -e "  View payment keys: ${BLUE}redis-cli KEYS 'payment:*'${NC}"
echo -e "  View queue keys: ${BLUE}redis-cli KEYS 'wbq:*'${NC}"
echo -e "  View queue length: ${BLUE}redis-cli LLEN 'wbq:global'${NC}"
echo -e "  View queue item: ${BLUE}redis-cli LRANGE 'wbq:global' 0 0${NC}"
echo -e "  View Redis data: ${BLUE}./scripts/view_redis_data.sh${NC}"
echo ""

