package models

import "errors"

type ShortenRequest struct {
	URL string `json:"url" validate:"required,url"`
}

type ShortenResponse struct {
	Result string `json:"result"`
}

type ShortenRequestItem struct {
	CorrelationID string `json:"correlation_id" validate:"required"`
	OriginalURL   string `json:"original_url" validate:"required,url"`
}

type BatchShortenRequest []ShortenRequestItem

type BatchShortenResponseItem struct {
	CorrelationID string `json:"correlation_id" validate:"required"`
	ShortURL      string `json:"short_url" validate:"required,url"`
}

type BatchShortenResponse []BatchShortenResponseItem

type UserURL struct {
	ShortURL    string `json:"short_url" validate:"required,url"`
	OriginalURL string `json:"original_url" validate:"required,url"`
}

type UserUrls []UserURL

const (
	StorageTypeUnknown = iota
	StorageTypePostgresql
	StorageTypeFile
	StorageTypeMemory
)

type DeleteURLsRequest []string

var ErrURLMarkedAsDeleted = errors.New("the URL marked as deleted")

type URLDeleteJob struct {
	UserID       string
	URLsToDelete DeleteURLsRequest
}
