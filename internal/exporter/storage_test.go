package exporter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewFileManager(t *testing.T) {
	config := StorageConfig{
		OutputDir:  "/tmp/test",
		Format:     FormatParquet,
		MaxRecords: 1000,
	}

	fm := NewFileManager(config)

	if fm == nil {
		t.Fatal("NewFileManager returned nil")
	}

	if fm.config.OutputDir != config.OutputDir {
		t.Errorf("Expected OutputDir %s, got %s", config.OutputDir, fm.config.OutputDir)
	}

	if fm.config.Format != config.Format {
		t.Errorf("Expected Format %s, got %s", config.Format, fm.config.Format)
	}

	if fm.tableName != "redis_data" {
		t.Errorf("Expected tableName 'redis_data', got %s", fm.tableName)
	}

	if fm.recordCount != 0 {
		t.Errorf("Expected recordCount 0, got %d", fm.recordCount)
	}
}

func TestCreateHivePartitionPath(t *testing.T) {
	config := StorageConfig{
		OutputDir:  "/tmp/test",
		Format:     FormatParquet,
		MaxRecords: 1000,
	}

	fm := NewFileManager(config)
	testTime := time.Date(2024, 1, 15, 14, 30, 0, 0, time.UTC)

	expectedPath := filepath.Join("/tmp/test", "year=2024", "month=01", "day=15", "hour=14")
	actualPath := fm.CreateHivePartitionPath(testTime)

	if actualPath != expectedPath {
		t.Errorf("Expected path %s, got %s", expectedPath, actualPath)
	}
}

func TestCSVWriting(t *testing.T) {
	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "redis_dumper_test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Warning: failed to remove temp dir: %v", err)
		}
	}()

	config := StorageConfig{
		OutputDir:  tempDir,
		Format:     FormatCSV,
		MaxRecords: 5, // Small number for testing rotation
	}

	fm := NewFileManager(config)

	// Test writing records
	records := []*RedisRecord{
		{
			Key:        "test:key1",
			Type:       "string",
			Value:      "value1",
			TTLSeconds: 3600,
			ExportedAt: "2024-01-15T14:30:00Z",
		},
		{
			Key:        "test:key2",
			Type:       "hash",
			Value:      `{"field1": "value1", "field2": "value2"}`,
			TTLSeconds: -1,
			ExportedAt: "2024-01-15T14:30:01Z",
		},
		{
			Key:        "test:key3",
			Type:       "set",
			Value:      "member1",
			TTLSeconds: 7200,
			ExportedAt: "2024-01-15T14:30:02Z",
		},
	}

	for _, record := range records {
		if err := fm.WriteRecord(record); err != nil {
			t.Errorf("Failed to write record: %v", err)
		}
	}

	// Test flushing
	fm.FlushAll()

	// Close and check metadata
	if err := fm.Close(); err != nil {
		t.Errorf("Failed to close file manager: %v", err)
	}

	// Verify metadata file exists
	metadataPath := filepath.Join(tempDir, "export_metadata.json")
	if _, err := os.Stat(metadataPath); os.IsNotExist(err) {
		t.Error("Metadata file was not created")
	}

	// Read and verify metadata
	metadataFile, err := os.Open(metadataPath)
	if err != nil {
		t.Errorf("Failed to open metadata file: %v", err)
	}
	defer func() {
		if err := metadataFile.Close(); err != nil {
			t.Logf("Warning: failed to close metadata file: %v", err)
		}
	}()

	var metadata ExportMetadata
	if err := json.NewDecoder(metadataFile).Decode(&metadata); err != nil {
		t.Errorf("Failed to decode metadata: %v", err)
	}

	if len(metadata.Partitions) == 0 {
		t.Error("No partitions found in metadata")
	}

	// Verify CSV files exist in Hive structure
	found := false
	err = filepath.Walk(tempDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if filepath.Ext(path) == ".csv" {
			found = true
			// Verify the file has content
			stat, err := os.Stat(path)
			if err != nil {
				return err
			}
			if stat.Size() == 0 {
				t.Errorf("CSV file %s is empty", path)
			}
		}
		return nil
	})

	if err != nil {
		t.Errorf("Error walking directory: %v", err)
	}

	if !found {
		t.Error("No CSV files found")
	}
}

