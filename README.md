# Shock Absorber

A high-performance, memory-first database abstraction layer that absorbs traffic spikes by writing to Redis/ElastiCache first, then asynchronously draining to a persistent database at a controlled rate.

## 🎯 Overview

Shock Absorber is designed to handle high-traffic scenarios where your application needs to:
- **Absorb traffic spikes** (e.g., 200 TPS) by writing to fast in-memory stores
- **Protect your database** by throttling writes to a sustainable rate (e.g., 50 RPS)
- **Maintain data consistency** with write-ahead logging and asynchronous write-back
- **Scale horizontally** with distributed queue support (Kafka)

### Key Benefits

- ✅ **High Throughput**: Handle 200+ TPS by writing to Redis/Kafka
- ✅ **Database Protection**: Throttle DB writes to prevent overload
- ✅ **Low Latency**: Sub-millisecond writes to Redis
- ✅ **Fault Tolerance**: Write-ahead logging ensures no data loss
- ✅ **Scalable**: Supports Kafka for distributed processing
- ✅ **Cache-First Reads**: Automatic cache with database fallback

## 🏗️ Architecture

```
┌─────────────┐
│   Client    │
│  (200 TPS)  │
└──────┬──────┘
       │
       ▼
┌─────────────────────────────────┐
│      Shock Absorber             │
│                                 │
│  ┌──────────┐    ┌──────────┐ │
│  │   Redis   │    │  Kafka   │ │
│  │  (WAL)    │───▶│  Queue   │ │
│  └──────────┘    └─────┬─────┘ │
│                        │        │
│                        ▼        │
│              ┌──────────────┐  │
│              │   Drainer     │  │
│              │  (Rate Limit) │  │
│              │   1-50 RPS    │  │
│              └───────┬───────┘  │
└──────────────────────┼──────────┘
                        │
                        ▼
                 ┌──────────┐
                 │  MySQL   │
                 │ (1-50 RPS)│
                 └──────────┘
```

### Data Flow

1. **Write Operation**:
   - Client writes to Redis immediately (write-ahead log)
   - Operation is enqueued to Kafka/Memory queue
   - Response returned immediately

2. **Read Operation**:
   - Check Redis cache first
   - Fallback to database if not in cache
   - Update cache on database read

3. **Write-Back**:
   - Background drainer consumes from queue
   - Rate-limited writes to database (configurable RPS)
   - Ensures database is not overwhelmed

## 🚀 Quick Start

### Prerequisites

- Go 1.21+
- Redis (for KV store)
- MySQL (for persistent storage)
- Kafka (optional, for production)

### Installation

```bash
go get github.com/rzpsarthak13/shock-absorber
```

### Basic Usage

```go
package main

import (
    "context"
    "time"
    
    "github.com/rzpsarthak13/shock-absorber/pkg/shockabsorber"
)

func main() {
    // Create configuration
    config := shockabsorber.DefaultConfig()
    config.KVStore.Endpoints = []string{"localhost:6379"}
    config.Database.Host = "localhost"
    config.Database.Port = 3306
    config.Database.Database = "mydb"
    config.Database.Username = "root"
    config.Database.Password = "password"
    config.WriteBack.DrainRate = 50 // 50 RPS for DB writes
    
    // Create client
    client, err := shockabsorber.NewClient(config)
    if err != nil {
        panic(err)
    }
    defer client.Close()
    
    // Enable KV for a table
    ctx := context.Background()
    table, err := client.EnableKV(ctx, "users")
    if err != nil {
        panic(err)
    }
    
    // Create a record (writes to Redis immediately, queues for DB)
    record := map[string]interface{}{
        "id":    "user_123",
        "name":  "John Doe",
        "email": "john@example.com",
    }
    if err := table.Create(ctx, record); err != nil {
        panic(err)
    }
    
    // Read a record (checks Redis first, falls back to DB)
    user, err := table.Read(ctx, "user_123")
    if err != nil {
        panic(err)
    }
    
    // Update a record
    updates := map[string]interface{}{
        "name": "Jane Doe",
    }
    if err := table.Update(ctx, "user_123", updates); err != nil {
        panic(err)
    }
}
```

## ⚙️ Configuration

### Basic Configuration

