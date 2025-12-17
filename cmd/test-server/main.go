package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/rzpsarthak13/shock-absorber/pkg/shockabsorber"
)

var (
	client       shockabsorber.Client
	paymentTable shockabsorber.Table
	metrics      *LoadTestMetrics
	db           *sql.DB
	queueType    string // "redis", "kafka", or "memory"
)

// LoadTestMetrics tracks real-time metrics for the load test
type LoadTestMetrics struct {
	mu sync.RWMutex

	// Test state
	TestRunning bool      `json:"test_running"`
	TestStarted time.Time `json:"test_started,omitempty"`

	// Request metrics
	TotalRequestsReceived int64     `json:"total_requests_received"`
	FirstRequestTime      time.Time `json:"first_request_time,omitempty"`
	LastRequestTime       time.Time `json:"last_request_time,omitempty"`

	// DB write metrics
	TotalDBWrites int64 `json:"total_db_writes"`

	// Drainer metrics
	DrainerStartTime time.Time `json:"drainer_start_time,omitempty"`
	DrainerRunning   bool      `json:"drainer_running"`

	// Peak queue size
	PeakQueueSize int `json:"peak_queue_size"`
}

func NewLoadTestMetrics() *LoadTestMetrics {
	return &LoadTestMetrics{}
}

func (m *LoadTestMetrics) RecordRequest() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	atomic.AddInt64(&m.TotalRequestsReceived, 1)

	if m.FirstRequestTime.IsZero() {
		m.FirstRequestTime = now
		m.TestStarted = now
		m.TestRunning = true
	}
	m.LastRequestTime = now
}

func (m *LoadTestMetrics) UpdatePeakQueueSize(currentSize int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if currentSize > m.PeakQueueSize {
		m.PeakQueueSize = currentSize
	}
}

func (m *LoadTestMetrics) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.TestRunning = false
	m.TestStarted = time.Time{}
	m.TotalRequestsReceived = 0
	m.FirstRequestTime = time.Time{}
	m.LastRequestTime = time.Time{}
	m.TotalDBWrites = 0
	m.DrainerStartTime = time.Time{}
	m.PeakQueueSize = 0
}

func (m *LoadTestMetrics) GetSnapshot() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	snapshot := map[string]interface{}{
		"test_running":            m.TestRunning,
		"total_requests_received": m.TotalRequestsReceived,
		"total_db_writes":         m.TotalDBWrites,
		"peak_queue_size":         m.PeakQueueSize,
		"drainer_running":         m.DrainerRunning,
	}

	// Calculate durations
	if !m.FirstRequestTime.IsZero() {
		snapshot["first_request_time"] = m.FirstRequestTime.Format("15:04:05.000")

		if !m.LastRequestTime.IsZero() {
			snapshot["last_request_time"] = m.LastRequestTime.Format("15:04:05.000")
		}
	}

	// Ingestion duration (first request to last request)
	if !m.FirstRequestTime.IsZero() && !m.LastRequestTime.IsZero() {
		snapshot["ingestion_duration_seconds"] = m.LastRequestTime.Sub(m.FirstRequestTime).Seconds()
	}

	// Note: total_elapsed_seconds will be calculated in metricsHandler
	// using drainer stats for accurate first request → last DB write timing

	return snapshot
}

