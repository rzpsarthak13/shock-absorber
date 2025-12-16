package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"reflect"
	"syscall"
	"time"

	"golang.org/x/time/rate"

	"github.com/razorpay/shock-absorber/internal/core"
	"github.com/razorpay/shock-absorber/internal/writeback"
	"github.com/razorpay/shock-absorber/pkg/shockabsorber"
)

var (
	client        shockabsorber.Client
	tableInstance shockabsorber.Table
	internalTable core.Table // Store internal table for drainer access
)

func main() {
	// Initialize client
	config := shockabsorber.DefaultConfig()
	config.KVStore.Endpoints = []string{"localhost:6379"}
	config.Database.Host = "localhost"
	config.Database.Port = 3306
	config.Database.Database = "testdb"
	config.Database.Username = "root"
	config.Database.Password = "password"
	// Queue configuration
	// Options: "memory", "redis", "kafka"
	// For production with 200 TPS, use "kafka"
	config.WriteBack.QueueType = "kafka" // Change to "kafka" to use Kafka
	config.WriteBack.DefaultTTL = 1 * time.Hour
	config.WriteBack.DrainRate = 1 // 1 RPS for DB writes (for performance test: 20 req/s -> 1 DB write/s)

	// Kafka configuration (only used when QueueType is "kafka")
	config.WriteBack.KafkaConfig = shockabsorber.KafkaConfig{
		Brokers:         []string{"localhost:9092"},
		Topic:           "shock-absorber-writeback",
		GroupID:         "shock-absorber-writeback",
		BatchSize:       100,
		BatchTimeout:    10 * time.Millisecond,
		WriteTimeout:    10 * time.Second,
		ReadTimeout:     10 * time.Second,
		RequiredAcks:    -1,      // All replicas
		MaxMessageBytes: 1000000, // 1MB
		MinBytes:        1,
		MaxBytes:        10 * 1024 * 1024, // 10MB
		MaxWait:         100 * time.Millisecond,
	}

	var err error
	client, err = shockabsorber.NewClient(config)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	// Initialize KV store and database manually for now
	// TODO: This should be done in client initialization
	if err := initializeConnections(config); err != nil {
		log.Fatalf("Failed to initialize connections: %v", err)
	}

	// Enable KV for payment table
	ctx := context.Background()
	log.Println("Enabling KV for 'payment' table...")
	tableInstance, err = client.EnableKV(ctx, "payment")
	if err != nil {
		log.Fatalf("Failed to enable KV for table: %v", err)
	}
	log.Println("KV enabled for 'payment' table successfully")

	// Get internal table for drainer access (must be done before starting drainer)
	internalTable, err = getInternalTable("payment")
	if err != nil {
		log.Fatalf("Failed to get internal table for drainer: %v", err)
	}
	log.Printf("Internal table retrieved for drainer: %T", internalTable)

	// Verify queue is accessible
	writeQueue := internalTable.GetWriteBackQueue()
	if writeQueue != nil {
		log.Printf("Write-back queue verified: %T, initial size: %d", writeQueue, writeQueue.Size())
	} else {
		log.Fatalf("Write-back queue is nil - drainer will not work")
	}

	// Start write-back drainer (after internal table is set)
	go startDrainer(ctx)

	// Setup HTTP routes
	http.HandleFunc("/health", healthHandler)
	http.HandleFunc("/payment", paymentHandler)
	http.HandleFunc("/payment/reference/", getPaymentByReferenceHandler)

	log.Println("API Endpoints:")
	log.Println("  POST   /payment - Create a new payment")
	log.Println("  GET    /payment/reference/{ref_id} - Get payment by reference ID")
	log.Println("  GET    /health - Health check")

	// Start server
	port := ":8080"
	log.Printf("Starting test server on port %s", port)

	// Graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		if err := http.ListenAndServe(port, nil); err != nil {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	<-sigChan
	log.Println("Shutting down...")
}

func initializeConnections(config *shockabsorber.Config) error {
	// Connections are now initialized automatically by the client
	// This function is kept for future extensibility
	log.Println("Client will initialize connections automatically")
	return nil
}

