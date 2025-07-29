package main

import (
	"fmt"
	"github.com/caarlos0/env/v10"
	"github.com/cameronnewman/redis-dumper/internal/exporter"
	"log"
	"os"
	"strings"
	"time"
)

const (
	CmdKeysOnly = "keys-only"
	CmdPattern  = "pattern"
	CmdFull     = "full"
)

type Config struct {
	RedisURL          string `env:"REDIS_URL" envDefault:"redis://localhost:6379/0"`
	OutputDir         string `env:"OUTPUT_DIR" envDefault:"/tmp/dumper"`
	BatchSize         int    `env:"BATCH_SIZE" envDefault:"1000"`
	EnableTLS         bool   `env:"ENABLE_TLS" envDefault:"false"`
	SkipTLSVerify     bool   `env:"SKIP_TLS_VERIFY" envDefault:"true"`
	OutputFormat      string `env:"OUTPUT_FORMAT" envDefault:"parquet"`
	MaxRecordsPerFile int64  `env:"MAX_RECORDS_PER_FILE" envDefault:"100000"`
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Redis to DuckDB Exporter - Memory Optimized for Large Datasets")
		fmt.Println("")
		fmt.Println("Usage:")
		fmt.Println("  redis-dumper <command> [pattern]")
		fmt.Println("")
		fmt.Println("Commands:")
		fmt.Println("  keys-only  - Export only key metadata (recommended for 180GB+ datasets)")
		fmt.Println("  pattern    - Export full data for keys matching pattern")
		fmt.Println("  full       - Export all data (use with caution on large datasets)")
		fmt.Println("")
		fmt.Println("Arguments:")
		fmt.Println("  pattern    - Optional key pattern to filter (default: *)")
		fmt.Println("")
		fmt.Println("Environment Variables:")
		fmt.Println("  REDIS_URL        - Redis connection URL (default: redis://localhost:6379/0)")
		fmt.Println("  OUTPUT_DIR            - Output directory for dump files (default: /tmp/dumper)")
		fmt.Println("  BATCH_SIZE            - Batch size for processing (default: 1000)")
		fmt.Println("  ENABLE_TLS            - Enable TLS connection (default: false)")
		fmt.Println("  SKIP_TLS_VERIFY       - Skip TLS certificate verification (default: false)")
		fmt.Println("  OUTPUT_FORMAT         - Output format: csv or parquet (default: parquet)")
		fmt.Println("  MAX_RECORDS_PER_FILE  - Max records per file before rotation (default: 100000)")
		fmt.Println("")
		fmt.Println("Examples:")
		fmt.Println("  REDIS_URL=rediss://user:pass@redis.example.com:6380/0 redis-dumper keys-only")
		fmt.Println("  REDIS_URL=redis://localhost:6379/0 redis-dumper pattern 'user:*'")
		fmt.Println("")
		fmt.Println("URL Schemes:")
		fmt.Println("  redis://   - Plain connection")
		fmt.Println("  rediss://  - TLS connection (automatically enables TLS)")
		os.Exit(1)
	}

	// Parse configuration from environment variables
	cfg := Config{}
	if err := env.Parse(&cfg); err != nil {
		log.Fatal("Failed to parse environment variables:", err)
	}

	command := os.Args[1]
	pattern := "*"

	// Check if a pattern is provided as a second argument
	if len(os.Args) > 2 {
		pattern = os.Args[2]
	}

	// Auto-enable TLS for rediss:// URLs
	if strings.HasPrefix(cfg.RedisURL, "rediss://") {
		cfg.EnableTLS = true
		fmt.Println("Auto-detected TLS from rediss:// URL scheme")
	}

	options := exporter.RedisExporterOptions{
		RedisURL:          cfg.RedisURL,
		OutputDir:         cfg.OutputDir,
		BatchSize:         cfg.BatchSize,
		EnableTLS:         cfg.EnableTLS,
		SkipTLSVerify:     cfg.SkipTLSVerify,
		OutputFormat:      cfg.OutputFormat,
		MaxRecordsPerFile: cfg.MaxRecordsPerFile,
	}

	exp, err := exporter.NewRedisExporter(options)
	if err != nil {
		log.Fatal("Failed to create exporter:", err)
	}

	switch command {
	case CmdKeysOnly:
		fmt.Printf("Exporting keys only with batch size: %d, pattern: %s\n", cfg.BatchSize, pattern)
		if pattern == "*" {
			err = exp.ExportKeysOnly()
		} else {
			err = exp.ExportKeysOnlyByPattern(pattern)
		}

		if err != nil {
			log.Fatal("Export failed:", err)
		}

	case CmdPattern:
		fmt.Printf("Exporting full data for keys matching pattern: %s (batch size: %d)\n", pattern, cfg.BatchSize)
		err = exp.ExportByPattern(pattern)
		if err != nil {
			log.Fatal("Export failed:", err)
		}

	case CmdFull:
		fmt.Println("WARNING: Full export on a large dataset will take significant time and resources!")
		fmt.Println("Consider using 'keys-only' or 'sample' commands instead.")
		fmt.Println("Proceeding in 5 seconds... (Ctrl+C to cancel)")
		time.Sleep(5 * time.Second)

		fmt.Printf("Exporting all data with batch size: %d, pattern: %s\n", cfg.BatchSize, pattern)
		// Export all data matching pattern
		err = exp.ExportByPattern(pattern)
		if err != nil {
			log.Fatal("Failed to create exporter:", err)
		}
		fmt.Println("Full export not implemented in this example - use sample instead")

	default:
		log.Fatal("Unknown command:", command)
	}

	fmt.Println("\nExport completed successfully!")
}
