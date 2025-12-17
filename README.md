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
- KV Store (choose one):
  - **Redis** (default): For in-memory caching
  - **DynamoDB**: For AWS-managed NoSQL storage
  - **Cassandra**: Future support
- MySQL (for persistent storage)
- Kafka (optional, for production)

### Installation

```bash
go get github.com/rzpsarthak13/shock-absorber
```

### Basic Usage

#### Using Redis (Default)

```go
package main

import (
    "context"
    "time"
    
    "github.com/rzpsarthak13/shock-absorber/pkg/shockabsorber"
)

func main() {
    // Create configuration with Redis
    config := shockabsorber.DefaultConfig()
    config.KVStore.Type = "redis"
    config.KVStore.RedisConfig.Endpoints = []string{"localhost:6379"}
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

#### Using DynamoDB

```go
package main

import (
    "context"
    "time"
    
    "github.com/rzpsarthak13/shock-absorber/pkg/shockabsorber"
)

func main() {
    // Create configuration with DynamoDB
    config := shockabsorber.DefaultConfig()
    config.KVStore.Type = "dynamodb"
    config.KVStore.DynamoDBConfig = shockabsorber.DynamoDBConfig{
        Region:    "us-east-1",
        TableName: "shock-absorber-cache",
    }
    config.Database.Host = "localhost"
    config.Database.Port = 3306
    config.Database.Database = "mydb"
    config.Database.Username = "root"
    config.Database.Password = "password"
    config.WriteBack.DrainRate = 50
    
    // Create client - same API, different backend!
    client, err := shockabsorber.NewClient(config)
    if err != nil {
        panic(err)
    }
    defer client.Close()
    
    // All operations work identically - backend is transparent!
    ctx := context.Background()
    table, err := client.EnableKV(ctx, "users")
    if err != nil {
        panic(err)
    }
    
    // Same API - works with DynamoDB instead of Redis
    record := map[string]interface{}{
        "id":    "user_123",
        "name":  "John Doe",
        "email": "john@example.com",
    }
    if err := table.Create(ctx, record); err != nil {
        panic(err)
    }
    
    user, err := table.Read(ctx, "user_123")
    if err != nil {
        panic(err)
    }
}
```

**Key Point**: The API is identical regardless of the backend. Switch between Redis and DynamoDB by changing only the configuration!

## ⚙️ Configuration

### KV Store Configuration (Pluggable Backends)

Shock Absorber supports multiple NoSQL backends through a unified configuration interface. Simply change the `Type` field to switch between backends.

#### Redis Configuration (Default)

```go
config := shockabsorber.DefaultConfig()

// KV Store - Redis
config.KVStore.Type = "redis"
config.KVStore.RedisConfig = shockabsorber.RedisConfig{
    Endpoints:    []string{"localhost:6379"},
    ClusterMode:  false,
    Password:     "",
    DB:           0,
    PoolSize:     10,
    MinIdleConns: 5,
}
config.KVStore.MaxRetries = 3
config.KVStore.DialTimeout = 5 * time.Second
config.KVStore.ReadTimeout = 3 * time.Second
config.KVStore.WriteTimeout = 3 * time.Second
```

**YAML Configuration (Redis)**:
```yaml
kvstore:
  type: "redis"
  redis_config:
    endpoints: ["localhost:6379"]
    cluster_mode: false
    password: ""
    db: 0
    pool_size: 10
    min_idle_conns: 5
  max_retries: 3
  dial_timeout: 5s
  read_timeout: 3s
  write_timeout: 3s
```

#### DynamoDB Configuration

```go
config := shockabsorber.DefaultConfig()

// KV Store - DynamoDB
config.KVStore.Type = "dynamodb"
config.KVStore.DynamoDBConfig = shockabsorber.DynamoDBConfig{
    Region:      "us-east-1",
    TableName:   "shock-absorber-cache",
    Endpoint:    "", // Optional, for LocalStack: "http://localhost:8000"
    AccessKeyID: "", // Optional, can use IAM role instead
    SecretAccessKey: "", // Optional, can use IAM role instead
}
config.KVStore.MaxRetries = 3
config.KVStore.DialTimeout = 5 * time.Second
config.KVStore.ReadTimeout = 3 * time.Second
config.KVStore.WriteTimeout = 3 * time.Second
```

**YAML Configuration (DynamoDB)**:
```yaml
kvstore:
  type: "dynamodb"
  dynamodb_config:
    region: "us-east-1"
    table_name: "shock-absorber-cache"
    endpoint: ""  # Optional, for LocalStack: "http://localhost:8000"
    access_key_id: ""  # Optional, can use IAM role instead
    secret_access_key: ""  # Optional, can use IAM role instead
  max_retries: 3
  dial_timeout: 5s
  read_timeout: 3s
  write_timeout: 3s
