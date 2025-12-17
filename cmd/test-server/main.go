package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rzpsarthak13/shock-absorber/pkg/shockabsorber"
)

var (
	client       shockabsorber.Client
	paymentTable shockabsorber.Table
)

func main() {
	// 1. Configure the shock absorber
	config := shockabsorber.DefaultConfig()

	// KV Store configuration
	// Option 1: Redis (commented out - using DynamoDB)
	// config.KVStore.Type = "redis" // Explicitly set Redis as the KV store type
	// config.KVStore.RedisConfig.Endpoints = []string{"localhost:6379"}

	// Option 2: DynamoDB (active - using LocalStack for local testing)
	config.KVStore.Type = "dynamodb"
	config.KVStore.DynamoDBConfig = shockabsorber.DynamoDBConfig{
		Region:         "us-east-1",
		TableName:      "shock-absorber-cache",
		Endpoint:       "http://localhost:4566", // LocalStack endpoint
		AccessKeyID:     "test",                  // Dummy credentials for LocalStack
		SecretAccessKey: "test",
	}

	// For AWS DynamoDB (production - uncomment to use real AWS):
	// config.KVStore.Type = "dynamodb"
	// config.KVStore.DynamoDBConfig = shockabsorber.DynamoDBConfig{
	// 	Region:    "us-east-1",                    // Your AWS region
	// 	TableName: "shock-absorber-cache",         // Your table name
	// 	Endpoint:  "",                             // Empty for real AWS
	// 	// AccessKeyID and SecretAccessKey are optional if using default AWS credentials
	// }

	// Database (MySQL) configuration
	config.Database.Host = "localhost"
	config.Database.Port = 3306
	config.Database.Database = "testdb"
	config.Database.Username = "root"
	config.Database.Password = "password"

	// Write-back configuration
	// Note: Using "memory" queue since DynamoDB doesn't support list operations needed for Redis queue
	// If using Redis KV store, you can use "redis" queue type
	config.WriteBack.QueueType = "memory" // Options: "memory", "redis", "kafka"
	config.WriteBack.DefaultTTL = 1 * time.Hour
	config.WriteBack.DrainRate = 5 // 5 DB writes per second (adjust based on your DB capacity)

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

	// 2. Create the client
	var err error
	client, err = shockabsorber.NewClient(config)
	if err != nil {
		log.Fatalf("Failed to create shock absorber client: %v", err)
	}
	defer client.Close()

	// 3. Enable KV for tables
	ctx := context.Background()
	log.Println("Enabling KV for 'payment' table...")

	paymentTable, err = client.EnableKV(ctx, "payment",
		shockabsorber.WithDrainRate(5), // Override drain rate for this table if needed
	)
	if err != nil {
		log.Fatalf("Failed to enable KV for 'payment' table: %v", err)
	}
	log.Println("✓ KV enabled for 'payment' table")

	// 4. Start the background drainer workers
	log.Println("Starting background drainer workers...")
	if err := client.Start(ctx); err != nil {
		log.Fatalf("Failed to start drainer workers: %v", err)
	}
	log.Println("✓ Drainer workers started")
	defer func() {
		log.Println("Stopping drainer workers...")
		client.Stop()
		log.Println("✓ Drainer workers stopped")
	}()

	// 5. Setup HTTP routes
	http.HandleFunc("/health", healthHandler)
	http.HandleFunc("/payment", paymentHandler)
	http.HandleFunc("/payment/reference/", getPaymentByReferenceHandler)

	// Print API info
	log.Println("")
	log.Println("╔════════════════════════════════════════════════════════════╗")
	log.Println("║           SHOCK ABSORBER TEST SERVER                       ║")
	log.Println("╠════════════════════════════════════════════════════════════╣")
	log.Println("║  API Endpoints:                                            ║")
	log.Println("║    POST   /payment                - Create a new payment   ║")
	log.Println("║    GET    /payment/reference/{id} - Get payment by ref ID  ║")
	log.Println("║    GET    /health                 - Health check           ║")
	log.Println("╠════════════════════════════════════════════════════════════╣")
	log.Printf("║  Queue Type: %-46s ║\n", config.WriteBack.QueueType)
	log.Printf("║  Drain Rate: %-3d ops/sec                                   ║\n", config.WriteBack.DrainRate)
	log.Println("╚════════════════════════════════════════════════════════════╝")
	log.Println("")

	// 6. Start HTTP server
	port := ":8080"
	log.Printf("Starting HTTP server on port %s", port)

	// Graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		if err := http.ListenAndServe(port, nil); err != nil {
			log.Fatalf("HTTP server failed: %v", err)
		}
	}()

	<-sigChan
	log.Println("\nReceived shutdown signal...")
}

// ============================================================================
// HTTP Handlers
// ============================================================================

func healthHandler(w http.ResponseWriter, r *http.Request) {
	status := map[string]interface{}{
		"status":    "ok",
		"timestamp": time.Now().Format(time.RFC3339),
		"drainer": map[string]interface{}{
			"running": client.IsRunning(),
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(status)
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
	requestStart := time.Now()

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
	log.Printf("[%s] ▶ REQUEST START | ref_id=%s",
		requestStart.Format("15:04:05.000"), refID)

	ctx := context.Background()

	// Check if payment exists by reference_id
	existingPayment, err := paymentTable.FindByField(ctx, "upi_reference_id", refID)
	if err == nil && existingPayment != nil {
		log.Printf("[%s] ◀ REQUEST END (duplicate) | ref_id=%s | duration=%v",
			time.Now().Format("15:04:05.000"), refID, time.Since(requestStart))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"message": "Payment already exists",
			"payment": existingPayment,
		})
		return
	}

	// Generate ID if not provided
	if _, ok := payment["id"]; !ok {
		payment["id"] = fmt.Sprintf("pay_%d", time.Now().UnixNano())
	}

	// Create payment (writes to Redis first, then queued for DB)
	redisWriteStart := time.Now()
	if err := paymentTable.Create(ctx, payment); err != nil {
		log.Printf("[%s] ✗ REQUEST FAILED | ref_id=%s | error=%v | duration=%v",
			time.Now().Format("15:04:05.000"), refID, err, time.Since(requestStart))
		http.Error(w, fmt.Sprintf("Failed to create payment: %v", err), http.StatusInternalServerError)
		return
	}
	redisWriteDuration := time.Since(redisWriteStart)

	totalDuration := time.Since(requestStart)
	log.Printf("[%s] ◀ REQUEST END (success) | ref_id=%s | redis_write=%v | total=%v",
		time.Now().Format("15:04:05.000"), refID, redisWriteDuration, totalDuration)

	w.Header().Set("Content-Type", "application/json")
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
	payment, err := paymentTable.FindByField(ctx, "upi_reference_id", refID)
	if err != nil {
		log.Printf("Error finding payment: %v", err)
		http.Error(w, fmt.Sprintf("Payment not found: %v", err), http.StatusNotFound)
		return
	}

	log.Printf("Payment found successfully")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(payment)
}