func TestParquetWriting(t *testing.T) {
	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "redis_dumper_parquet_test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Warning: failed to remove temp dir: %v", err)
		}
	}()

	config := StorageConfig{
		OutputDir:  tempDir,
		Format:     FormatParquet,
		MaxRecords: 3, // Small number for testing rotation
	}

	fm := NewFileManager(config)

	// Test writing records
	records := []*RedisRecord{
		{
			Key:        "test:key1",
			Type:       "string",
			Value:      "simple string value",
			TTLSeconds: 3600,
			ExportedAt: "2024-01-15T14:30:00Z",
		},
		{
			Key:        "test:key2",
			Type:       "hash",
			Value:      `{"username": "john", "email": "john@example.com"}`,
			TTLSeconds: -1,
			ExportedAt: "2024-01-15T14:30:01Z",
		},
		{
			Key:        "test:key3",
			Type:       "zset",
			Value:      `{"member": "player1", "score": "100", "rank": 1}`,
			TTLSeconds: 7200,
			ExportedAt: "2024-01-15T14:30:02Z",
		},
		{
			Key:        "test:key4",
			Type:       "list",
			Value:      `{"index": 0, "value": "first item"}`,
			TTLSeconds: 3600,
			ExportedAt: "2024-01-15T14:30:03Z",
		},
		{
			Key:        "test:key5",
			Type:       "set",
			Value:      "member1",
			TTLSeconds: -1,
			ExportedAt: "2024-01-15T14:30:04Z",
		},
	}

	for _, record := range records {
		if err := fm.WriteRecord(record); err != nil {
			t.Errorf("Failed to write record: %v", err)
		}
	}

	// Close and check metadata
	if err := fm.Close(); err != nil {
		t.Errorf("Failed to close file manager: %v", err)
	}

	// Verify metadata file exists
	metadataPath := filepath.Join(tempDir, "export_metadata.json")
	if _, err := os.Stat(metadataPath); os.IsNotExist(err) {
		t.Error("Metadata file was not created")
	}

	// Read and verify metadata
	metadataFile, err := os.Open(metadataPath)
	if err != nil {
		t.Errorf("Failed to open metadata file: %v", err)
	}
	defer func() {
		if err := metadataFile.Close(); err != nil {
			t.Logf("Warning: failed to close metadata file: %v", err)
		}
	}()

	var metadata ExportMetadata
	if err := json.NewDecoder(metadataFile).Decode(&metadata); err != nil {
		t.Errorf("Failed to decode metadata: %v", err)
	}

	if len(metadata.Partitions) == 0 {
		t.Error("No partitions found in metadata")
	}

	// Verify Parquet files exist in Hive structure
	found := false
	err = filepath.Walk(tempDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if filepath.Ext(path) == ".parquet" {
			found = true
			// Verify the file has content
			stat, err := os.Stat(path)
			if err != nil {
				return err
			}
			if stat.Size() == 0 {
				t.Errorf("Parquet file %s is empty", path)
			}
		}
		return nil
	})

	if err != nil {
		t.Errorf("Error walking directory: %v", err)
	}

	if !found {
		t.Error("No Parquet files found")
	}
}

func TestFileRotation(t *testing.T) {
	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "redis_dumper_rotation_test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Warning: failed to remove temp dir: %v", err)
		}
	}()

	config := StorageConfig{
		OutputDir:  tempDir,
		Format:     FormatCSV,
		MaxRecords: 2, // Force rotation after 2 records
	}

	fm := NewFileManager(config)

	// Write more records than MaxRecords to trigger rotation
	records := []*RedisRecord{
		{Key: "key1", Type: "string", Value: "value1", TTLSeconds: 3600, ExportedAt: "2024-01-15T14:30:00Z"},
		{Key: "key2", Type: "string", Value: "value2", TTLSeconds: 3600, ExportedAt: "2024-01-15T14:30:01Z"},
		{Key: "key3", Type: "string", Value: "value3", TTLSeconds: 3600, ExportedAt: "2024-01-15T14:30:02Z"},
		{Key: "key4", Type: "string", Value: "value4", TTLSeconds: 3600, ExportedAt: "2024-01-15T14:30:03Z"},
		{Key: "key5", Type: "string", Value: "value5", TTLSeconds: 3600, ExportedAt: "2024-01-15T14:30:04Z"},
	}

	for _, record := range records {
		if err := fm.WriteRecord(record); err != nil {
			t.Errorf("Failed to write record: %v", err)
		}
	}

	if err := fm.Close(); err != nil {
		t.Errorf("Failed to close file manager: %v", err)
	}

	// Read metadata to check partitions
	metadataPath := filepath.Join(tempDir, "export_metadata.json")
	metadataFile, err := os.Open(metadataPath)
	if err != nil {
		t.Errorf("Failed to open metadata file: %v", err)
	}
	defer func() {
		if err := metadataFile.Close(); err != nil {
			t.Logf("Warning: failed to close metadata file: %v", err)
		}
	}()

	var metadata ExportMetadata
	if err := json.NewDecoder(metadataFile).Decode(&metadata); err != nil {
		t.Errorf("Failed to decode metadata: %v", err)
	}

	// Should have multiple partitions due to rotation
	if len(metadata.Partitions) < 2 {
		t.Errorf("Expected at least 2 partitions due to rotation, got %d", len(metadata.Partitions))
	}

	// Count CSV files
	csvCount := 0
	err = filepath.Walk(tempDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if filepath.Ext(path) == ".csv" {
			csvCount++
		}
		return nil
	})

	if err != nil {
		t.Errorf("Error walking directory: %v", err)
	}

	if csvCount < 2 {
		t.Errorf("Expected at least 2 CSV files due to rotation, got %d", csvCount)
	}
}

