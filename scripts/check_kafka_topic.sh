#!/bin/bash
# Script to check Kafka topic and consumer group status

KAFKA_CONTAINER=$(docker ps | grep kafka | awk '{print $1}')
TOPIC="shock-absorber-writeback"
GROUP_ID="shock-absorber-writeback"

if [ -z "$KAFKA_CONTAINER" ]; then
    echo "ERROR: Kafka container not found"
    exit 1
fi

echo "=== Kafka Topic Check ==="
echo "Topic: $TOPIC"
echo "Container: $KAFKA_CONTAINER"
echo ""

echo "To check if messages exist in Kafka, you can:"
echo ""
echo "1. Check if topic exists:"
echo "   docker exec $KAFKA_CONTAINER kafka-topics.sh --bootstrap-server localhost:9092 --list"
echo ""
echo "2. Check topic details:"
echo "   docker exec $KAFKA_CONTAINER kafka-topics.sh --bootstrap-server localhost:9092 --describe --topic $TOPIC"
echo ""
echo "3. Consume messages from beginning (to see if any exist):"
echo "   docker exec $KAFKA_CONTAINER kafka-console-consumer.sh --bootstrap-server localhost:9092 --topic $TOPIC --from-beginning --max-messages 10"
echo ""
echo "4. Check consumer group offsets:"
echo "   docker exec $KAFKA_CONTAINER kafka-consumer-groups.sh --bootstrap-server localhost:9092 --group $GROUP_ID --describe"
echo ""
echo "NOTE: The apache/kafka image might not have these scripts."
echo "If not available, check your server logs for 'PRODUCING TO KAFKA' messages."

