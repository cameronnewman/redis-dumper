# Redis Dumper

A high-performance tool for exporting Redis data to CSV or Parquet format with Hive-style partitioning for optimal DuckDB querying.

## Features

- Export Redis data to CSV or Parquet format
- Memory-efficient streaming for large datasets
- Hive-style partitioning for efficient querying
- Support for all Redis data types (strings, hashes, sets, sorted sets, lists)
- Configurable batch sizes and file rotation
- TLS/SSL support
- DuckDB-optimized output format

## Installation

```bash
go install github.com/cameronnewman/redis-dumper/cmd/dumper@latest
```

Or build from source:

```bash
git clone https://github.com/cameronnewman/redis-dumper.git
cd redis-dumper
go build -o dumper ./cmd/dumper
```

## Usage

The tool uses subcommands and environment variables for configuration.

### Commands

- `keys-only` - Export only key metadata (recommended for large datasets)
- `pattern` - Export full data for keys matching a pattern
- `full` - Export all data (use with caution on large datasets)

### Basic Usage

Export only key metadata:
```bash
dumper keys-only
```

Export keys matching a pattern:
```bash
dumper pattern "user:*"
```

Export all data:
```bash
dumper full
```

### Using Environment Variables

Configure via environment variables:
```bash
export REDIS_URL=redis://localhost:6379/0
export OUTPUT_DIR=./export
export OUTPUT_FORMAT=csv
export BATCH_SIZE=5000

dumper keys-only
```

Or inline:
```bash
REDIS_URL=redis://localhost:6379 OUTPUT_DIR=./export dumper pattern "session:*"
```

### TLS/SSL Support

For Redis with TLS:
```bash
REDIS_URL=rediss://user:pass@redis.example.com:6380/0 dumper keys-only
```

Or manually enable TLS:
```bash
export REDIS_URL=redis://redis.example.com:6380
export ENABLE_TLS=true
export SKIP_TLS_VERIFY=true
dumper keys-only
```

## Configuration

All configuration is done through environment variables:

| Variable | Description | Default |
|----------|-------------|---------|
| `REDIS_URL` | Redis connection URL | `redis://localhost:6379/0` |
| `OUTPUT_DIR` | Output directory path | `/tmp/dumper` |
| `OUTPUT_FORMAT` | Output format: csv or parquet | `parquet` |
| `BATCH_SIZE` | Number of keys to process in each batch | `1000` |
| `MAX_RECORDS_PER_FILE` | Maximum records per file before rotation | `100000` |
| `ENABLE_TLS` | Enable TLS connection | `false` |
| `SKIP_TLS_VERIFY` | Skip TLS certificate verification | `true` |

### Redis URL Schemes

- `redis://` - Plain connection
- `rediss://` - TLS connection (automatically enables TLS)

## Output Format

Data is exported with Hive-style partitioning:
```
output/
├── year=2024/
│   └── month=01/
│       └── day=15/
│           └── hour=14/
│               ├── redis_data_part_0001.csv
│               └── redis_data_part_0002.csv
└── export_metadata.json
```

### Schema

All Redis data is exported with a unified schema:

| Column | Type | Description |
|--------|------|-------------|
| key | string | Redis key |
| type | string | Redis data type |
| value | string | Serialized value |
| ttl_seconds | int64 | TTL in seconds (-1 if no TTL) |
| exported_at | string | Export timestamp |
| partition_id | int | Partition identifier |

### Parquet Schema Details

The Parquet files use the following schema definition:

```
message redis_data {
  optional binary key (STRING);
  optional binary type (STRING);
  optional binary value (STRING);
  optional int64 ttl_seconds;
  optional binary exported_at (STRING);
  optional int32 partition_id;
}
```

### Data Type Representations

Different Redis data types are stored in the unified schema as follows:

#### Strings
- **key**: Original Redis key (e.g., `"user:123"`)
- **type**: `"string"`
- **value**: The actual string value

#### Hashes
- **key**: `"{original_key}:field:{field_name}"` (e.g., `"user:123:field:email"`)
- **type**: `"hash_field"`
- **value**: The field's value

#### Sets
- **key**: `"{original_key}:member:{member_value}"` (e.g., `"tags:member:golang"`)
- **type**: `"set_member"`
- **value**: The member value

#### Sorted Sets (ZSets)
- **key**: `"{original_key}:member:{member_value}"` (e.g., `"leaderboard:member:player1"`)
- **type**: `"zset_member"`
- **value**: `"score={score},rank={rank}"` (e.g., `"score=95.5,rank=0"`)

#### Lists
- **key**: `"{original_key}:index:{index}"` (e.g., `"queue:index:0"`)
- **type**: `"list_item"`
- **value**: The item value

## Querying with DuckDB

### Basic Queries

Query all exported data:
```sql
-- For Parquet files
SELECT * FROM read_parquet('output/**/*.parquet');

-- For CSV files
SELECT * FROM read_csv('output/**/*.csv');
```

Count by data type:
```sql
SELECT type, COUNT(*) as count 
FROM read_parquet('output/**/*.parquet')
GROUP BY type
ORDER BY count DESC;
```

### Querying String Keys