func TestGetQueryPath(t *testing.T) {
	tests := []struct {
		name        string
		format      OutputFormat
		outputDir   string
		expectedExt string
	}{
		{
			name:        "CSV format",
			format:      FormatCSV,
			outputDir:   "/tmp/test",
			expectedExt: "csv",
		},
		{
			name:        "Parquet format",
			format:      FormatParquet,
			outputDir:   "/home/user/data",
			expectedExt: "parquet",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := StorageConfig{
				OutputDir:  tt.outputDir,
				Format:     tt.format,
				MaxRecords: 1000,
			}

			fm := NewFileManager(config)
			queryPath := fm.GetQueryPath()

			expectedPath := filepath.Join(tt.outputDir, "**", "*."+tt.expectedExt)
			if queryPath != expectedPath {
				t.Errorf("Expected query path %s, got %s", expectedPath, queryPath)
			}
		})
	}
}

func TestSetMetadata(t *testing.T) {
	config := StorageConfig{
		OutputDir:  "/tmp/test",
		Format:     FormatParquet,
		MaxRecords: 1000,
	}

	fm := NewFileManager(config)

	pattern := "user:*"
	totalKeys := int64(12345)

	fm.SetMetadata(pattern, totalKeys)

	if fm.metadata.Pattern != pattern {
		t.Errorf("Expected pattern %s, got %s", pattern, fm.metadata.Pattern)
	}

	if fm.metadata.TotalKeys != totalKeys {
		t.Errorf("Expected total keys %d, got %d", totalKeys, fm.metadata.TotalKeys)
	}
}

func TestInvalidFormat(t *testing.T) {
	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "redis_dumper_invalid_test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Warning: failed to remove temp dir: %v", err)
		}
	}()

	config := StorageConfig{
		OutputDir:  tempDir,
		Format:     OutputFormat("invalid"),
		MaxRecords: 1000,
	}

	fm := NewFileManager(config)

	record := &RedisRecord{
		Key:        "test:key",
		Type:       "string",
		Value:      "value",
		TTLSeconds: 3600,
		ExportedAt: "2024-01-15T14:30:00Z",
	}

	// Should return error for invalid format
	if err := fm.WriteRecord(record); err == nil {
		t.Error("Expected error for invalid format, got nil")
	}
}

func TestEmptyRecordHandling(t *testing.T) {
	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "redis_dumper_empty_test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Warning: failed to remove temp dir: %v", err)
		}
	}()

	config := StorageConfig{
		OutputDir:  tempDir,
		Format:     FormatCSV,
		MaxRecords: 1000,
	}

	fm := NewFileManager(config)

	// Test closing without writing any records
	if err := fm.Close(); err != nil {
		t.Errorf("Failed to close empty file manager: %v", err)
	}

	// Verify metadata file still exists
	metadataPath := filepath.Join(tempDir, "export_metadata.json")
	if _, err := os.Stat(metadataPath); os.IsNotExist(err) {
		t.Error("Metadata file was not created for empty file manager")
	}
}

// Benchmark tests
func BenchmarkCSVWriting(b *testing.B) {
	tempDir, err := os.MkdirTemp("", "redis_dumper_bench")
	if err != nil {
		b.Fatal(err)
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			b.Logf("Warning: failed to remove temp dir: %v", err)
		}
	}()

	config := StorageConfig{
		OutputDir:  tempDir,
		Format:     FormatCSV,
		MaxRecords: 100000,
	}

	fm := NewFileManager(config)

	record := &RedisRecord{
		Key:        "benchmark:key",
		Type:       "string",
		Value:      "benchmark value with some content",
		TTLSeconds: 3600,
		ExportedAt: "2024-01-15T14:30:00Z",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := fm.WriteRecord(record); err != nil {
			b.Errorf("Failed to write record: %v", err)
		}
	}

	if err := fm.Close(); err != nil {
		b.Errorf("Failed to close file manager: %v", err)
	}
}

func BenchmarkParquetWriting(b *testing.B) {
	tempDir, err := os.MkdirTemp("", "redis_dumper_parquet_bench")
	if err != nil {
		b.Fatal(err)
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			b.Logf("Warning: failed to remove temp dir: %v", err)
		}
	}()

	config := StorageConfig{
		OutputDir:  tempDir,
		Format:     FormatParquet,
		MaxRecords: 100000,
	}

	fm := NewFileManager(config)

	record := &RedisRecord{
		Key:        "benchmark:key",
		Type:       "string",
		Value:      "benchmark value with some content",
		TTLSeconds: 3600,
		ExportedAt: "2024-01-15T14:30:00Z",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := fm.WriteRecord(record); err != nil {
			b.Errorf("Failed to write record: %v", err)
		}
	}

	if err := fm.Close(); err != nil {
		b.Errorf("Failed to close file manager: %v", err)
	}
}
