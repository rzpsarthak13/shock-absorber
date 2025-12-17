#!/bin/bash
# Script to reset Kafka consumer group offset
# This allows reprocessing messages that were already consumed

CONSUMER_GROUP="shock-absorber-writeback"
TOPIC="shock-absorber-writeback"
KAFKA_CONTAINER=$(docker ps | grep kafka | awk '{print $1}')

if [ -z "$KAFKA_CONTAINER" ]; then
    echo "ERROR: Kafka container not found"
    exit 1
fi

echo "Resetting consumer group '$CONSUMER_GROUP' for topic '$TOPIC'"
echo "This will allow reprocessing of messages"

# Note: The apache/kafka image might not have kafka-consumer-groups.sh
# You may need to use a different Kafka image or install Kafka tools
# For now, we'll provide instructions

echo ""
echo "To reset the consumer group, you can:"
echo "1. Stop the test server"
echo "2. Delete the consumer group:"
echo "   docker exec $KAFKA_CONTAINER kafka-consumer-groups.sh --bootstrap-server localhost:9092 --group $CONSUMER_GROUP --delete"
echo "3. Or change the consumer group ID in your config to use a new group"
echo ""
echo "Alternatively, you can change the GroupID in cmd/test-server/main.go to use a new consumer group"