Find all string values:
```sql
SELECT key, value, ttl_seconds 
FROM read_parquet('output/**/*.parquet')
WHERE type = 'string'
LIMIT 10;
```

### Querying Hash Fields

Get all fields for a specific hash:
```sql
-- Extract the original key and field name
SELECT 
    SPLIT_PART(key, ':field:', 1) as hash_key,
    SPLIT_PART(key, ':field:', 2) as field_name,
    value
FROM read_parquet('output/**/*.parquet')
WHERE type = 'hash_field'
  AND key LIKE 'user:123:field:%'
ORDER BY field_name;
```

Reconstruct hash objects:
```sql
-- Group hash fields into JSON objects
SELECT 
    SPLIT_PART(key, ':field:', 1) as hash_key,
    MAP_FROM_ENTRIES(
        ARRAY_AGG(
            ROW(
                SPLIT_PART(key, ':field:', 2),
                value
            )
        )
    ) as fields
FROM read_parquet('output/**/*.parquet')
WHERE type = 'hash_field'
GROUP BY SPLIT_PART(key, ':field:', 1)
LIMIT 5;
```

### Querying Sets

Get all members of a specific set:
```sql
SELECT 
    SPLIT_PART(key, ':member:', 1) as set_key,
    value as member
FROM read_parquet('output/**/*.parquet')
WHERE type = 'set_member'
  AND key LIKE 'tags:member:%'
ORDER BY member;
```

Count members per set:
```sql
SELECT 
    SPLIT_PART(key, ':member:', 1) as set_key,
    COUNT(*) as member_count
FROM read_parquet('output/**/*.parquet')
WHERE type = 'set_member'
GROUP BY SPLIT_PART(key, ':member:', 1)
ORDER BY member_count DESC;
```

Find sets containing a specific member:
```sql
SELECT DISTINCT SPLIT_PART(key, ':member:', 1) as set_key
FROM read_parquet('output/**/*.parquet')
WHERE type = 'set_member'
  AND value = 'golang';
```

### Querying Sorted Sets

Get leaderboard with scores:
```sql
SELECT 
    SPLIT_PART(key, ':member:', 1) as zset_key,
    SPLIT_PART(key, ':member:', 2) as member,
    CAST(SPLIT_PART(SPLIT_PART(value, 'score=', 2), ',', 1) AS DOUBLE) as score,
    CAST(SPLIT_PART(value, 'rank=', 2) AS INTEGER) as rank
FROM read_parquet('output/**/*.parquet')
WHERE type = 'zset_member'
  AND key LIKE 'leaderboard:%'
ORDER BY score DESC;
```

### Querying Lists

Get list items in order:
```sql
SELECT 
    SPLIT_PART(key, ':index:', 1) as list_key,
    CAST(SPLIT_PART(key, ':index:', 2) AS INTEGER) as index,
    value
FROM read_parquet('output/**/*.parquet')
WHERE type = 'list_item'
  AND key LIKE 'queue:%'
ORDER BY list_key, index;
```

### Advanced Queries

Find keys expiring soon:
```sql
SELECT key, type, ttl_seconds, 
    ttl_seconds / 3600.0 as hours_remaining
FROM read_parquet('output/**/*.parquet')
WHERE ttl_seconds > 0 
  AND ttl_seconds < 3600  -- Expiring within 1 hour
ORDER BY ttl_seconds;
```

Analyze data distribution by partition:
```sql
SELECT 
    partition_id,
    COUNT(*) as record_count,
    COUNT(DISTINCT SPLIT_PART(key, ':', 1)) as unique_key_prefixes
FROM read_parquet('output/**/*.parquet')
GROUP BY partition_id
ORDER BY partition_id;
```

Export query results:
```sql
-- Export filtered data to a new Parquet file
COPY (
    SELECT * FROM read_parquet('output/**/*.parquet')
    WHERE type = 'hash_field' AND key LIKE 'user:%'
) TO 'user_hashes.parquet' (FORMAT 'parquet');
```

## Setting Up DuckDB

### Installation

Install DuckDB CLI:

**macOS:**
```bash
brew install duckdb
```

**Linux:**
```bash
wget https://github.com/duckdb/duckdb/releases/download/v0.10.0/duckdb_cli-linux-amd64.zip
unzip duckdb_cli-linux-amd64.zip
chmod +x duckdb
sudo mv duckdb /usr/local/bin/
```