```

**Note**: When using DynamoDB, ensure the table exists with:
- Partition key: `key` (String)
- Optional TTL attribute: `ttl` (Number)
- Optional: `created_at` (String)

#### Switching Between Backends

Switching between backends requires **only configuration changes** - no code modifications:

```go
// Switch from Redis to DynamoDB
config.KVStore.Type = "dynamodb"
config.KVStore.DynamoDBConfig = shockabsorber.DynamoDBConfig{
    Region:    "us-east-1",
    TableName: "shock-absorber-cache",
}

// Switch back to Redis
config.KVStore.Type = "redis"
config.KVStore.RedisConfig = shockabsorber.RedisConfig{
    Endpoints: []string{"localhost:6379"},
}

// All table operations work identically with both backends!
table.Create(ctx, record)  // Works with Redis or DynamoDB
table.Read(ctx, key)        // Works with Redis or DynamoDB
```

### Database Configuration

```go
// Database (MySQL)
config.Database.Type = "mysql"
config.Database.Host = "localhost"
config.Database.Port = 3306
config.Database.Database = "mydb"
config.Database.Username = "root"
config.Database.Password = "password"
```

### Write-Back Configuration

```go
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

### Pluggable NoSQL KV Store Architecture

Shock Absorber uses a **plugin-based, config-driven architecture** with the **Strategy pattern** to support multiple NoSQL databases. You can switch between different KV stores (Redis, DynamoDB, Cassandra, etc.) by simply changing the configuration - **no code changes needed**.

#### Architecture Overview

```
Config (Type: "redis" | "dynamodb" | "cassandra" | ...)
    ↓
KV Store Factory (Strategy Pattern)
    ↓
KV Store Registry (registers implementations)
    ↓
core.KVStore Interface (unchanged)
    ↓
Table Implementation (uses interface, no changes)
```

#### Strategy Pattern Implementation

The system uses the **Strategy pattern** to eliminate conditional logic (no if-else statements):

1. **Factory Strategy**: Each backend (Redis, DynamoDB, etc.) implements `KVStoreFactory` interface
   - `Create(config)` - Creates the appropriate KV store instance
   - `Type()` - Returns the backend type identifier
   - `Validate(config)` - Validates backend-specific configuration

2. **Validation Strategy**: Each backend provides its own `ConfigValidator` implementation
   - `Validate(config)` - Validates backend-specific configuration
   - `Type()` - Returns the backend type identifier

3. **Auto-Registration**: All implementations register themselves automatically on package initialization
   - No manual registration needed
   - New backends are discovered automatically

#### Key Benefits

- ✅ **Zero Code Changes**: Switch backends via configuration only
- ✅ **No If-Else**: Strategy pattern eliminates conditional logic
- ✅ **Extensible**: Easy to add new NoSQL databases
- ✅ **Maintainable**: Each implementation is isolated
- ✅ **Testable**: Can mock any backend
- ✅ **Future-Proof**: Ready for Cassandra, MongoDB, etc.

#### Supported Backends

- **Redis** (default): In-memory data store, ideal for high-performance caching
- **DynamoDB**: AWS managed NoSQL database, serverless and scalable
- **Cassandra**: Future support for distributed NoSQL database

#### Adding a New Backend

To add a new NoSQL database backend:

1. Create a new file `internal/kvstore/newdb.go`
2. Implement `core.KVStore` interface
3. Create factory implementing `KVStoreFactory`
4. Create validator implementing `ConfigValidator`
5. Register both in `init()` function
6. Add config struct to `pkg/shockabsorber/config.go`
7. Done! The new backend is automatically available

Example structure:
```go
// In internal/kvstore/newdb.go
type NewDBKVStore struct { /* ... */ }
type NewDBKVStoreFactory struct {}
type NewDBConfigValidator struct {}

func init() {
    RegisterFactory(&NewDBKVStoreFactory{})
    RegisterValidator(&NewDBConfigValidator{})
}
```

### Components

1. **KV Store** (Pluggable: Redis/DynamoDB/Cassandra)
   - Write-ahead log (WAL) for all operations
   - Cache for fast reads
   - TTL-based expiration
   - Backend-agnostic interface

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

All write operations are logged to the configured KV store before acknowledgment:
- Ensures no data loss
- Enables recovery on failure
- Maintains operation order
- Works with any backend (Redis, DynamoDB, etc.)

### Cache Strategy

- **Cache-First Reads**: Check KV store, fallback to DB
- **Automatic Cache Updates**: DB reads update cache
- **TTL Management**: Configurable per-table TTL
- **Backend-Agnostic**: Same behavior regardless of KV store type

### Strategy Pattern Deep Dive

