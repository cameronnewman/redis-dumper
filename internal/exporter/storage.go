package exporter

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	_ "github.com/marcboeker/go-duckdb"
)

// OutputFormat represents the file format for exports
type OutputFormat string

const (
	FormatCSV     OutputFormat = "csv"
	FormatParquet OutputFormat = "parquet"
)

// RedisRecord represents the unified schema for all Redis data
type RedisRecord struct {
	Key        string
	Type       string
	Value      string
	TTLSeconds int64
	ExportedAt string
}

// HivePartition represents a Hive-style partition structure
type HivePartition struct {
	DataType    string    `json:"data_type"`
	Year        string    `json:"year"`
	Month       string    `json:"month"`
	Day         string    `json:"day"`
	Hour        string    `json:"hour"`
	PartitionID int       `json:"partition_id"`
	CreatedAt   time.Time `json:"created_at"`
}

// StorageConfig holds configuration for storage operations
type StorageConfig struct {
	OutputDir  string
	Format     OutputFormat
	MaxRecords int64
}

// FileManager handles all file operations for the exporter using DuckDB
type FileManager struct {
	config               StorageConfig
	db                   *sql.DB
	tableName            string
	recordCount          int64
	partitionID          int
	metadata             *ExportMetadata
	currentPartitionPath string
	csvWriter            *csv.Writer
	csvFile              *os.File
}

// NewFileManager creates a new file manager instance
func NewFileManager(config StorageConfig) *FileManager {
	return &FileManager{
		config:      config,
		tableName:   "redis_data",
		recordCount: 0,
		partitionID: 0,
		metadata: &ExportMetadata{
			ExportID:   fmt.Sprintf("export_%d", time.Now().Unix()),
			StartTime:  time.Now(),
			Partitions: make([]PartitionInfo, 0),
		},
	}
}

// CreateHivePartitionPath creates a Hive-style partition path
func (fm *FileManager) CreateHivePartitionPath(timestamp time.Time) string {
	year := timestamp.Format("2006")
	month := timestamp.Format("01")
	day := timestamp.Format("02")
	hour := timestamp.Format("15")

	return filepath.Join(
		fm.config.OutputDir,
		fmt.Sprintf("year=%s", year),
		fmt.Sprintf("month=%s", month),
		fmt.Sprintf("day=%s", day),
		fmt.Sprintf("hour=%s", hour),
	)
}

// initializeWriter initializes the appropriate writer based on format
func (fm *FileManager) initializeWriter() error {
	now := time.Now()
	fm.partitionID++

	// Create partition path
	partitionPath := fm.CreateHivePartitionPath(now)
	if err := os.MkdirAll(partitionPath, 0755); err != nil {
		return fmt.Errorf("failed to create partition directory: %w", err)
	}

	fm.currentPartitionPath = partitionPath

	switch fm.config.Format {
	case FormatCSV:
		return fm.initializeCSVWriter(partitionPath)
	case FormatParquet:
		return fm.initializeDuckDBWriter(partitionPath)
	default:
		return fmt.Errorf("unsupported format: %s", fm.config.Format)
	}
}

// initializeCSVWriter sets up CSV writing
func (fm *FileManager) initializeCSVWriter(partitionPath string) error {
	fileName := fmt.Sprintf("redis_data_part_%04d.csv", fm.partitionID)
	filePath := filepath.Join(partitionPath, fileName)

	file, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to create CSV file: %w", err)
	}

	fm.csvFile = file
	fm.csvWriter = csv.NewWriter(file)

	// Write headers
	headers := []string{"key", "type", "value", "ttl_seconds", "exported_at", "partition_id"}
	if err := fm.csvWriter.Write(headers); err != nil {
		return fmt.Errorf("failed to write CSV headers: %w", err)
	}

	return nil
}

// initializeDuckDBWriter sets up DuckDB for Parquet writing
func (fm *FileManager) initializeDuckDBWriter(partitionPath string) error {
	// Create DuckDB connection
	db, err := sql.Open("duckdb", "")
	if err != nil {
		return fmt.Errorf("failed to open DuckDB connection: %w", err)
	}

	fm.db = db

	// Create table for this partition
	createTableSQL := fmt.Sprintf(`
		CREATE TABLE %s (
			key VARCHAR,
			type VARCHAR,
			value VARCHAR,
			ttl_seconds BIGINT,
			exported_at VARCHAR,
			partition_id INTEGER
		)`, fm.tableName)

	if _, err := fm.db.Exec(createTableSQL); err != nil {
		return fmt.Errorf("failed to create table: %w", err)
	}

	return nil
}

// WriteRecord writes a RedisRecord to the writer
func (fm *FileManager) WriteRecord(record *RedisRecord) error {
	// Initialize writer if not already done
	if fm.csvWriter == nil && fm.db == nil {
		if err := fm.initializeWriter(); err != nil {
			return err
		}
	}

	// Check if we need to rotate
	if fm.recordCount >= fm.config.MaxRecords {
		if err := fm.RotateWriter(); err != nil {
			return err
		}
		// After rotation, reinitialize writer
		if err := fm.initializeWriter(); err != nil {
			return err
		}
	}

	switch fm.config.Format {
	case FormatCSV:
		return fm.writeCSVRecord(record)
	case FormatParquet:
		return fm.writeDuckDBRecord(record)
	default:
		return fmt.Errorf("unsupported format: %s", fm.config.Format)
	}
}

// writeCSVRecord writes to CSV
func (fm *FileManager) writeCSVRecord(record *RedisRecord) error {
	row := []string{
		record.Key,
		record.Type,
		record.Value,
		strconv.FormatInt(record.TTLSeconds, 10),
		record.ExportedAt,
		strconv.Itoa(fm.partitionID),
	}

	if err := fm.csvWriter.Write(row); err != nil {
		return fmt.Errorf("failed to write CSV record: %w", err)
	}

	fm.recordCount++
	return nil
}

