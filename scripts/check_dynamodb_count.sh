#!/bin/bash
# Script to check the number of entries in DynamoDB table

ENDPOINT="${DYNAMODB_ENDPOINT:-http://localhost:4566}"
TABLE_NAME="${DYNAMODB_TABLE:-shock-absorber-cache}"

echo "=== DynamoDB Item Count ==="
echo "Table: $TABLE_NAME"
echo "Endpoint: $ENDPOINT"
echo ""

# Method 1: Using COUNT scan (faster, doesn't return items)
echo "Counting items (using COUNT scan)..."
COUNT=$(curl -s "$ENDPOINT/" \
  -H "Content-Type: application/x-amz-json-1.0" \
  -H "X-Amz-Target: DynamoDB_20120810.Scan" \
  -d "{\"TableName\": \"$TABLE_NAME\", \"Select\": \"COUNT\"}" | \
  python3 -c "import sys, json; data=json.load(sys.stdin); print(data.get('Count', 0))" 2>/dev/null)

if [ -n "$COUNT" ]; then
    echo "✓ Total items: $COUNT"
else
    echo "✗ Failed to get count"
fi

echo ""

# Method 2: Get actual items (slower, but shows sample data)
echo "Sample items (first 5):"
curl -s "$ENDPOINT/" \
  -H "Content-Type: application/x-amz-json-1.0" \
  -H "X-Amz-Target: DynamoDB_20120810.Scan" \
  -d "{\"TableName\": \"$TABLE_NAME\", \"Limit\": 5}" | \
  python3 -c "
import sys, json
data = json.load(sys.stdin)
items = data.get('Items', [])
print(f'Showing {len(items)} sample items:')
for i, item in enumerate(items[:5], 1):
    key = item.get('key', {}).get('S', 'N/A')
    print(f'  {i}. Key: {key[:50]}...' if len(key) > 50 else f'  {i}. Key: {key}')
" 2>/dev/null

echo ""
echo "=== Useful Commands ==="
echo ""
echo "Quick count:"
echo "  curl -s 'http://localhost:4566/' \\"
echo "    -H 'Content-Type: application/x-amz-json-1.0' \\"
echo "    -H 'X-Amz-Target: DynamoDB_20120810.Scan' \\"
echo "    -d '{\"TableName\": \"$TABLE_NAME\", \"Select\": \"COUNT\"}' | python3 -m json.tool"
echo ""
echo "Get all items:"
echo "  curl -s 'http://localhost:4566/' \\"
echo "    -H 'Content-Type: application/x-amz-json-1.0' \\"
echo "    -H 'X-Amz-Target: DynamoDB_20120810.Scan' \\"
echo "    -d '{\"TableName\": \"$TABLE_NAME\"}' | python3 -m json.tool"
echo ""