The Strategy pattern is used throughout the pluggable architecture to eliminate conditional logic and make the system extensible. Here's how it works:

#### 1. Factory Strategy Pattern

Instead of using if-else statements to select implementations, each backend provides its own factory:

```go
// Strategy Interface
type KVStoreFactory interface {
    Create(config KVStoreConfig) (core.KVStore, error)
    Type() string
    Validate(config KVStoreConfig) error
}

// Redis Strategy Implementation
type RedisKVStoreFactory struct{}
func (f *RedisKVStoreFactory) Type() string { return "redis" }
func (f *RedisKVStoreFactory) Create(config KVStoreConfig) (core.KVStore, error) {
    // Redis-specific creation logic
}

// DynamoDB Strategy Implementation
type DynamoDBKVStoreFactory struct{}
func (f *DynamoDBKVStoreFactory) Type() string { return "dynamodb" }
func (f *DynamoDBKVStoreFactory) Create(config KVStoreConfig) (core.KVStore, error) {
    // DynamoDB-specific creation logic
}
```

**Usage (No If-Else!)**:
```go
// Old way (with if-else - NOT USED):
if config.Type == "redis" {
    kvStore = NewRedisKVStore(...)
} else if config.Type == "dynamodb" {
    kvStore = NewDynamoDBKVStore(...)
}

// New way (Strategy pattern):
kvStore, err := kvstore.Create(config)  // Factory selects strategy automatically
```

#### 2. Validation Strategy Pattern

Each backend validates its own configuration using the Strategy pattern:

```go
// Strategy Interface
type ConfigValidator interface {
    Validate(config *InternalConfig) error
    Type() string
}

// Redis Validation Strategy
type RedisConfigValidator struct{}
func (v *RedisConfigValidator) Validate(config *InternalConfig) error {
    // Redis-specific validation
}

// DynamoDB Validation Strategy
type DynamoDBConfigValidator struct{}
func (v *DynamoDBConfigValidator) Validate(config *InternalConfig) error {
    // DynamoDB-specific validation
}
```

**Usage (No If-Else!)**:
```go
// Old way (with if-else - NOT USED):
if config.KVStore.Type == "redis" {
    // validate Redis config
} else if config.KVStore.Type == "dynamodb" {
    // validate DynamoDB config
}

// New way (Strategy pattern):
validator, _ := GetValidator(config.KVStore.Type)
err := validator.Validate(config)  // Strategy validates automatically
```

#### 3. Auto-Registration Pattern

All implementations register themselves automatically on package initialization:

```go
// In internal/kvstore/redis.go
func init() {
    RegisterFactory(&RedisKVStoreFactory{})
    RegisterValidator(&RedisConfigValidator{})
}

// In internal/kvstore/dynamodb.go
func init() {
    RegisterFactory(&DynamoDBKVStoreFactory{})
    RegisterValidator(&DynamoDBConfigValidator{})
}
```

This means:
- ✅ No manual registration needed
- ✅ New backends are automatically discovered
- ✅ No central registry to maintain
- ✅ Each implementation is self-contained

#### Benefits of Strategy Pattern

1. **No Conditional Logic**: Eliminates if-else chains
2. **Open/Closed Principle**: Open for extension, closed for modification
3. **Single Responsibility**: Each strategy handles one backend
4. **Easy Testing**: Mock any strategy independently
5. **Type Safety**: Compile-time guarantees
6. **Extensibility**: Add new backends without modifying existing code

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
- Write to KV store (Redis/DynamoDB) immediately
- Drain to MySQL at 50 RPS
- Maintain sub-10ms response times
- **Switch backends** based on infrastructure (Redis for on-prem, DynamoDB for AWS)

### Event Logging
- Log events at high rate
- Batch write to database
- Prevent database overload
- Use DynamoDB for serverless deployments

### Real-time Analytics
- Fast writes to cache
- Async aggregation to DB
- Real-time queries from cache
- Choose backend based on latency requirements (Redis for lowest latency)

### Multi-Cloud Deployments
- Use Redis for on-premise or self-hosted deployments
- Use DynamoDB for AWS-native deployments
- **Same codebase** works with both - just change configuration

## 🛠️ Development

### Project Structure

```
shock-absorber/
├── cmd/
│   └── test-server/      # Test HTTP server
├── internal/
│   ├── client/          # Internal client implementation
│   ├── core/            # Core interfaces (KVStore, Database, etc.)
│   ├── database/        # Database implementations (MySQL)
│   ├── kvstore/         # Pluggable KV store implementations
│   │   ├── factory.go   # Factory interface and registry (Strategy pattern)
│   │   ├── redis.go     # Redis implementation + factory + validator
│   │   └── dynamodb.go  # DynamoDB implementation + factory + validator
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

