package exporter

import (
	"context"
	"crypto/tls"
	"fmt"
	"github.com/go-redis/redis/v8"
	"log"
	"os"
	"time"
)

type RedisExporterOptions struct {
	RedisURL          string
	OutputDir         string
	BatchSize         int
	EnableTLS         bool
	SkipTLSVerify     bool
	OutputFormat      string
	MaxRecordsPerFile int64
}

type PartitionInfo struct {
	PartitionID   int       `json:"partition_id"`
	DataType      string    `json:"data_type"`
	FileName      string    `json:"file_name"`
	RecordCount   int64     `json:"record_count"`
	FileSizeBytes int64     `json:"file_size_bytes"`
	StartTime     time.Time `json:"start_time"`
	EndTime       time.Time `json:"end_time"`
}

type ExportMetadata struct {
	ExportID   string          `json:"export_id"`
	Pattern    string          `json:"pattern"`
	StartTime  time.Time       `json:"start_time"`
	EndTime    time.Time       `json:"end_time"`
	TotalKeys  int64           `json:"total_keys"`
	Partitions []PartitionInfo `json:"partitions"`
}

type RedisExporter struct {
	client        *redis.Client
	fileManager   *FileManager
	ctx           context.Context
	batchSize     int
	flushInterval int
}

func NewRedisExporter(opts RedisExporterOptions) (Exporter, error) {
	// Parse Redis connection
	opt, err := redis.ParseURL(opts.RedisURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Redis URL: %w", err)
	}

	// Optimize Redis client for large datasets
	opt.PoolSize = 10
	opt.MinIdleConns = 5
	opt.MaxRetries = 3
	opt.DialTimeout = time.Second * 5
	opt.ReadTimeout = time.Second * 30
	opt.WriteTimeout = time.Second * 30

	// Configure TLS if needed
	if opts.EnableTLS {
		tlsConfig := &tls.Config{
			InsecureSkipVerify: opts.SkipTLSVerify,
		}

		// If the URL scheme is rediss://, it should already enable TLS
		// But we can force it here too
		opt.TLSConfig = tlsConfig

		fmt.Printf("TLS enabled (InsecureSkipVerify: %v)\n", opts.SkipTLSVerify)
	}

	client := redis.NewClient(opt)

	// Test connection
	ctx := context.Background()
	_, err = client.Ping(ctx).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	// Create output directory
	if err := os.MkdirAll(opts.OutputDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}

	// Determine output format
	var format OutputFormat
	switch opts.OutputFormat {
	case "parquet":
		format = FormatParquet
	case "csv", "":
		format = FormatCSV
	default:
		return nil, fmt.Errorf("unsupported output format: %s", opts.OutputFormat)
	}

	// Create file manager
	storageConfig := StorageConfig{
		OutputDir:  opts.OutputDir,
		Format:     format,
		MaxRecords: opts.MaxRecordsPerFile,
	}
	fileManager := NewFileManager(storageConfig)

	return &RedisExporter{
		client:        client,
		fileManager:   fileManager,
		ctx:           ctx,
		batchSize:     opts.BatchSize,
		flushInterval: 1000,
	}, nil
}

func (re *RedisExporter) Close() error {
	if err := re.fileManager.Close(); err != nil {
		log.Printf("Error closing file manager: %v", err)
	}
	return re.client.Close()
}

