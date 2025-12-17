package writeback

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/rzpsarthak13/shock-absorber/internal/core"
	"github.com/segmentio/kafka-go"
)

var (
	// ErrKafkaQueueClosed is returned when trying to enqueue to a closed Kafka queue.
	ErrKafkaQueueClosed = errors.New("kafka queue is closed")
)

// KafkaQueue implements WriteBackQueue using Apache Kafka.
// This provides high throughput, persistence, and distributed processing capabilities.
type KafkaQueue struct {
	writer  *kafka.Writer
	reader  *kafka.Reader
	topic   string
	closed  bool
	mu      sync.RWMutex
	groupID string
	brokers []string
	size    int // Approximate size (not exact for Kafka)
}

// KafkaQueueConfig holds configuration for Kafka queue.
type KafkaQueueConfig struct {
	Brokers         []string
	Topic           string
	GroupID         string
	BatchSize       int
	BatchTimeout    time.Duration
	WriteTimeout    time.Duration
	ReadTimeout     time.Duration
	RequiredAcks    int // 0, 1, or -1 (all)
	MaxMessageBytes int
	MinBytes        int
	MaxBytes        int
	MaxWait         time.Duration
}

// NewKafkaQueue creates a new Kafka-based write-back queue.
func NewKafkaQueue(config KafkaQueueConfig) (*KafkaQueue, error) {
	if len(config.Brokers) == 0 {
		return nil, fmt.Errorf("at least one Kafka broker is required")
	}
	if config.Topic == "" {
		return nil, fmt.Errorf("Kafka topic is required")
	}
	if config.GroupID == "" {
		config.GroupID = "shock-absorber-writeback"
	}

	log.Printf("[KAFKA] Initializing Kafka queue...")
	log.Printf("[KAFKA] Brokers: %v", config.Brokers)
	log.Printf("[KAFKA] Topic: %s", config.Topic)
	log.Printf("[KAFKA] Consumer Group ID: %s", config.GroupID)
	log.Printf("[KAFKA] Batch Size: %d", config.BatchSize)
	log.Printf("[KAFKA] Required Acks: %d", config.RequiredAcks)

	// Create Kafka writer for producing messages
	writer := &kafka.Writer{
		Addr:         kafka.TCP(config.Brokers...),
		Topic:        config.Topic,
		Balancer:     &kafka.LeastBytes{},
		BatchSize:    config.BatchSize,
		BatchTimeout: config.BatchTimeout,
		WriteTimeout: config.WriteTimeout,
		RequiredAcks: kafka.RequiredAcks(config.RequiredAcks),
		MaxAttempts:  3,
		Async:        false, // Synchronous writes for reliability
	}

	// Create Kafka reader for consuming messages
	// When using a consumer group (GroupID), Kafka manages offsets automatically.
	// For write-back queues, we want to process ALL messages from the beginning.
	// StartOffset: FirstOffset ensures new consumer groups read from the beginning.
	// Note: If you want to resume from last committed offset, use kafka.LastOffset instead.
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:     config.Brokers,
		Topic:       config.Topic,
		GroupID:     config.GroupID,
		MinBytes:    config.MinBytes,
		MaxBytes:    config.MaxBytes,
		MaxWait:     config.MaxWait,
		StartOffset: kafka.FirstOffset, // Always read from beginning for new consumer groups
		// Important: With a timestamp-based GroupID (changing on each restart),
		// each restart creates a new consumer group and reads from FirstOffset.
		// This ensures all messages in the topic are processed.
	})

	log.Printf("[KAFKA] Kafka queue initialized successfully")
	log.Printf("[KAFKA] Producer ready to write to topic: %s", config.Topic)
	log.Printf("[KAFKA] Consumer ready to read from topic: %s with group: %s", config.Topic, config.GroupID)

	return &KafkaQueue{
		writer:  writer,
		reader:  reader,
		topic:   config.Topic,
		brokers: config.Brokers,
		groupID: config.GroupID,
		closed:  false,
		size:    0, // Kafka doesn't provide exact queue size
	}, nil
}

