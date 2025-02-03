package models

type ShortenRequest struct {
	URL string `json:"url" validate:"required,url"`
}

type ShortenResponse struct {
	Result string `json:"result"`
}

type BatchShortenRequest []struct {
	CorrelationID string `json:"correlation_id" validate:"required"`
	OriginalURL   string `json:"original_url" validate:"required,url"`
}

type BatchShortenResponseItem struct {
	CorrelationID string `json:"correlation_id" validate:"required"`
	ShortURL      string `json:"short_url" validate:"required,url"`
}

type BatchShortenResponse []BatchShortenResponseItem

const (
	StorageTypeUnknown = iota
	StorageTypePostgresql
	StorageTypeFile
	StorageTypeMemory
)