```go
config := shockabsorber.DefaultConfig()

// KV Store (Redis/ElastiCache)
config.KVStore.Type = "redis"
config.KVStore.Endpoints = []string{"localhost:6379"}
config.KVStore.Password = ""
config.KVStore.PoolSize = 10

// Database (MySQL)
config.Database.Type = "mysql"
config.Database.Host = "localhost"
config.Database.Port = 3306
config.Database.Database = "mydb"
config.Database.Username = "root"
config.Database.Password = "password"

// Write-Back Configuration
config.WriteBack.DrainRate = 50        // DB writes per second
config.WriteBack.BatchSize = 100       // Batch size for processing
config.WriteBack.DefaultTTL = 1 * time.Hour  // Cache TTL
config.WriteBack.QueueType = "kafka"   // "memory", "redis", or "kafka"
```

### Kafka Configuration

```go
config.WriteBack.QueueType = "kafka"
config.WriteBack.KafkaConfig = shockabsorber.KafkaConfig{
    Brokers:        []string{"localhost:9092"},
    Topic:          "shock-absorber-writeback",
    GroupID:        "shock-absorber-writeback",
    BatchSize:      100,
    RequiredAcks:   -1, // All replicas
    MaxMessageBytes: 1000000, // 1MB
}
```

### Table-Specific Configuration

```go
// Configure specific table
config.Tables["payments"] = shockabsorber.TableConfig{
    TTL:                2 * time.Hour,
    WriteBackBatchSize: 50,
    DrainRate:          25, // Override global drain rate
    Namespace:          "payments",
}

// Or use options when enabling KV
table, err := client.EnableKV(ctx, "payments",
    shockabsorber.WithTTL(2*time.Hour),
    shockabsorber.WithNamespace("payments"),
    shockabsorber.WithDrainRate(25),
)
```

## 📚 API Reference

### Client Interface

```go
type Client interface {
    // Enable KV store caching for a table
    EnableKV(ctx context.Context, tableName string, opts ...TableOption) (Table, error)
    
    // Disable KV store caching for a table
    DisableKV(ctx context.Context, tableName string) error
    
    // Get a table instance
    GetTable(tableName string) (Table, error)
    
    // Close all connections
    Close() error
}
```

### Table Interface

```go
type Table interface {
    // Create a new record
    Create(ctx context.Context, record map[string]interface{}) error
    
    // Read a record by primary key
    Read(ctx context.Context, key interface{}) (map[string]interface{}, error)
    
    // Update a record
    Update(ctx context.Context, key interface{}, updates map[string]interface{}) error
    
    // Delete a record
    Delete(ctx context.Context, key interface{}) error
    
    // Find by non-primary key field
    FindByField(ctx context.Context, fieldName string, fieldValue interface{}) (map[string]interface{}, error)
    
    // Enable/Disable KV caching
    EnableKV() error
    DisableKV() error
}
```

## 🧪 Performance Testing

See [PERFORMANCE_TEST.md](./PERFORMANCE_TEST.md) for detailed performance testing guide.

### Quick Performance Test

```bash
# Start test server
go run cmd/test-server/main.go

# Run load test (20 req/s for 30 seconds = 600 requests)
python3 scripts/load_test.py
```

**Expected Results**:
- 600 requests accepted immediately (written to Redis/Kafka)
- DB writes at 1 RPS (configurable)
- Queue drains over ~600 seconds
- All data eventually persisted to MySQL

## 🏛️ Architecture Details

### Components

1. **KV Store** (Redis/ElastiCache)
   - Write-ahead log (WAL) for all operations
   - Cache for fast reads
   - TTL-based expiration

2. **Write-Back Queue** (Memory/Redis/Kafka)
   - Memory: In-process channel (testing)
   - Redis: Redis Lists (single instance)
   - Kafka: Distributed queue (production)

3. **Drainer**
   - Background goroutine
   - Rate-limited processing
   - Configurable drain rate (RPS)

4. **Schema Translator**
   - Converts between relational and KV formats
   - Handles JSON columns
   - Type mapping and validation

### Write-Ahead Log (WAL)

All write operations are logged to Redis before acknowledgment:
- Ensures no data loss
- Enables recovery on failure
- Maintains operation order

### Cache Strategy

- **Cache-First Reads**: Check Redis, fallback to DB
- **Automatic Cache Updates**: DB reads update cache
- **TTL Management**: Configurable per-table TTL

## 🔧 Local Development Setup

### 1. Start Dependencies