// ExportKeysOnly - Memory-efficient export of just key metadata
func (re *RedisExporter) ExportKeysOnly() error {
	defer func() {
		_ = re.Close()
	}()

	var cursor uint64
	var keys []string
	var err error
	count := 0

	fmt.Println("Starting Redis key metadata export (keys only)...")

	for {
		// Use smaller scan batches for memory efficiency
		keys, cursor, err = re.client.Scan(re.ctx, cursor, "*", int64(re.batchSize)).Result()
		if err != nil {
			return fmt.Errorf("failed to scan keys: %w", err)
		}

		// Process keys in a batch with a pipeline for efficiency
		pipe := re.client.Pipeline()
		keyTypes := make(map[string]*redis.StatusCmd)
		keyTTLs := make(map[string]*redis.DurationCmd)

		// Build pipeline commands
		for _, key := range keys {
			keyTypes[key] = pipe.Type(re.ctx, key)
			keyTTLs[key] = pipe.TTL(re.ctx, key)
		}

		// Execute pipeline
		_, err = pipe.Exec(re.ctx)
		if err != nil {
			log.Printf("Pipeline error: %v", err)
			continue
		}

		// Process results
		timestamp := time.Now().UTC().Format(time.RFC3339)
		for _, key := range keys {
			keyType, err := keyTypes[key].Result()
			if err != nil {
				log.Printf("Error getting type for key %s: %v", key, err)
				continue
			}

			ttl, err := keyTTLs[key].Result()
			if err != nil {
				log.Printf("Error getting TTL for key %s: %v", key, err)
				continue
			}

			ttlSeconds := int64(-1)
			if ttl > 0 {
				ttlSeconds = int64(ttl.Seconds())
			}

			// Estimate size without fetching data
			sizeEstimate := re.estimateKeySize(key, keyType)

			record := &RedisRecord{
				Key:        key,
				Type:       keyType,
				Value:      fmt.Sprintf("size_estimate=%d", sizeEstimate),
				TTLSeconds: ttlSeconds,
				ExportedAt: timestamp,
			}

			if err := re.fileManager.WriteRecord(record); err != nil {
				log.Printf("Error writing key %s: %v", key, err)
				continue
			}

			count++
		}

		// Flush periodically
		if count%re.flushInterval == 0 {
			fmt.Printf("Exported %d keys...\n", count)
			re.flushAll()
		}

		// Break when the cursor returns to 0
		if cursor == 0 {
			break
		}
	}

	fmt.Printf("Key export completed! Total keys exported: %d\n", count)
	return nil
}

// estimateKeySize provides rough size estimates without fetching data
func (re *RedisExporter) estimateKeySize(key, keyType string) int64 {
	switch keyType {
	case "string":
		// For strings, we'd need to fetch to get an accurate size
		// Return key length as an estimate
		return int64(len(key))
	case "set", "list", "hash", "zset":
		// Use key length as base estimate - not accurate but avoids memory issues
		return int64(len(key) * 10) // Rough multiplier
	default:
		return int64(len(key))
	}
}

// ExportKeysOnlyByPattern - Memory-efficient export with pattern matching
func (re *RedisExporter) ExportKeysOnlyByPattern(pattern string) error {
	defer func() {
		_ = re.Close()
	}()

	var cursor uint64
	var keys []string
	var err error
	count := 0

	fmt.Printf("Starting Redis key metadata export with pattern: %s\n", pattern)

	for {
		keys, cursor, err = re.client.Scan(re.ctx, cursor, pattern, int64(re.batchSize)).Result()
		if err != nil {
			return fmt.Errorf("failed to scan keys: %w", err)
		}

		// Use pipeline for efficiency
		pipe := re.client.Pipeline()
		keyTypes := make(map[string]*redis.StatusCmd)
		keyTTLs := make(map[string]*redis.DurationCmd)

		for _, key := range keys {
			keyTypes[key] = pipe.Type(re.ctx, key)
			keyTTLs[key] = pipe.TTL(re.ctx, key)
		}

		_, err = pipe.Exec(re.ctx)
		if err != nil {
			log.Printf("Pipeline error: %v", err)
			continue
		}

		timestamp := time.Now().UTC().Format(time.RFC3339)
		for _, key := range keys {
			keyType, err := keyTypes[key].Result()
			if err != nil {
				continue
			}

			ttl, err := keyTTLs[key].Result()
			if err != nil {
				continue
			}

			ttlSeconds := int64(-1)
			if ttl > 0 {
				ttlSeconds = int64(ttl.Seconds())
			}

			sizeEstimate := re.estimateKeySize(key, keyType)

			record := &RedisRecord{
				Key:        key,
				Type:       keyType,
				Value:      fmt.Sprintf("size_estimate=%d", sizeEstimate),
				TTLSeconds: ttlSeconds,
				ExportedAt: timestamp,
			}

			_ = re.fileManager.WriteRecord(record)
			count++
		}

		if count%re.flushInterval == 0 {
			fmt.Printf("Exported %d keys...\n", count)
			re.flushAll()
		}

		if cursor == 0 {
			break
		}
	}

	fmt.Printf("Export completed! Total keys exported: %d\n", count)
	return nil
}

