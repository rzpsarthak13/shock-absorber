#!/bin/bash
# Script to view Redis Queue (Write-Back Queue) data

REDIS_HOST="${REDIS_HOST:-localhost}"
REDIS_PORT="${REDIS_PORT:-6379}"
REDIS_PASSWORD="${REDIS_PASSWORD:-}"

echo "=== Redis Queue (Write-Back Queue) Viewer ==="
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

echo "=== Queue Keys ==="
QUEUE_KEYS=$($REDIS_CMD KEYS 'wbq:*')
if [ -z "$QUEUE_KEYS" ]; then
    echo "No queue keys found (all items may have been processed)"
    echo ""
else
    echo "Found queue keys:"
    echo "$QUEUE_KEYS"
    echo ""
    
    echo "=== Queue Statistics ==="
    for key in $QUEUE_KEYS; do
        LENGTH=$($REDIS_CMD LLEN "$key")
        echo "Queue: $key"
        echo "  Length: $LENGTH items"
        echo ""
    done
    
    echo "=== Sample Queue Items ==="
    for key in $QUEUE_KEYS; do
        LENGTH=$($REDIS_CMD LLEN "$key")
        if [ "$LENGTH" -gt 0 ]; then
            echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
            echo "Queue: $key"
            echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
            echo ""
            echo "First item (head of queue):"
            $REDIS_CMD LRANGE "$key" 0 0 | python3 -m json.tool 2>/dev/null || $REDIS_CMD LRANGE "$key" 0 0
            echo ""
            
            if [ "$LENGTH" -gt 1 ]; then
                echo "Last item (tail of queue):"
                $REDIS_CMD LRANGE "$key" -1 -1 | python3 -m json.tool 2>/dev/null || $REDIS_CMD LRANGE "$key" -1 -1
                echo ""
            fi
            
            # Show up to 5 items
            if [ "$LENGTH" -gt 0 ] && [ "$LENGTH" -le 5 ]; then
                echo "All items in queue:"
                $REDIS_CMD LRANGE "$key" 0 -1 | python3 -m json.tool 2>/dev/null || $REDIS_CMD LRANGE "$key" 0 -1
                echo ""
            elif [ "$LENGTH" -gt 5 ]; then
                echo "First 3 items:"
                $REDIS_CMD LRANGE "$key" 0 2 | python3 -m json.tool 2>/dev/null || $REDIS_CMD LRANGE "$key" 0 2
                echo ""
                echo "... ($((LENGTH - 3)) more items)"
                echo ""
            fi
        fi
    done
fi

echo "=== Useful Commands ==="
echo ""
echo "View all queue keys:"
echo "  $REDIS_CMD KEYS 'wbq:*'"
echo ""
echo "Get queue length:"
echo "  $REDIS_CMD LLEN 'wbq:global'"
echo "  $REDIS_CMD LLEN 'wbq:payment'"
echo ""
echo "View first item (without removing):"
echo "  $REDIS_CMD LRANGE 'wbq:global' 0 0"
echo ""
echo "View all items:"
echo "  $REDIS_CMD LRANGE 'wbq:global' 0 -1"
echo ""
echo "Pop first item (removes from queue):"
echo "  $REDIS_CMD LPOP 'wbq:global'"
echo ""
echo "Clear a queue:"
echo "  $REDIS_CMD DEL 'wbq:global'"
echo ""