**Windows:**
Download from [DuckDB releases](https://github.com/duckdb/duckdb/releases)

### Creating Views for Easy Querying

Start DuckDB and create persistent views:

```bash
# Start DuckDB with a persistent database
duckdb redis_export.db
```

In the DuckDB shell:

```sql
-- Create a view for all Redis data
CREATE VIEW redis_data AS 
SELECT * FROM read_parquet('./output/**/*.parquet');

-- Create views for each data type
CREATE VIEW redis_strings AS 
SELECT * FROM redis_data WHERE type = 'string';

CREATE VIEW redis_hashes AS 
SELECT 
    SPLIT_PART(key, ':field:', 1) as hash_key,
    SPLIT_PART(key, ':field:', 2) as field_name,
    value,
    ttl_seconds,
    exported_at
FROM redis_data 
WHERE type = 'hash_field';

CREATE VIEW redis_sets AS 
SELECT 
    SPLIT_PART(key, ':member:', 1) as set_key,
    value as member,
    ttl_seconds,
    exported_at
FROM redis_data 
WHERE type = 'set_member';

CREATE VIEW redis_zsets AS 
SELECT 
    SPLIT_PART(key, ':member:', 1) as zset_key,
    SPLIT_PART(key, ':member:', 2) as member,
    CAST(SPLIT_PART(SPLIT_PART(value, 'score=', 2), ',', 1) AS DOUBLE) as score,
    CAST(SPLIT_PART(value, 'rank=', 2) AS INTEGER) as rank,
    ttl_seconds,
    exported_at
FROM redis_data 
WHERE type = 'zset_member';

CREATE VIEW redis_lists AS 
SELECT 
    SPLIT_PART(key, ':index:', 1) as list_key,
    CAST(SPLIT_PART(key, ':index:', 2) AS INTEGER) as index,
    value,
    ttl_seconds,
    exported_at
FROM redis_data 
WHERE type = 'list_item';

-- Show available views
SHOW TABLES;
```

### Using the Views

Now you can query Redis data more easily:

```sql
-- Count records by type
SELECT COUNT(*) FROM redis_strings;
SELECT COUNT(*) FROM redis_hashes;
SELECT COUNT(*) FROM redis_sets;

-- Query specific hash
SELECT * FROM redis_hashes WHERE hash_key = 'user:123';

-- Find all sets containing a member
SELECT set_key FROM redis_sets WHERE member = 'golang';

-- Get top 10 from leaderboard
SELECT * FROM redis_zsets 
WHERE zset_key = 'leaderboard' 
ORDER BY score DESC 
LIMIT 10;
```

### Performance Tips

1. **Use Parquet format** - It's columnar and compressed, making queries much faster than CSV
2. **Partition your queries** - Use the partition_id or date filters when possible
3. **Create indexes** for frequently queried columns:
   ```sql
   CREATE INDEX idx_key_prefix ON redis_data (SPLIT_PART(key, ':', 1));
   ```
4. **Use EXPLAIN** to understand query plans:
   ```sql
   EXPLAIN SELECT * FROM redis_sets WHERE member = 'test';
   ```

## Running Locally

### Quick Start with Docker Compose

The easiest way to test Redis Dumper is using the included docker-compose setup with test data:

```bash
# Start Redis with test data
docker-compose up -d

# Wait for data to load
sleep 5

# Run the dumper on test data
go run ./cmd/dumper keys-only

# Or export full data for specific patterns
go run ./cmd/dumper pattern "user:*"
go run ./cmd/dumper pattern "product:*"

# Check the output
ls -la ./output/
```

The test data includes:
- String values (configs, sessions, cached pages)
- Hashes (user profiles, products, orders)
- Sets (tags, categories, user skills)
- Sorted sets (leaderboards, trending items, view counts)
- Lists (queues, logs, user history)
- Keys with TTLs
- Complex JSON structures

### Manual Docker Setup

Alternatively, you can manually start Redis:

```bash
# Start a Redis instance
docker run -d --name redis-local -p 6379:6379 redis:latest

# Add some test data
docker exec -it redis-local redis-cli SET mykey "Hello World"
docker exec -it redis-local redis-cli HSET user:123 name "Alice" email "alice@example.com"
docker exec -it redis-local redis-cli SADD fruits "apple" "banana" "orange"

# Run the dumper
go run ./cmd/dumper keys-only
```

### Full Export Example

```bash
export REDIS_URL=redis://localhost:6379
export OUTPUT_DIR=./local-export
export OUTPUT_FORMAT=parquet
export BATCH_SIZE=5000

# Export full data for all keys
go run ./cmd/dumper full

# Or export data matching a pattern
go run ./cmd/dumper pattern "user:*"
```

### Testing with DuckDB

After exporting, you can query the data using DuckDB:

```bash
# Install DuckDB if you haven't already
# macOS: brew install duckdb
# Linux: wget https://github.com/duckdb/duckdb/releases/download/v0.9.2/duckdb_cli-linux-amd64.zip

# Query CSV exports
duckdb -c "SELECT * FROM read_csv('./local-export/**/*.csv');"

# Query Parquet exports
duckdb -c "SELECT * FROM read_parquet('./local-export/**/*.parquet');"

# More complex queries
duckdb -c "SELECT type, COUNT(*) as count FROM read_parquet('./local-export/**/*.parquet') GROUP BY type;"
```

## Development

### Requirements

- Go 1.21 or higher
- Docker (for running tests with make commands)
- Redis (for local testing)
- Make

### Building

```bash
make build
```

### Testing

```bash
make go-test
```

### Linting

```bash
make go-lint
```

### Formatting

```bash
make go-fmt
```

## Contributing

We welcome contributions! Please see our [Contributing Guide](CONTRIBUTING.md) for details on:

- Setting up your development environment
- Running tests and linting
- Submitting pull requests
- Reporting issues

## License

MIT License - see LICENSE file for details.