func main() {
	// Initialize metrics
	metrics = NewLoadTestMetrics()

	// 1. Configure the shock absorber
	config := shockabsorber.DefaultConfig()

	// KV Store (Redis) configuration
	config.KVStore.Endpoints = []string{"localhost:6379"}

	// Database (MySQL) configuration
	config.Database.Host = "localhost"
	config.Database.Port = 3306
	config.Database.Database = "testdb"
	config.Database.Username = "root"
	config.Database.Password = "password"

	// Write-back configuration
	config.WriteBack.QueueType = "redis" // Options: "memory", "redis", "kafka"
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

	// Store queue type for metrics
	queueType = config.WriteBack.QueueType

	// 2. Create the client
	var err error
	client, err = shockabsorber.NewClient(config)
	if err != nil {
		log.Fatalf("Failed to create shock absorber client: %v", err)
	}
	defer client.Close()

	// 3. Connect to MySQL directly for record count queries
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s",
		config.Database.Username,
		config.Database.Password,
		config.Database.Host,
		config.Database.Port,
		config.Database.Database,
	)
	db, err = sql.Open("mysql", dsn)
	if err != nil {
		log.Printf("Warning: Could not connect to MySQL for metrics: %v", err)
	}
	defer func() {
		if db != nil {
			db.Close()
		}
	}()

	// 4. Enable KV for tables
	ctx := context.Background()
	log.Println("Enabling KV for 'payment' table...")

	paymentTable, err = client.EnableKV(ctx, "payment",
		shockabsorber.WithDrainRate(5), // Override drain rate for this table if needed
	)
	if err != nil {
		log.Fatalf("Failed to enable KV for 'payment' table: %v", err)
	}
	log.Println("✓ KV enabled for 'payment' table")

	// 5. Start the background drainer workers
	log.Println("Starting background drainer workers...")
	if err := client.Start(ctx); err != nil {
		log.Fatalf("Failed to start drainer workers: %v", err)
	}
	log.Println("✓ Drainer workers started")
	metrics.DrainerRunning = true
	metrics.DrainerStartTime = time.Now()

	defer func() {
		log.Println("Stopping drainer workers...")
		client.Stop()
		metrics.DrainerRunning = false
		log.Println("✓ Drainer workers stopped")
	}()

	// 6. Setup HTTP routes
	http.HandleFunc("/health", healthHandler)
	http.HandleFunc("/payment", paymentHandler)
	http.HandleFunc("/payment/reference/", getPaymentByReferenceHandler)
	http.HandleFunc("/metrics", metricsHandler)
	http.HandleFunc("/metrics/reset", metricsResetHandler)
	http.HandleFunc("/dashboard", dashboardHandler)

	// Print API info
	log.Println("")
	log.Println("╔════════════════════════════════════════════════════════════╗")
	log.Println("║           SHOCK ABSORBER TEST SERVER                       ║")
	log.Println("╠════════════════════════════════════════════════════════════╣")
	log.Println("║  API Endpoints:                                            ║")
	log.Println("║    POST   /payment                - Create a new payment   ║")
	log.Println("║    GET    /payment/reference/{id} - Get payment by ref ID  ║")
	log.Println("║    GET    /health                 - Health check           ║")
	log.Println("║    GET    /metrics                - Real-time metrics      ║")
	log.Println("║    POST   /metrics/reset          - Reset metrics          ║")
	log.Println("║    GET    /dashboard              - Live Dashboard UI      ║")
	log.Println("╠════════════════════════════════════════════════════════════╣")
	log.Printf("║  Queue Type: %-46s ║\n", config.WriteBack.QueueType)
	log.Printf("║  Drain Rate: %-3d ops/sec                                   ║\n", config.WriteBack.DrainRate)
	log.Println("╚════════════════════════════════════════════════════════════╝")
	log.Println("")

	// 7. Start HTTP server
	port := ":8080"
	log.Printf("Starting HTTP server on port %s", port)
	log.Printf("📊 Dashboard available at: http://localhost%s/dashboard", port)

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

	// Record the request in metrics
	metrics.RecordRequest()

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