// ExportByPattern - Export full data for all keys matching pattern
func (re *RedisExporter) ExportByPattern(pattern string) error {
	defer func() {
		_ = re.Close()
	}()

	var cursor uint64
	var keys []string
	var err error
	count := 0

	// Update metadata with pattern
	re.fileManager.SetMetadata(pattern, 0)

	fmt.Printf("Starting full data export with pattern: %s\n", pattern)

	// Export full data for all keys matching pattern
	for {
		keys, cursor, err = re.client.Scan(re.ctx, cursor, pattern, int64(re.batchSize)).Result()
		if err != nil {
			return fmt.Errorf("failed to scan keys: %w", err)
		}

		// Export full data for each key in batch
		for _, key := range keys {
			if err := re.exportKey(key); err != nil {
				log.Printf("Error exporting key %s: %v", key, err)
				continue
			}
			count++

			if count%100 == 0 {
				fmt.Printf("Exported %d keys...\n", count)
				re.flushAll()
			}
		}

		if cursor == 0 {
			break
		}
	}

	// Update final metadata
	re.fileManager.SetMetadata(pattern, int64(count))

	fmt.Printf("Export completed! Total keys exported with full data: %d\n", count)
	fmt.Printf("Files created with %s format\n", re.fileManager.config.Format)
	fmt.Println("Using Hive-style partitioning for optimal DuckDB querying")

	// Print DuckDB query example
	queryPath := re.fileManager.GetQueryPath()
	fmt.Printf("DuckDB query: SELECT * FROM read_%s('%s');\n",
		string(re.fileManager.config.Format), queryPath)
	fmt.Printf("Example filter: SELECT * FROM read_%s('%s') WHERE type = 'string';\n",
		string(re.fileManager.config.Format), queryPath)
	return nil
}

func (re *RedisExporter) flushAll() {
	re.fileManager.FlushAll()
}

func (re *RedisExporter) exportKey(key string) error {
	// Get key type
	keyType, err := re.client.Type(re.ctx, key).Result()
	if err != nil {
		return fmt.Errorf("failed to get type for key %s: %w", key, err)
	}

	// Get TTL
	ttl, err := re.client.TTL(re.ctx, key).Result()
	if err != nil {
		return fmt.Errorf("failed to get TTL for key %s: %w", key, err)
	}

	ttlSeconds := int64(-1)
	if ttl > 0 {
		ttlSeconds = int64(ttl.Seconds())
	}

	// Get size and export detailed data
	size, err := re.exportKeyData(key, keyType)
	if err != nil {
		return fmt.Errorf("failed to export data for key %s: %w", key, err)
	}

	// Write key metadata
	timestamp := time.Now().UTC().Format(time.RFC3339)
	keyRecord := &RedisRecord{
		Key:        key,
		Type:       keyType,
		Value:      fmt.Sprintf("size=%d", size),
		TTLSeconds: ttlSeconds,
		ExportedAt: timestamp,
	}

	return re.fileManager.WriteRecord(keyRecord)
}