// writeDuckDBRecord writes to DuckDB table
func (fm *FileManager) writeDuckDBRecord(record *RedisRecord) error {
	insertSQL := fmt.Sprintf(`
		INSERT INTO %s (key, type, value, ttl_seconds, exported_at, partition_id)
		VALUES (?, ?, ?, ?, ?, ?)`, fm.tableName)

	_, err := fm.db.Exec(insertSQL,
		record.Key,
		record.Type,
		record.Value,
		record.TTLSeconds,
		record.ExportedAt,
		fm.partitionID)

	if err != nil {
		return fmt.Errorf("failed to insert record: %w", err)
	}

	fm.recordCount++
	return nil
}

// RotateWriter closes current writer and creates a new partition
func (fm *FileManager) RotateWriter() error {
	if fm.recordCount == 0 {
		return nil // Nothing to rotate
	}

	switch fm.config.Format {
	case FormatCSV:
		return fm.rotateCSVWriter()
	case FormatParquet:
		return fm.rotateDuckDBWriter()
	default:
		return fmt.Errorf("unsupported format: %s", fm.config.Format)
	}
}

// rotateCSVWriter handles CSV rotation
func (fm *FileManager) rotateCSVWriter() error {
	if fm.csvWriter != nil {
		fm.csvWriter.Flush()
	}

	if fm.csvFile != nil {
		stat, err := fm.csvFile.Stat()
		if err != nil {
			return err
		}

		// Add partition info
		partitionInfo := PartitionInfo{
			PartitionID:   fm.partitionID,
			DataType:      "redis_data",
			FileName:      filepath.Base(fm.csvFile.Name()),
			RecordCount:   fm.recordCount,
			FileSizeBytes: stat.Size(),
			StartTime:     time.Now().Add(-time.Hour), // Approximate
			EndTime:       time.Now(),
		}
		fm.metadata.Partitions = append(fm.metadata.Partitions, partitionInfo)

		if err := fm.csvFile.Close(); err != nil {
			return fmt.Errorf("failed to close CSV file: %w", err)
		}
		fm.csvFile = nil
		fm.csvWriter = nil
	}

	fm.recordCount = 0
	return nil
}

// rotateDuckDBWriter handles DuckDB rotation by exporting to Parquet
func (fm *FileManager) rotateDuckDBWriter() error {
	if fm.db == nil {
		return nil
	}

	// Export table to Parquet file
	fileName := fmt.Sprintf("redis_data_part_%04d.parquet", fm.partitionID)
	filePath := filepath.Join(fm.currentPartitionPath, fileName)

	exportSQL := fmt.Sprintf("COPY %s TO '%s' (FORMAT 'parquet')", fm.tableName, filePath)
	if _, err := fm.db.Exec(exportSQL); err != nil {
		return fmt.Errorf("failed to export to Parquet: %w", err)
	}

	// Get file info
	stat, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("failed to stat Parquet file: %w", err)
	}

	// Add partition info
	partitionInfo := PartitionInfo{
		PartitionID:   fm.partitionID,
		DataType:      "redis_data",
		FileName:      fileName,
		RecordCount:   fm.recordCount,
		FileSizeBytes: stat.Size(),
		StartTime:     time.Now().Add(-time.Hour), // Approximate
		EndTime:       time.Now(),
	}
	fm.metadata.Partitions = append(fm.metadata.Partitions, partitionInfo)

	// Drop the table and close connection
	if _, err := fm.db.Exec(fmt.Sprintf("DROP TABLE %s", fm.tableName)); err != nil {
		// Log error but continue - table might not exist
		fmt.Printf("Warning: failed to drop table: %v\n", err)
	}
	if err := fm.db.Close(); err != nil {
		return fmt.Errorf("failed to close database connection: %w", err)
	}
	fm.db = nil

	fm.recordCount = 0
	return nil
}

// FlushAll flushes all active writers
func (fm *FileManager) FlushAll() {
	switch fm.config.Format {
	case FormatCSV:
		if fm.csvWriter != nil {
			fm.csvWriter.Flush()
		}
	case FormatParquet:
		// DuckDB handles flushing automatically
	}
}

// SetMetadata updates the export metadata
func (fm *FileManager) SetMetadata(pattern string, totalKeys int64) {
	fm.metadata.Pattern = pattern
	fm.metadata.TotalKeys = totalKeys
}

// Close finalizes all writers and creates metadata file
func (fm *FileManager) Close() error {
	// Rotate final partition
	if fm.recordCount > 0 {
		if err := fm.RotateWriter(); err != nil {
			fmt.Printf("Error rotating final writer: %v\n", err)
		}
	}

	// Write metadata file
	fm.metadata.EndTime = time.Now()
	metadataPath := filepath.Join(fm.config.OutputDir, "export_metadata.json")
	metadataFile, err := os.Create(metadataPath)
	if err != nil {
		return fmt.Errorf("failed to create metadata file: %w", err)
	}
	defer func() {
		if err := metadataFile.Close(); err != nil {
			fmt.Printf("Warning: failed to close metadata file: %v\n", err)
		}
	}()

	encoder := json.NewEncoder(metadataFile)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(fm.metadata); err != nil {
		return fmt.Errorf("failed to write metadata: %w", err)
	}

	return nil
}

// GetQueryPath returns the DuckDB query path for all data
func (fm *FileManager) GetQueryPath() string {
	pattern := filepath.Join(
		fm.config.OutputDir,
		"**",
		fmt.Sprintf("*.%s", string(fm.config.Format)),
	)
	return pattern
}