// metricsHandler returns real-time metrics as JSON
func metricsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get queue size from drainer
	drainer := client.GetDrainer("payment")
	queueSize := 0
	if drainer != nil {
		queueSize = drainer.QueueSize()
		metrics.UpdatePeakQueueSize(queueSize)
	}

	// Get DB record count
	dbRecordCount := int64(0)
	if db != nil {
		err := db.QueryRow("SELECT COUNT(*) FROM payment WHERE upi_reference_id LIKE 'perf_test_%'").Scan(&dbRecordCount)
		if err != nil {
			log.Printf("Error querying DB record count: %v", err)
		}
	}

	// Update DB writes count from actual DB
	metrics.mu.Lock()
	metrics.TotalDBWrites = dbRecordCount
	metrics.mu.Unlock()

	// Build response
	snapshot := metrics.GetSnapshot()
	snapshot["current_queue_size"] = queueSize
	snapshot["db_record_count"] = dbRecordCount
	snapshot["drainer_running"] = client.IsRunning()
	snapshot["queue_type"] = queueType

	// Get drainer stats
	if drainer != nil {
		snapshot["configured_drain_rate"] = drainer.GetConfig().DrainRate

		// Add drainer statistics (accurate DB write timing)
		stats := drainer.GetStats()
		snapshot["drainer_operation_count"] = stats.OperationCount
		if !stats.FirstWriteTime.IsZero() {
			snapshot["first_db_write_time"] = stats.FirstWriteTime.Format("15:04:05.000")
		}
		if !stats.LastWriteTime.IsZero() {
			snapshot["last_db_write_time"] = stats.LastWriteTime.Format("15:04:05.000")
		}
		if !stats.FirstWriteTime.IsZero() && !stats.LastWriteTime.IsZero() {
			snapshot["db_write_duration_seconds"] = stats.LastWriteTime.Sub(stats.FirstWriteTime).Seconds()
		}
		if stats.OperationCount > 0 {
			avgWriteTimeMs := float64(stats.TotalWriteTimeNs) / float64(stats.OperationCount) / 1e6
			snapshot["avg_db_write_time_ms"] = avgWriteTimeMs
		}

		// Calculate total elapsed time: first request → last DB write
		// Only show final value when queue is empty (test complete)
		metrics.mu.RLock()
		firstRequestTime := metrics.FirstRequestTime
		metrics.mu.RUnlock()

		if !firstRequestTime.IsZero() && !stats.LastWriteTime.IsZero() {
			// Test is complete or in progress - show time from first request to last DB write
			snapshot["total_elapsed_seconds"] = stats.LastWriteTime.Sub(firstRequestTime).Seconds()
		} else if !firstRequestTime.IsZero() && queueSize > 0 {
			// Test in progress, queue not empty - show running time
			snapshot["total_elapsed_seconds"] = time.Since(firstRequestTime).Seconds()
		} else if !firstRequestTime.IsZero() {
			// Requests received but no DB writes yet
			snapshot["total_elapsed_seconds"] = time.Since(firstRequestTime).Seconds()
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(snapshot)
}

// metricsResetHandler resets the metrics
func metricsResetHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	metrics.Reset()

	// Also reset drainer stats
	drainer := client.GetDrainer("payment")
	if drainer != nil {
		drainer.ResetStats()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "metrics reset"})
}

// dashboardHandler serves the HTML dashboard
func dashboardHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(dashboardHTML))
}

const dashboardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Shock Absorber Dashboard</title>
    <link href="https://fonts.googleapis.com/css2?family=JetBrains+Mono:wght@400;600;700&family=Space+Grotesk:wght@400;500;600;700&display=swap" rel="stylesheet">
    <style>
        :root {
            --bg-primary: #0a0a0f;
            --bg-secondary: #12121a;
            --bg-card: #1a1a24;
            --border-color: #2a2a3a;
            --text-primary: #e4e4e7;
            --text-secondary: #a1a1aa;
            --accent-cyan: #22d3ee;
            --accent-green: #4ade80;
            --accent-yellow: #facc15;
            --accent-red: #f87171;
            --accent-purple: #a78bfa;
            --accent-blue: #60a5fa;
        }
        
        * {
            margin: 0;
            padding: 0;
            box-sizing: border-box;
        }
        
        body {
            font-family: 'Space Grotesk', sans-serif;
            background: var(--bg-primary);
            color: var(--text-primary);
            min-height: 100vh;
            background-image: 
                radial-gradient(ellipse at top, rgba(34, 211, 238, 0.1) 0%, transparent 50%),
                radial-gradient(ellipse at bottom right, rgba(167, 139, 250, 0.1) 0%, transparent 50%);
        }
        
        .container {
            max-width: 1400px;
            margin: 0 auto;
            padding: 2rem;
        }
        
        header {
            text-align: center;
            margin-bottom: 3rem;
        }
        
        h1 {
            font-size: 2.5rem;
            font-weight: 700;
            background: linear-gradient(135deg, var(--accent-cyan), var(--accent-purple));
            -webkit-background-clip: text;
            -webkit-text-fill-color: transparent;
            background-clip: text;
            margin-bottom: 0.5rem;
        }
        
        .subtitle {
            color: var(--text-secondary);
            font-size: 1.1rem;
        }
        
        .status-bar {
            display: flex;
            justify-content: center;
            gap: 2rem;
            margin-bottom: 2rem;
            flex-wrap: wrap;
        }
        
        .status-item {
            display: flex;
            align-items: center;
            gap: 0.5rem;
            font-family: 'JetBrains Mono', monospace;
            font-size: 0.9rem;
        }
        
        .status-dot {
            width: 10px;
            height: 10px;
            border-radius: 50%;
            animation: pulse 2s infinite;
        }
        
        .status-dot.active { background: var(--accent-green); }
        .status-dot.inactive { background: var(--accent-red); animation: none; }
        
        @keyframes pulse {
            0%, 100% { opacity: 1; }
            50% { opacity: 0.5; }
        }
        
        .metrics-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
            gap: 1.5rem;
            margin-bottom: 2rem;
        }
        
        .metric-card {
            background: var(--bg-card);
            border: 1px solid var(--border-color);
            border-radius: 16px;
            padding: 1.5rem;
            position: relative;
            overflow: hidden;
            transition: transform 0.2s, border-color 0.2s;
        }
        
        .metric-card:hover {
            transform: translateY(-2px);
            border-color: var(--accent-cyan);
        }
        
        .metric-card::before {
            content: '';
            position: absolute;
            top: 0;
            left: 0;
            right: 0;
            height: 3px;
            background: linear-gradient(90deg, var(--accent-cyan), var(--accent-purple));
        }
        
        .metric-label {
            font-size: 0.85rem;
            color: var(--text-secondary);
            text-transform: uppercase;
            letter-spacing: 0.05em;
            margin-bottom: 0.75rem;
        }
        
        .metric-value {
            font-family: 'JetBrains Mono', monospace;
            font-size: 2.5rem;
            font-weight: 700;
            color: var(--text-primary);
            line-height: 1;
        }
        
        .metric-value.cyan { color: var(--accent-cyan); }
        .metric-value.green { color: var(--accent-green); }
        .metric-value.yellow { color: var(--accent-yellow); }
        .metric-value.purple { color: var(--accent-purple); }
        .metric-value.blue { color: var(--accent-blue); }
        
        .metric-unit {
            font-size: 1rem;
            color: var(--text-secondary);
            margin-left: 0.25rem;
        }
        
        .metric-detail {
            font-size: 0.8rem;
            color: var(--text-secondary);
            margin-top: 0.5rem;
            font-family: 'JetBrains Mono', monospace;
        }
        
        .timeline-section {
            background: var(--bg-card);
            border: 1px solid var(--border-color);
            border-radius: 16px;
            padding: 2rem;
            margin-bottom: 2rem;
        }
        
        .section-title {
            font-size: 1.25rem;
            font-weight: 600;
            margin-bottom: 1.5rem;
            display: flex;
            align-items: center;
            gap: 0.5rem;
        }
        
        .timeline {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
            gap: 1rem;
        }
        
        .timeline-item {
            background: var(--bg-secondary);
            border-radius: 8px;
            padding: 1rem;
        }
        
        .timeline-label {
            font-size: 0.75rem;
            color: var(--text-secondary);
            text-transform: uppercase;
            letter-spacing: 0.05em;
            margin-bottom: 0.5rem;
        }
        
        .timeline-value {
            font-family: 'JetBrains Mono', monospace;
            font-size: 1.1rem;
            color: var(--accent-cyan);
        }
        
        .controls {
            display: flex;
            justify-content: center;
            gap: 1rem;
            margin-bottom: 2rem;
        }
        
        button {
            font-family: 'Space Grotesk', sans-serif;
            font-size: 0.9rem;
            font-weight: 600;
            padding: 0.75rem 1.5rem;
            border-radius: 8px;
            border: none;
            cursor: pointer;
            transition: all 0.2s;
        }
        
        .btn-primary {
            background: linear-gradient(135deg, var(--accent-cyan), var(--accent-blue));
            color: var(--bg-primary);
        }
        
        .btn-primary:hover {
            transform: translateY(-2px);
            box-shadow: 0 4px 20px rgba(34, 211, 238, 0.3);
        }
        
        .btn-secondary {
            background: var(--bg-secondary);
            color: var(--text-primary);
            border: 1px solid var(--border-color);
        }
        
        .btn-secondary:hover {
            border-color: var(--accent-cyan);
        }
        
        .queue-visualization {
            background: var(--bg-secondary);
            border-radius: 8px;
            padding: 1rem;
            margin-top: 1rem;
        }
        
        .queue-bar {
            height: 24px;
            background: var(--bg-primary);
            border-radius: 4px;
            overflow: hidden;
            position: relative;
        }
        
        .queue-fill {
            height: 100%;
            background: linear-gradient(90deg, var(--accent-cyan), var(--accent-purple));
            transition: width 0.3s ease;
            border-radius: 4px;
        }
        
        .queue-labels {
            display: flex;
            justify-content: space-between;
            margin-top: 0.5rem;
            font-size: 0.75rem;
            color: var(--text-secondary);
            font-family: 'JetBrains Mono', monospace;
        }
        
        .last-update {
            text-align: center;
            font-size: 0.8rem;
            color: var(--text-secondary);
            margin-top: 2rem;
            font-family: 'JetBrains Mono', monospace;
        }
        
        @media (max-width: 768px) {
            .container { padding: 1rem; }
            h1 { font-size: 1.75rem; }
            .metric-value { font-size: 2rem; }
        }
    </style>
