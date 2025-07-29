package exporter

type Exporter interface {
	ExportKeysOnly() error
	ExportKeysOnlyByPattern(pattern string) error
	ExportByPattern(pattern string) error
	Close() error
}