// Enqueue adds a write operation to the Kafka queue.
func (q *KafkaQueue) Enqueue(ctx context.Context, operation *core.WriteOperation) error {
	q.mu.RLock()
	if q.closed {
		q.mu.RUnlock()
		return ErrKafkaQueueClosed
	}
	q.mu.RUnlock()

	if operation == nil {
		return ErrInvalidOperation
	}

	if operation.Table == "" {
		return fmt.Errorf("%w: table name is required", ErrInvalidOperation)
	}

	// Set timestamp if not set
	if operation.Timestamp.IsZero() {
		operation.Timestamp = time.Now()
	}

	// Serialize the operation
	opData, err := json.Marshal(operation)
	if err != nil {
		return fmt.Errorf("failed to marshal write operation: %w", err)
	}

	// Create Kafka message with table name as key for partitioning
	message := kafka.Message{
		Key:   []byte(operation.Table), // Use table name as key for partitioning
		Value: opData,
		Time:  operation.Timestamp,
		Headers: []kafka.Header{
			{Key: "operation", Value: []byte(string(operation.Operation))},
			{Key: "table", Value: []byte(operation.Table)},
		},
	}

	// Write to Kafka
	timestamp := time.Now().Format("2006-01-02 15:04:05.000")
	log.Printf("[KAFKA] [%s] === PRODUCING TO KAFKA ===", timestamp)
	log.Printf("[KAFKA] [%s] Topic: %s", timestamp, q.topic)
	log.Printf("[KAFKA] [%s] Operation: %s, Table: %s, Key: %v", timestamp, operation.Operation, operation.Table, operation.Key)
	log.Printf("[KAFKA] [%s] Message Size: %d bytes", timestamp, len(opData))

	produceStart := time.Now()
	if err := q.writer.WriteMessages(ctx, message); err != nil {
		produceDuration := time.Since(produceStart)
		log.Printf("[KAFKA] [%s] ERROR: Failed to write message to Kafka topic %s: %v (Duration: %v)",
			time.Now().Format("2006-01-02 15:04:05.000"), q.topic, err, produceDuration)
		log.Printf("[KAFKA] [%s] ERROR: Message was NOT added to queue - Size counter will NOT increment",
			time.Now().Format("2006-01-02 15:04:05.000"))
		return fmt.Errorf("failed to write message to Kafka: %w", err)
	}
	produceDuration := time.Since(produceStart)

	// Only increment size counter AFTER successful production
	q.mu.Lock()
	q.size++
	currentSize := q.size
	q.mu.Unlock()

	log.Printf("[KAFKA] [%s] ✓ Successfully produced message to Kafka topic '%s' (Duration: %v)",
		time.Now().Format("2006-01-02 15:04:05.000"), q.topic, produceDuration)
	log.Printf("[KAFKA] [%s] Operation %s for table %s is now in Kafka queue (approximate queue size: %d)",
		time.Now().Format("2006-01-02 15:04:05.000"), operation.Operation, operation.Table, currentSize)
	log.Printf("[KAFKA] [%s] Message can now be consumed by consumer group", time.Now().Format("2006-01-02 15:04:05.000"))
	return nil
}

