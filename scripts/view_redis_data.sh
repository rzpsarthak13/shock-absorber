#!/bin/bash
# Script to view Redis data for Shock Absorber

REDIS_HOST="${REDIS_HOST:-localhost}"
REDIS_PORT="${REDIS_PORT:-6379}"
REDIS_PASSWORD="${REDIS_PASSWORD:-}"

echo "=== Redis Data Viewer for Shock Absorber ==="
echo "Connecting to Redis at $REDIS_HOST:$REDIS_PORT"
echo ""

# Check if redis-cli is available
if ! command -v redis-cli &> /dev/null; then
    echo "ERROR: redis-cli not found. Install Redis client:"
    echo "  brew install redis"
    echo "  or"
    echo "  apt-get install redis-tools"
    exit 1
fi

# Build redis-cli command
REDIS_CMD="redis-cli -h $REDIS_HOST -p $REDIS_PORT"
if [ -n "$REDIS_PASSWORD" ]; then
    REDIS_CMD="$REDIS_CMD -a $REDIS_PASSWORD"
fi

echo "=== 1. List All Keys ==="
echo "Command: $REDIS_CMD KEYS '*'"
echo ""
$REDIS_CMD KEYS '*' | head -20
echo ""

echo "=== 2. Payment Table Keys ==="
echo "Command: $REDIS_CMD KEYS 'payment:*'"
echo ""
$REDIS_CMD KEYS 'payment:*' | head -20
echo ""

echo "=== 3. WAL (Write-Ahead Log) Keys ==="
echo "Command: $REDIS_CMD KEYS 'wal:*'"
echo ""
$REDIS_CMD KEYS 'wal:*' | head -20
echo ""

echo "=== 4. Get a Specific Payment (replace KEY with actual key) ==="
echo "Example: $REDIS_CMD GET 'payment:pay_1234567890'"
echo ""
echo "To view a specific payment, run:"
echo "  $REDIS_CMD GET 'payment:<payment_id>'"
echo ""

echo "=== 5. Get All Payment Keys with Values ==="
echo "This will show all payment keys and their JSON values:"
echo ""
for key in $($REDIS_CMD KEYS 'payment:*' | head -10); do
    echo "Key: $key"
    $REDIS_CMD GET "$key" | python3 -m json.tool 2>/dev/null || $REDIS_CMD GET "$key"
    echo "---"
done

echo ""
echo "=== 6. Check TTL (Time To Live) for Keys ==="
echo "Command: $REDIS_CMD TTL 'payment:<key>'"
echo ""
echo "To check TTL for a key:"
echo "  $REDIS_CMD TTL 'payment:pay_1234567890'"
echo "  (-1 = no expiration, -2 = key doesn't exist, positive number = seconds until expiration)"
echo ""

echo "=== 7. Count Total Keys ==="
echo "Command: $REDIS_CMD DBSIZE"
echo ""
$REDIS_CMD DBSIZE
echo ""

echo "=== Useful Commands ==="
echo ""
echo "View all keys matching pattern:"
echo "  $REDIS_CMD KEYS 'payment:*'"
echo ""
echo "Get a specific key's value:"
echo "  $REDIS_CMD GET 'payment:pay_1234567890'"
echo ""
echo "Get value as pretty JSON:"
echo "  $REDIS_CMD GET 'payment:pay_1234567890' | python3 -m json.tool"
echo ""
echo "Check if key exists:"
echo "  $REDIS_CMD EXISTS 'payment:pay_1234567890'"
echo ""
echo "Get key TTL:"
echo "  $REDIS_CMD TTL 'payment:pay_1234567890'"
echo ""
echo "Delete a key:"
echo "  $REDIS_CMD DEL 'payment:pay_1234567890'"
echo ""
echo "Delete all payment keys:"
echo "  $REDIS_CMD --eval 'return redis.call(\"del\", unpack(redis.call(\"keys\", ARGV[1])))' 0 'payment:*'"
echo ""

