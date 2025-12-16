# Performance Test Guide

## Overview
This performance test demonstrates the Shock Absorber's ability to handle high write traffic (20 TPS) while throttling database writes to 1 RPS.

## Test Configuration
- **Request Rate**: 20 requests/second
- **Duration**: 30 seconds
- **Total Requests**: 600
- **DB Write Rate**: 1 request/second (configured)
- **Expected Behavior**: 
  - All 600 requests accepted immediately (written to Redis/Kafka)
  - DB writes complete over ~600 seconds (1 RPS)

## Prerequisites

1. **Start Redis** (if using memory queue, skip this):
   ```bash
   brew services start redis
   # or
   redis-server
   ```

2. **Start Kafka** (required for this test):
   ```bash
   # Using Docker
   docker run -p 9092:9092 apache/kafka:latest
   
   # Or using local Kafka installation
   # Follow Kafka setup instructions for your system
   ```

3. **Start MySQL** (if not already running):
   ```bash
   docker run --name mysql -e MYSQL_ROOT_PASSWORD=password -p 3306:3306 -d mysql:latest
   ```

4. **Setup Database**:
   ```bash
   docker exec mysql mysql -uroot -ppassword testdb < scripts/setup_payment_db.sql
   ```

## Running the Test

### Step 1: Start the Test Server
```bash
go run cmd/test-server/main.go
```

The server will:
- Connect to Redis (localhost:6379)
- Connect to MySQL (localhost:3306)
- Connect to Kafka (localhost:9092)
- Start the drainer with 1 RPS rate limiting

### Step 2: Run the Load Test

**Option 1: Python Script (Recommended)**
```bash
python3 scripts/load_test.py
```

**Option 2: Bash Script**
```bash
bash scripts/load_test.sh
```

## What to Observe

### During the Test (30 seconds)
1. **Request Phase**:
   - 600 requests sent at 20 req/s
   - All requests accepted immediately
   - Logs show: `[KAFKA] ✓ Successfully produced message to Kafka`
   - Queue size grows: `[DRAINER] Queue size: 1, 2, 3, ... 600`

2. **Drainer Logs**:
   - `[DRAINER] [timestamp] Rate limiter allowed operation`
   - `[KAFKA] [timestamp] ✓ Consumed message from Kafka`
   - `[DRAINER] [timestamp] Writing to MySQL database...`
   - `[MYSQL] CREATE successful, rows affected: 1`
   - Operations processed at exactly 1 per second

### After 30 Seconds
- All 600 requests have been sent
- Queue should have ~570 operations remaining (600 - 30 processed)
- DB writes continue at 1 RPS
- Queue drains over the next ~570 seconds

## Expected Log Output

### Request Phase (First 30 seconds):
```
[KAFKA] [2025-12-17 00:30:41.123] === PRODUCING TO KAFKA ===
[KAFKA] [2025-12-17 00:30:41.123] ✓ Successfully produced message to Kafka topic 'shock-absorber-writeback'
[DRAINER] [2025-12-17 00:30:41.125] Queue size: 1
[DRAINER] [2025-12-17 00:30:41.125] Rate limiter allowed operation (waited: 0ms, queue size: 1)
[KAFKA] [2025-12-17 00:30:41.126] === CONSUMING FROM KAFKA ===
[KAFKA] [2025-12-17 00:30:41.127] ✓ Consumed message from Kafka
[DRAINER] [2025-12-17 00:30:41.128] === PROCESSING OPERATION #1 ===
[DRAINER] [2025-12-17 00:30:41.128] Writing to MySQL database...
[MYSQL] Executing statement: INSERT INTO payment...
[MYSQL] CREATE successful, rows affected: 1
[DRAINER] [2025-12-17 00:30:41.130] ✓ Successfully written to MySQL
```

### Drain Phase (After 30 seconds):
```
[DRAINER] [2025-12-17 00:31:42.130] Rate limiter allowed operation (waited: 1000ms, queue size: 571)
[KAFKA] [2025-12-17 00:31:42.131] ✓ Consumed message from Kafka
[DRAINER] [2025-12-17 00:31:42.132] === PROCESSING OPERATION #31 ===
...
```

## Key Metrics to Monitor

1. **Queue Size**: Should grow to ~600, then decrease at 1 per second
2. **DB Write Rate**: Exactly 1 operation per second (1000ms intervals)
3. **Request Acceptance**: All 600 requests accepted immediately
4. **Total Processing Time**: ~600 seconds to drain all operations

## Verification

After the test completes, verify all payments were written:

```bash
docker exec mysql mysql -uroot -ppassword testdb -t -e "SELECT COUNT(*) as total_payments FROM payment WHERE upi_reference_id LIKE 'perf_test_%';"
```

Expected: 600 payments

## Troubleshooting

### Messages not being consumed
- Check Kafka is running: `docker ps | grep kafka`
- Check consumer group: Kafka manages offsets automatically
- Verify topic exists: `kafka-topics --list --bootstrap-server localhost:9092`

### Rate limiting not working
- Check drain rate in config: `config.WriteBack.DrainRate = 1`
- Verify rate limiter logs show 1000ms waits between operations

### Queue size not accurate
- Kafka queue size is approximate (tracked in memory)
- Use Kafka tools to check actual topic size