```bash
# Start Redis
brew services start redis
# or
redis-server

# Start MySQL (Docker)
docker run --name mysql \
  -e MYSQL_ROOT_PASSWORD=password \
  -p 3306:3306 \
  -d mysql:latest

# Start Kafka (Docker)
docker run -p 9092:9092 apache/kafka:latest
```

### 2. Setup Database

```bash
# Create database and tables
docker exec mysql mysql -uroot -ppassword < scripts/setup_payment_db.sql
```

### 3. Run Test Server

```bash
go run cmd/test-server/main.go
```

### 4. Test API

```bash
# Create a payment
curl -X POST http://localhost:8080/payment \
  -H "Content-Type: application/json" \
  -d '{
    "upi_reference_id": "ref_001",
    "amount": 10000,
    "currency": "INR",
    "upi_transaction_id": "txn_001",
    "tenant_id": "tenant_001",
    "customer_id": "cust_001",
    "leg": "PAYER",
    "action": "PAY",
    "status": "PENDING",
    "payer": {"vpa": "user@upi", "name": "John Doe"},
    "payees": [{"vpa": "merchant@upi", "name": "Merchant", "amount": 10000}],
    "meta": {"msg_id": "msg_001", "note": "Test payment"},
    "resp_meta": {"timestamp": 1234567890}
  }'

# Get payment by reference
curl http://localhost:8080/payment/reference/ref_001
```

## 📊 Monitoring & Logging

All operations include detailed, timestamped logs:

- `[KAFKA]` - Kafka queue operations
- `[REDIS]` - Redis operations
- `[MYSQL]` - Database operations
- `[TABLE]` - Table operations
- `[DRAINER]` - Write-back drainer operations

Example log output:
```
[KAFKA] [2025-12-17 00:30:41.123] ✓ Successfully produced message to Kafka
[DRAINER] [2025-12-17 00:30:41.125] Rate limiter allowed operation (waited: 0ms, queue size: 1)
[KAFKA] [2025-12-17 00:30:41.127] ✓ Consumed message from Kafka (Read Duration: 2ms)
[DRAINER] [2025-12-17 00:30:41.128] Writing to MySQL database...
[MYSQL] CREATE successful, rows affected: 1
[DRAINER] [2025-12-17 00:30:41.130] ✓ Successfully written to MySQL (DB Write Duration: 2ms)
```

## 🎛️ Configuration Options

### Queue Types

- **`memory`**: In-process channel (testing only)
- **`redis`**: Redis Lists (single instance)
- **`kafka`**: Apache Kafka (production, distributed)

### Rate Limiting

Control database write rate to protect your DB:
- `DrainRate`: Operations per second (e.g., 50 RPS)
- Applied per table or globally
- Uses token bucket algorithm

### TTL Configuration

- Global default TTL
- Per-table TTL override
- Automatic cache expiration

## 🔍 Use Cases

### High-Traffic Payment Processing
- Handle 200+ TPS payment requests
- Write to Redis immediately
- Drain to MySQL at 50 RPS
- Maintain sub-10ms response times

### Event Logging
- Log events at high rate
- Batch write to database
- Prevent database overload

### Real-time Analytics
- Fast writes to cache
- Async aggregation to DB
- Real-time queries from cache

## 🛠️ Development

### Project Structure

```
shock-absorber/
├── cmd/
│   └── test-server/      # Test HTTP server
├── internal/
│   ├── client/          # Internal client implementation
│   ├── core/            # Core interfaces
│   ├── database/        # Database implementations (MySQL)
│   ├── kvstore/         # KV store implementations (Redis)
│   ├── read/            # Read handlers (cache, fallback)
│   ├── registry/        # Table registry and config
│   ├── schema/          # Schema translation
│   ├── table/           # Table implementation
│   ├── write/           # Write handlers (WAL)
│   └── writeback/       # Write-back queue implementations
├── pkg/
│   └── shockabsorber/   # Public API
└── scripts/            # Setup and test scripts
```

### Building

```bash
# Build all packages
go build ./...

# Build test server
go build ./cmd/test-server

# Run tests
go test ./...
```

## 📝 License

[Add your license here]

## 🤝 Contributing

[Add contributing guidelines here]

## 📞 Support

[Add support information here]

---

**Note**: This is a production-ready implementation designed to handle high-traffic scenarios while protecting your database from overload.