func startDrainer(ctx context.Context) {
	log.Println("[DRAINER] Starting write-back drainer...")

	// Wait a bit for the table to be fully initialized
	time.Sleep(500 * time.Millisecond)

	// Use the global internalTable variable
	if internalTable == nil {
		log.Printf("[DRAINER] ERROR: Internal table is nil - drainer cannot start")
		log.Printf("[DRAINER] This means the table was not properly initialized.")
		return
	}

	log.Printf("[DRAINER] Using internal table: %T", internalTable)

	writeQueue := internalTable.GetWriteBackQueue()
	if writeQueue == nil {
		log.Printf("[DRAINER] ERROR: Write-back queue is nil")
		log.Printf("[DRAINER] This means the drainer will not work. Please check table initialization.")
		return
	}

	log.Printf("[DRAINER] Successfully retrieved write-back queue: %T", writeQueue)
	log.Printf("[DRAINER] Initial queue size: %d", writeQueue.Size())

	// Log queue type for clarity
	queueType := "unknown"
	switch writeQueue.(type) {
	case *writeback.MemoryQueue:
		queueType = "Memory Queue"
	case *writeback.RedisQueue:
		queueType = "Redis Queue"
	case *writeback.KafkaQueue:
		queueType = "Kafka Queue"
	}
	log.Printf("[DRAINER] Queue Type: %s", queueType)

	if queueType == "Kafka Queue" {
		log.Printf("[DRAINER] ✓ Kafka consumer is ready to consume write-back operations")
		log.Printf("[DRAINER] Operations will be consumed from Kafka and written to MySQL")
	}

	// Get drain rate from config (default: 1 RPS for DB writes in performance test)
	// This ensures Redis/Kafka can absorb 20 TPS while DB writes are throttled to 1 RPS
	drainRate := 1 // operations per second (1 RPS for performance test)
	log.Printf("[DRAINER] Drain rate configured: %d operations/second (DB write limit)", drainRate)
	log.Printf("[DRAINER] Redis/Kafka will absorb all realtime traffic (e.g., 20 TPS)")
	log.Printf("[DRAINER] Database writes will be throttled to %d RPS", drainRate)

	// Create rate limiter for DB writes (1 RPS = 1 operation per second = 1 operation per 1000ms)
	// This allows Redis/Kafka to handle high traffic while protecting the DB
	limiter := rate.NewLimiter(rate.Limit(drainRate), 1) // 1 ops/sec, burst of 1

	log.Printf("[DRAINER] [%s] Write-back drainer started successfully", time.Now().Format("2006-01-02 15:04:05.000"))

	// Drain operations continuously (not on a fixed ticker)
	// The rate limiter will control the throughput
	operationCount := 0
	startTime := time.Now()

	for {
		select {
		case <-ctx.Done():
			log.Printf("[DRAINER] [%s] Stopping write-back drainer...", time.Now().Format("2006-01-02 15:04:05.000"))
			return
		default:
			// Check queue size
			queueSize := writeQueue.Size()
			if queueSize == 0 {
				// No operations, wait a bit before checking again
				time.Sleep(100 * time.Millisecond)
				continue
			}

			// Wait for rate limiter before processing (enforces 1 RPS limit)
			waitStart := time.Now()
			if err := limiter.Wait(ctx); err != nil {
				if err == context.Canceled || err == context.DeadlineExceeded {
					return
				}
				log.Printf("[DRAINER] [%s] ERROR: Rate limiter error: %v",
					time.Now().Format("2006-01-02 15:04:05.000"), err)
				continue
			}
			waitDuration := time.Since(waitStart)

			// Dequeue a single operation (rate limiter controls throughput)
			// We process one at a time to respect the rate limit precisely
			log.Printf("[DRAINER] [%s] Rate limiter allowed operation (waited: %v, queue size: %d)",
				time.Now().Format("2006-01-02 15:04:05.000"), waitDuration, queueSize)

			operations, err := writeQueue.Dequeue(ctx, 1)
			if err != nil {
				log.Printf("[DRAINER] [%s] ERROR: Failed to dequeue operations: %v",
					time.Now().Format("2006-01-02 15:04:05.000"), err)
				continue
			}

			if len(operations) == 0 {
				continue
			}

			// Process the operation
			op := operations[0]
			if op == nil {
				continue
			}

			operationCount++
			elapsed := time.Since(startTime)

			log.Printf("[DRAINER] [%s] === PROCESSING OPERATION #%d ===",
				time.Now().Format("2006-01-02 15:04:05.000"), operationCount)
			log.Printf("[DRAINER] [%s] Operation: %s, Table: %s, Key: %v",
				time.Now().Format("2006-01-02 15:04:05.000"), op.Operation, op.Table, op.Key)
			log.Printf("[DRAINER] [%s] Queue size: %d, Elapsed time: %v, Operations processed: %d",
				time.Now().Format("2006-01-02 15:04:05.000"), writeQueue.Size(), elapsed, operationCount)

			// Execute the write operation directly to the database
			dbWriteStart := time.Now()
			log.Printf("[DRAINER] [%s] Writing to MySQL database...",
				time.Now().Format("2006-01-02 15:04:05.000"))

			if err := internalTable.ExecuteWriteOperation(ctx, op); err != nil {
				dbWriteDuration := time.Since(dbWriteStart)
				log.Printf("[DRAINER] [%s] ERROR: Failed to write to MySQL - Operation: %s, Table: %s, Key: %v, Error: %v, Duration: %v",
					time.Now().Format("2006-01-02 15:04:05.000"), op.Operation, op.Table, op.Key, err, dbWriteDuration)
				// In production, you might want to re-queue failed operations
				// For now, we'll just log the error
				continue
			}

			dbWriteDuration := time.Since(dbWriteStart)
			log.Printf("[DRAINER] [%s] ✓ Successfully written to MySQL - Operation: %s, Table: %s, Key: %v, DB Write Duration: %v",
				time.Now().Format("2006-01-02 15:04:05.000"), op.Operation, op.Table, op.Key, dbWriteDuration)
			log.Printf("[DRAINER] [%s] === OPERATION #%d COMPLETE === (Queue size: %d, Total processed: %d, Elapsed: %v)",
				time.Now().Format("2006-01-02 15:04:05.000"), operationCount, writeQueue.Size(), operationCount, time.Since(startTime))
		}
	}
}