</head>
<body>
    <div class="container">
        <header>
            <h1>⚡ Shock Absorber Dashboard</h1>
            <p class="subtitle">Real-time Load Test Monitoring</p>
        </header>
        
        <div class="status-bar">
            <div class="status-item">
                <span class="status-dot" id="drainerStatus"></span>
                <span>Drainer: <span id="drainerText">-</span></span>
            </div>
            <div class="status-item">
                <span class="status-dot" id="testStatus"></span>
                <span>Test: <span id="testText">-</span></span>
            </div>
            <div class="status-item">
                <span style="color: var(--accent-purple);">📦</span>
                <span>Queue: <span id="queueTypeText" style="color: var(--accent-cyan); font-weight: 600;">-</span></span>
            </div>
        </div>
        
        <div class="controls">
            <button class="btn-primary" onclick="resetMetrics()">🔄 Reset Metrics</button>
            <button class="btn-secondary" onclick="toggleAutoRefresh()">
                <span id="refreshBtn">⏸ Pause</span>
            </button>
        </div>
        
        <div class="metrics-grid">
            <div class="metric-card">
                <div class="metric-label">📥 Queue Size</div>
                <div class="metric-value cyan" id="queueSize">0</div>
                <div class="metric-detail">Peak: <span id="peakQueue">0</span></div>
                <div class="queue-visualization">
                    <div class="queue-bar">
                        <div class="queue-fill" id="queueFill" style="width: 0%"></div>
                    </div>
                    <div class="queue-labels">
                        <span>0</span>
                        <span id="maxQueueLabel">100</span>
                    </div>
                </div>
            </div>
            
            <div class="metric-card">
                <div class="metric-label">📝 Redis Records Written</div>
                <div class="metric-value green" id="requestsReceived">0</div>
                <div class="metric-detail">Written to cache/queue</div>
            </div>
            
            <div class="metric-card">
                <div class="metric-label">💾 DB Records Written</div>
                <div class="metric-value purple" id="dbWrites">0</div>
                <div class="metric-detail">Persisted to MySQL</div>
            </div>
            
            <div class="metric-card">
                <div class="metric-label">⏱️ Total Elapsed Time</div>
                <div class="metric-value yellow" id="totalElapsed">0.0<span class="metric-unit">s</span></div>
                <div class="metric-detail">First request → Last DB write</div>
            </div>
            
            <div class="metric-card">
                <div class="metric-label">🔄 DB Write Duration</div>
                <div class="metric-value blue" id="dbDuration">0.0<span class="metric-unit">s</span></div>
                <div class="metric-detail">First → Last DB write</div>
            </div>
            
            <div class="metric-card">
                <div class="metric-label">⚡ Drain Rate</div>
                <div class="metric-value cyan" id="drainRate">0<span class="metric-unit">ops/s</span></div>
                <div class="metric-detail">Configured limit</div>
            </div>
        </div>
        
        <div class="timeline-section">
            <h2 class="section-title">📊 Timeline</h2>
            <div class="timeline">
                <div class="timeline-item">
                    <div class="timeline-label">First Request</div>
                    <div class="timeline-value" id="firstRequest">--:--:--.---</div>
                </div>
                <div class="timeline-item">
                    <div class="timeline-label">Last Request</div>
                    <div class="timeline-value" id="lastRequest">--:--:--.---</div>
                </div>
                <div class="timeline-item">
                    <div class="timeline-label">First DB Write</div>
                    <div class="timeline-value" id="firstDBWrite">--:--:--.---</div>
                </div>
                <div class="timeline-item">
                    <div class="timeline-label">Last DB Write</div>
                    <div class="timeline-value" id="lastDBWrite">--:--:--.---</div>
                </div>
            </div>
        </div>
        
        <div class="last-update">
            Last updated: <span id="lastUpdate">-</span>
        </div>
    </div>
    
    <script>
        let autoRefresh = true;
        let refreshInterval;
        let maxQueueSeen = 100;
        
        async function fetchMetrics() {
            try {
                const response = await fetch('/metrics');
                const data = await response.json();
                updateDashboard(data);
            } catch (error) {
                console.error('Error fetching metrics:', error);
            }
        }
        
        function updateDashboard(data) {
            // Update status indicators
            const drainerStatus = document.getElementById('drainerStatus');
            const drainerText = document.getElementById('drainerText');
            if (data.drainer_running) {
                drainerStatus.className = 'status-dot active';
                drainerText.textContent = 'Running';
            } else {
                drainerStatus.className = 'status-dot inactive';
                drainerText.textContent = 'Stopped';
            }
            
            const testStatus = document.getElementById('testStatus');
            const testText = document.getElementById('testText');
            if (data.test_running) {
                testStatus.className = 'status-dot active';
                testText.textContent = 'Active';
            } else {
                testStatus.className = 'status-dot inactive';
                testText.textContent = 'Idle';
            }
            
            // Update queue type
            const queueTypeText = document.getElementById('queueTypeText');
            if (data.queue_type) {
                queueTypeText.textContent = data.queue_type.toUpperCase();
            }
            
            // Update main metrics
            const queueSize = data.current_queue_size || 0;
            document.getElementById('queueSize').textContent = queueSize;
            document.getElementById('peakQueue').textContent = data.peak_queue_size || 0;
            document.getElementById('requestsReceived').textContent = data.total_requests_received || 0;
            document.getElementById('dbWrites').textContent = data.db_record_count || 0;
            
            // Update queue visualization
            if (queueSize > maxQueueSeen) maxQueueSeen = Math.max(queueSize * 1.5, 100);
            const queuePercent = Math.min((queueSize / maxQueueSeen) * 100, 100);
            document.getElementById('queueFill').style.width = queuePercent + '%';
            document.getElementById('maxQueueLabel').textContent = Math.round(maxQueueSeen);
            
            // Update timing metrics
            const totalElapsed = data.total_elapsed_seconds || 0;
            document.getElementById('totalElapsed').innerHTML = totalElapsed.toFixed(1) + '<span class="metric-unit">s</span>';
            
            const drainRate = data.configured_drain_rate || 0;
            document.getElementById('drainRate').innerHTML = drainRate + '<span class="metric-unit">ops/s</span>';
            
            // Update DB write duration
            const dbDuration = data.db_write_duration_seconds || 0;
            document.getElementById('dbDuration').innerHTML = dbDuration.toFixed(1) + '<span class="metric-unit">s</span>';
            
            // Update timeline
            document.getElementById('firstRequest').textContent = data.first_request_time || '--:--:--.---';
            document.getElementById('lastRequest').textContent = data.last_request_time || '--:--:--.---';
            document.getElementById('firstDBWrite').textContent = data.first_db_write_time || '--:--:--.---';
            document.getElementById('lastDBWrite').textContent = data.last_db_write_time || '--:--:--.---';
            
            // Update last update time
            document.getElementById('lastUpdate').textContent = new Date().toLocaleTimeString();
        }
        
        async function resetMetrics() {
            try {
                await fetch('/metrics/reset', { method: 'POST' });
                maxQueueSeen = 100;
                fetchMetrics();
            } catch (error) {
                console.error('Error resetting metrics:', error);
            }
        }
        
        function toggleAutoRefresh() {
            autoRefresh = !autoRefresh;
            const btn = document.getElementById('refreshBtn');
            if (autoRefresh) {
                btn.textContent = '⏸ Pause';
                startRefresh();
            } else {
                btn.textContent = '▶ Resume';
                clearInterval(refreshInterval);
            }
        }
        
        function startRefresh() {
            refreshInterval = setInterval(fetchMetrics, 500);
        }
        
        // Initial fetch and start auto-refresh
        fetchMetrics();
        startRefresh();
    </script>
</body>
</html>`
