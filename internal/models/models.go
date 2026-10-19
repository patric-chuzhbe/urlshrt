package models

import "errors"

// ShortenRequest represents an input URL for the shortening API.
type ShortenRequest struct {
	URL string `json:"url" validate:"required,url"` // Original long URL to be shortened
}

// ShortenResponse defines the response payload containing the shortened URL.
type ShortenResponse struct {
	Result string `json:"result"` // Shortened URL
}

// ShortenRequestItem defines a batch shortening request payload.
type ShortenRequestItem struct {
	CorrelationID string `json:"correlation_id" validate:"required"`   // ID to correlate request/response
	OriginalURL   string `json:"original_url" validate:"required,url"` // Original URL
}

// BatchShortenRequest defines a batch shortening request payload.
type BatchShortenRequest []ShortenRequestItem

// BatchShortenResponseItem defines single item for a batch shortening response payload
type BatchShortenResponseItem struct {
	CorrelationID string `json:"correlation_id" validate:"required"` // Correlation ID matching the request
	ShortURL      string `json:"short_url" validate:"required,url"`  // Shortened URL
}

// BatchShortenResponse defines the response payload for batch shortening.
type BatchShortenResponse []BatchShortenResponseItem

// UserURL represents a mapping between a short and original URL for a user.
type UserURL struct {
	ShortURL    string `json:"short_url" validate:"required,url"`
	OriginalURL string `json:"original_url" validate:"required,url"`
}

// UserUrls is a slice of UserURL, returned for user-specific URL queries.
type UserUrls []UserURL

// Storage type constants. See every constant description.
const (
	// StorageTypeUnknown represents an unknown storage type. Used when the storage type is undefined or unsupported.
	StorageTypeUnknown = iota

	// StorageTypePostgresql represents the PostgreSQL storage type. Used for database-backed storage solutions.
	StorageTypePostgresql

	// StorageTypeFile represents the file-based storage type. Used when data is stored in JSON file.
	StorageTypeFile

	// StorageTypeMemory represents the in-memory storage type. Used for fast, temporary data storage (e.g., caching).
	StorageTypeMemory
)

// DeleteURLsRequest represents a slice of short keys of URLs to be deleted.
// Used as request body in batch delete operations.
type DeleteURLsRequest []string

// ErrURLMarkedAsDeleted is returned when an attempt is made to access or modify a URL that is marked as deleted.
var ErrURLMarkedAsDeleted = errors.New("the URL marked as deleted")

// URLDeleteJob defines a deletion task associated with a specific user.
// Used in background deletion queues.
type URLDeleteJob struct {
	UserID       string            // ID of the user initiating deletion
	URLsToDelete DeleteURLsRequest // URLs to be deleted
}

// URLFormatter defines a function type that takes a string URL as input
// and returns a modified string. It is typically used to apply formatting
// to short URLs before presenting them to the user (e.g., prefixing with a base URL).
type URLFormatter func(string) string

// InternalStatsResponse defines the schema for the response payload
// of the GET /api/internal/stats endpoint.
//
// It contains service-level statistics useful for internal monitoring,
// including the number of active shortened URLs and registered users.
type InternalStatsResponse struct {
	URLs  int64 `json:"urls"`  // Total active (non-deleted) shortened URLs
	Users int64 `json:"users"` // Total count of distinct users tracked by the application.
}