// getInternalTable retrieves the internal core.Table instance.
// This is a helper function to access internal implementation details for the drainer.
func getInternalTable(tableName string) (core.Table, error) {
	// Get the wrapped table
	table, err := client.GetTable(tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to get table: %w", err)
	}

	// Use reflection to call GetInternalTable method
	tableValue := reflect.ValueOf(table)
	method := tableValue.MethodByName("GetInternalTable")
	if !method.IsValid() {
		return nil, fmt.Errorf("table does not have GetInternalTable method (type: %T)", table)
	}

	// Call the method
	results := method.Call(nil)
	if len(results) == 0 {
		return nil, fmt.Errorf("GetInternalTable returned no results")
	}

	// Convert to core.Table
	internalTable, ok := results[0].Interface().(core.Table)
	if !ok {
		return nil, fmt.Errorf("GetInternalTable returned wrong type: %T, expected core.Table", results[0].Interface())
	}

	return internalTable, nil
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func paymentHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		createPayment(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func createPayment(w http.ResponseWriter, r *http.Request) {
	var payment map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&payment); err != nil {
		http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	// Validate required fields
	if _, ok := payment["upi_reference_id"]; !ok {
		http.Error(w, "upi_reference_id is required", http.StatusBadRequest)
		return
	}

	refID := payment["upi_reference_id"].(string)
	log.Printf("=== CREATE PAYMENT REQUEST ===")
	log.Printf("Reference ID: %s", refID)

	ctx := context.Background()

	// Check if payment exists by reference_id
	log.Printf("Step 1: Checking if payment exists with reference_id: %s", refID)
	existingPayment, err := findPaymentByReferenceID(ctx, refID)
	if err == nil && existingPayment != nil {
		log.Printf("Step 1 Result: Payment already exists with reference_id: %s", refID)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"message": "Payment already exists",
			"payment": existingPayment,
		})
		return
	}
	log.Printf("Step 1 Result: Payment not found, proceeding with creation")

	// Generate ID if not provided
	if _, ok := payment["id"]; !ok {
		payment["id"] = fmt.Sprintf("pay_%d", time.Now().UnixNano())
		log.Printf("Step 2: Generated payment ID: %s", payment["id"])
	}

	// Create payment (this will write to Redis first, then queue for DB)
	log.Printf("Step 3: Creating payment in Redis (write-ahead log)")
	if err := tableInstance.Create(ctx, payment); err != nil {
		log.Printf("Step 3 Error: Failed to create payment: %v", err)
		http.Error(w, fmt.Sprintf("Failed to create payment: %v", err), http.StatusInternalServerError)
		return
	}

	timestamp := time.Now().Format("2006-01-02 15:04:05.000")
	log.Printf("[%s] Step 3 Success: Payment written to Redis and queued for database write-back", timestamp)
	log.Printf("[%s] Step 4: Payment will be written to database asynchronously via write-back queue", timestamp)
	log.Printf("[%s] === CREATE PAYMENT COMPLETE ===", timestamp)

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "Payment created successfully (in Redis, queued for DB)",
		"payment": payment,
	})
}

func getPaymentByReferenceHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract reference_id from path
	refID := r.URL.Path[len("/payment/reference/"):]
	if refID == "" {
		http.Error(w, "Reference ID required", http.StatusBadRequest)
		return
	}

	log.Printf("=== GET PAYMENT BY REFERENCE ===")
	log.Printf("Reference ID: %s", refID)

	ctx := context.Background()
	payment, err := findPaymentByReferenceID(ctx, refID)
	if err != nil {
		log.Printf("Error finding payment: %v", err)
		http.Error(w, fmt.Sprintf("Failed to find payment: %v", err), http.StatusNotFound)
		return
	}

	log.Printf("Payment found successfully")
	json.NewEncoder(w).Encode(payment)
}

// findPaymentByReferenceID searches for a payment by upi_reference_id
// It queries the database to find the payment ID, then reads from table (which checks Redis first)
func findPaymentByReferenceID(ctx context.Context, refID string) (map[string]interface{}, error) {
	log.Printf("[API] Searching for payment with reference_id: %s", refID)

	// Use the table's FindByField method to query by reference_id
	payment, err := tableInstance.FindByField(ctx, "upi_reference_id", refID)
	if err != nil {
		log.Printf("[API] Payment not found with reference_id: %s, error: %v", refID, err)
		return nil, err
	}

	log.Printf("[API] Payment found successfully with reference_id: %s", refID)
	return payment, nil
}