func (re *RedisExporter) exportKeyData(key, keyType string) (int64, error) {
	timestamp := time.Now().UTC().Format(time.RFC3339)

	switch keyType {
	case "string":
		val, err := re.client.Get(re.ctx, key).Result()
		if err != nil {
			return 0, err
		}
		return int64(len(val)), nil

	case "set":
		// Use SSCAN for memory efficiency on large sets
		var cursor uint64
		totalSize := int64(0)

		for {
			members, nextCursor, err := re.client.SScan(re.ctx, key, cursor, "*", 1000).Result()
			if err != nil {
				return 0, err
			}

			for _, member := range members {
				record := &RedisRecord{
					Key:        fmt.Sprintf("%s:member:%s", key, member),
					Type:       "set_member",
					Value:      member,
					TTLSeconds: -1,
					ExportedAt: timestamp,
				}
				if err := re.fileManager.WriteRecord(record); err != nil {
					return 0, err
				}
				totalSize += int64(len(member))
			}

			cursor = nextCursor
			if cursor == 0 {
				break
			}
		}
		return totalSize, nil

	case "hash":
		// Use HSCAN for memory efficiency on large hashes
		var cursor uint64
		totalSize := int64(0)

		for {
			fields, nextCursor, err := re.client.HScan(re.ctx, key, cursor, "*", 1000).Result()
			if err != nil {
				return 0, err
			}

			// HScan returns field-value pairs in alternating positions
			for i := 0; i < len(fields); i += 2 {
				if i+1 < len(fields) {
					field := fields[i]
					value := fields[i+1]
					record := &RedisRecord{
						Key:        fmt.Sprintf("%s:field:%s", key, field),
						Type:       "hash_field",
						Value:      value,
						TTLSeconds: -1,
						ExportedAt: timestamp,
					}
					if err := re.fileManager.WriteRecord(record); err != nil {
						return 0, err
					}
					totalSize += int64(len(field) + len(value))
				}
			}

			cursor = nextCursor
			if cursor == 0 {
				break
			}
		}
		return totalSize, nil

	case "zset":
		// Use ZSCAN for memory efficiency
		var cursor uint64
		totalSize := int64(0)
		rank := 0

		for {
			members, nextCursor, err := re.client.ZScan(re.ctx, key, cursor, "*", 1000).Result()
			if err != nil {
				return 0, err
			}

			// ZSCAN returns member-score pairs in alternating positions
			for i := 0; i < len(members); i += 2 {
				if i+1 < len(members) {
					member := members[i]
					scoreStr := members[i+1]
					record := &RedisRecord{
						Key:        fmt.Sprintf("%s:member:%s", key, member),
						Type:       "zset_member",
						Value:      fmt.Sprintf("score=%s,rank=%d", scoreStr, rank),
						TTLSeconds: -1,
						ExportedAt: timestamp,
					}
					if err := re.fileManager.WriteRecord(record); err != nil {
						return 0, err
					}
					totalSize += int64(len(member))
					rank++
				}
			}

			cursor = nextCursor
			if cursor == 0 {
				break
			}
		}
		return totalSize, nil

	case "list":
		// For lists, we need to be careful with very large lists
		length, err := re.client.LLen(re.ctx, key).Result()
		if err != nil {
			return 0, err
		}

		// Process in chunks to avoid memory issues
		const chunkSize = 1000
		totalSize := int64(0)

		for start := int64(0); start < length; start += chunkSize {
			end := start + chunkSize - 1
			if end >= length {
				end = length - 1
			}

			values, err := re.client.LRange(re.ctx, key, start, end).Result()
			if err != nil {
				return 0, err
			}

			for i, value := range values {
				record := &RedisRecord{
					Key:        fmt.Sprintf("%s:index:%d", key, start+int64(i)),
					Type:       "list_item",
					Value:      value,
					TTLSeconds: -1,
					ExportedAt: timestamp,
				}
				if err := re.fileManager.WriteRecord(record); err != nil {
					return 0, err
				}
				totalSize += int64(len(value))
			}
		}
		return totalSize, nil

	default:
		return 0, nil
	}
}
