package models

type Request struct {
	URL string `json:"url" validate:"required,url"`
}

type Response struct {
	Result string `json:"result"`
}

type OriginalURLToCorrelationID struct {
	CorrelationID string `json:"correlation_id" validate:"required"`
	OriginalURL   string `json:"original_url" validate:"required,url"`
}

type PostApishortenbatchRequest []OriginalURLToCorrelationID

type ShortURLToCorrelationID struct {
	CorrelationID string `json:"correlation_id" validate:"required"`
	ShortURL      string `json:"short_url" validate:"required,url"`
}

type PostApishortenbatchResponse []ShortURLToCorrelationID