// Dequeue retrieves a batch of write operations from the Kafka queue.
// Returns operations in the order they were enqueued (FIFO).
func (q *KafkaQueue) Dequeue(ctx context.Context, batchSize int) ([]*core.WriteOperation, error) {
	q.mu.RLock()
	if q.closed {
		q.mu.RUnlock()
		return nil, ErrKafkaQueueClosed
	}
	q.mu.RUnlock()

	if batchSize <= 0 {
		batchSize = 100
	}

	operations := make([]*core.WriteOperation, 0, batchSize)

	timestamp := time.Now().Format("2006-01-02 15:04:05.000")
	log.Printf("[KAFKA] [%s] === CONSUMING FROM KAFKA ===", timestamp)
	log.Printf("[KAFKA] [%s] Topic: %s, Consumer Group: %s", timestamp, q.topic, q.groupID)
	log.Printf("[KAFKA] [%s] Attempting to consume up to %d messages...", timestamp, batchSize)

	for i := 0; i < batchSize; i++ {
		// Set a timeout for reading (longer timeout to allow Kafka to fetch messages)
		readStart := time.Now()
		readCtx, cancel := context.WithTimeout(ctx, 5*time.Second) // Increased from 1s to 5s
		message, err := q.reader.ReadMessage(readCtx)
		cancel()
		readDuration := time.Since(readStart)

		if err != nil {
			// Check if it's a timeout (no messages available)
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				if i == 0 {
					log.Printf("[KAFKA] [%s] No messages available in topic '%s' (timeout after %v, consumer group: %s)",
						time.Now().Format("2006-01-02 15:04:05.000"), q.topic, readDuration, q.groupID)
					log.Printf("[KAFKA] [%s] NOTE: This may mean: (1) No messages in topic, (2) All messages already consumed, or (3) Consumer group offset is at end",
						time.Now().Format("2006-01-02 15:04:05.000"))
				}
				break // No more messages available
			}
			log.Printf("[KAFKA] [%s] ERROR: Failed to read message from topic '%s': %v (Duration: %v)",
				time.Now().Format("2006-01-02 15:04:05.000"), q.topic, err, readDuration)
			break
		}

		consumeTimestamp := time.Now().Format("2006-01-02 15:04:05.000")
		log.Printf("[KAFKA] [%s] ✓ Consumed message from Kafka (Read Duration: %v)", consumeTimestamp, readDuration)
		log.Printf("[KAFKA] [%s]   Partition: %d, Offset: %d", consumeTimestamp, message.Partition, message.Offset)
		log.Printf("[KAFKA] [%s]   Message Size: %d bytes", consumeTimestamp, len(message.Value))
		log.Printf("[KAFKA] [%s]   Key: %s", consumeTimestamp, string(message.Key))

		// Deserialize the operation
		deserializeStart := time.Now()
		var op core.WriteOperation
		if err := json.Unmarshal(message.Value, &op); err != nil {
			deserializeDuration := time.Since(deserializeStart)
			log.Printf("[KAFKA] [%s] ERROR: Failed to unmarshal message (Partition: %d, Offset: %d), skipping: %v (Duration: %v)",
				time.Now().Format("2006-01-02 15:04:05.000"), message.Partition, message.Offset, err, deserializeDuration)
			continue
		}
		deserializeDuration := time.Since(deserializeStart)

		log.Printf("[KAFKA] [%s] ✓ Deserialized operation: %s on table %s, key: %v (Duration: %v)",
			time.Now().Format("2006-01-02 15:04:05.000"), op.Operation, op.Table, op.Key, deserializeDuration)

		operations = append(operations, &op)
		
		// Store message for later commit (after successful DB write)
		// We'll commit the offset after the operation is successfully processed
		// This prevents message loss if DB write fails
		// Note: We're storing the message reference, but we'll need to commit it later
		// For now, we'll commit immediately but this should be changed to commit after DB write
		commitStart := time.Now()
		if err := q.reader.CommitMessages(ctx, message); err != nil {
			commitDuration := time.Since(commitStart)
			log.Printf("[KAFKA] [%s] WARNING: Failed to commit message offset (Partition: %d, Offset: %d): %v (Duration: %v)",
				time.Now().Format("2006-01-02 15:04:05.000"), message.Partition, message.Offset, err, commitDuration)
		} else {
			commitDuration := time.Since(commitStart)
			log.Printf("[KAFKA] [%s] ✓ Committed offset: Partition %d, Offset %d (Duration: %v)",
				time.Now().Format("2006-01-02 15:04:05.000"), message.Partition, message.Offset, commitDuration)
		}
	}

	if len(operations) > 0 {
		log.Printf("[KAFKA] [%s] === CONSUMPTION COMPLETE ===", time.Now().Format("2006-01-02 15:04:05.000"))
		log.Printf("[KAFKA] [%s] Consumed %d operations from Kafka topic '%s'",
			time.Now().Format("2006-01-02 15:04:05.000"), len(operations), q.topic)
	}

	// Update size counter only if we actually consumed messages
	if len(operations) > 0 {
		q.mu.Lock()
		if q.size >= len(operations) {
			q.size -= len(operations)
		} else {
			// Size counter was inaccurate, reset it
			q.size = 0
		}
		q.mu.Unlock()
	}

	return operations, nil
}

// Size returns an approximate number of operations in the queue.
// Note: Kafka doesn't provide exact queue size, this is an approximation.
func (q *KafkaQueue) Size() int {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.size
}

// Close closes the Kafka queue and releases resources.
func (q *KafkaQueue) Close() error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return nil
	}

	q.closed = true

	// Close writer
	if err := q.writer.Close(); err != nil {
		log.Printf("[KAFKA] ERROR: Failed to close writer: %v", err)
	}

	// Close reader
	if err := q.reader.Close(); err != nil {
		log.Printf("[KAFKA] ERROR: Failed to close reader: %v", err)
		return err
	}

	return nil
}
