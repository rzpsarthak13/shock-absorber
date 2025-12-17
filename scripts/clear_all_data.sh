#!/bin/bash
# Script to clear all Redis keys and database records

echo "=== Clearing All Data ==="
echo ""

# Clear Redis
echo "1. Clearing Redis..."
redis-cli FLUSHDB
if [ $? -eq 0 ]; then
    echo "   ✓ Redis cleared successfully"
    redis-cli DBSIZE
else
    echo "   ✗ Failed to clear Redis"
fi
echo ""

# Clear Database
echo "2. Clearing payment table in MySQL..."
docker exec mysql mysql -uroot -ppassword testdb -e "TRUNCATE TABLE payment;" 2>/dev/null
if [ $? -eq 0 ]; then
    echo "   ✓ Database cleared successfully"
    docker exec mysql mysql -uroot -ppassword testdb -e "SELECT COUNT(*) as payment_count FROM payment;" 2>/dev/null
else
    echo "   ✗ Failed to clear database"
fi
echo ""

echo "=== All Data Cleared ==="
echo ""
echo "Now you can:"
echo "1. Start your test server: go run cmd/test-server/main.go"
echo "2. Create a payment: curl -X POST http://localhost:8080/payment -H 'Content-Type: application/json' -d '{...}'"
echo "3. View Redis data: redis-cli KEYS 'payment:*'"
echo "4. View specific key: redis-cli GET 'payment:<id>' | python3 -m json.tool"

