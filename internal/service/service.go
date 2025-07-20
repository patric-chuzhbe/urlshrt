package service

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/thoas/go-funk"

	"github.com/patric-chuzhbe/urlshrt/internal/models"
)

type transactioner interface {
	BeginTransaction() (*sql.Tx, error)

	RollbackTransaction(transaction *sql.Tx) error

	CommitTransaction(transaction *sql.Tx) error
}

type urlsMapper interface {
	FindShortsByFulls(
		ctx context.Context,
		originalUrls []string,
		transaction *sql.Tx,
	) (map[string]string, error)

	SaveNewFullsAndShorts(
		ctx context.Context,
		unexistentFullsToShortsMap map[string]string,
		transaction *sql.Tx,
	) error

	FindFullByShort(ctx context.Context, short string) (string, bool, error)

	FindShortByFull(
		ctx context.Context,
		full string,
		transaction *sql.Tx,
	) (string, bool, error)

	InsertURLMapping(
		ctx context.Context,
		short,
		full string,
		transaction *sql.Tx,
	) error
}

type userUrlsKeeper interface {
	GetUserUrls(
		ctx context.Context,
		userID string,
		shortURLFormatter models.URLFormatter,
	) (models.UserUrls, error)

	SaveUserUrls(
		ctx context.Context,
		userID string,
		urls []string,
		transaction *sql.Tx,
	) error

	GetNumberOfShortenedURLs(ctx context.Context) (int64, error)

	GetNumberOfUsers(ctx context.Context) (int64, error)
}

type pinger interface {
	Ping(ctx context.Context) error
}

type storage interface {
	transactioner
	urlsMapper
	userUrlsKeeper
	pinger
}

type urlsRemover interface {
	EnqueueJob(job *models.URLDeleteJob)
}

// ErrConflict is returned when a short URL already exists for the provided original URL.
var ErrConflict = errors.New("URL already shortened")

type Service struct {
	db           storage
	urlsRemover  urlsRemover
	shortURLBase string
}

var ErrURLMarkedAsDeleted = models.ErrURLMarkedAsDeleted

var ErrInvalidURLInRequest = errors.New("there is no valid URL substring in the request")

var urlPattern = regexp.MustCompile(`\bhttps?://\S+\b`)

func New(
	db storage,
	urlsRemover urlsRemover,
	shortURLBase string,
) *Service {
	return &Service{
		db:           db,
		urlsRemover:  urlsRemover,
		shortURLBase: shortURLBase,
	}
}

// ShortenURL shortens a given URL and links it to the specified user.
func (s *Service) ShortenURL(ctx context.Context, urlToShort, userID string) (string, error) {
	tx, err := s.db.BeginTransaction()
	if err != nil {
		return "", err
	}
	defer func() {
		_ = s.db.RollbackTransaction(tx)
	}()

	short, found, err := s.db.FindShortByFull(ctx, urlToShort, tx)
	if err != nil {
		return "", err
	}

	var resultErr error
	if found {
		resultErr = ErrConflict
	} else {
		short = uuid.New().String()
		if err := s.db.InsertURLMapping(ctx, short, urlToShort, tx); err != nil {
			return "", err
		}
	}

	if err := s.db.SaveUserUrls(ctx, userID, []string{urlToShort}, tx); err != nil {
		return "", err
	}

	if err := s.db.CommitTransaction(tx); err != nil {
		return "", err
	}

	return s.GetShortURL(short), resultErr
}

func (s *Service) GetOriginalURL(ctx context.Context, short string) (string, error) {
	full, found, err := s.db.FindFullByShort(ctx, short)
	if err != nil {
		return "", err
	}
	if !found {
		return "", nil
	}
	return full, nil
}

// Ping checks the health of the database/storage layer.
func (s *Service) Ping(ctx context.Context) error {
	return s.db.Ping(ctx)
}

func (s *Service) BatchShortenURLs(ctx context.Context, batch models.BatchShortenRequest, userID string) (models.BatchShortenResponse, error) {
	tx, err := s.db.BeginTransaction()
	if err != nil {
		return nil, err
	}
	defer s.db.RollbackTransaction(tx)

	corrMap := make(map[string]string, len(batch))
	originals := make([]string, 0, len(batch))
	for _, item := range batch {
		corrMap[item.OriginalURL] = item.CorrelationID
		originals = append(originals, item.OriginalURL)
	}

	existingMap, err := s.db.FindShortsByFulls(ctx, originals, tx)
	if err != nil {
		return nil, err
	}

	unseen := differenceStringSlices(originals, funk.Keys(existingMap).([]string))
	newMap := make(map[string]string, len(unseen))
	for _, url := range unseen {
		newMap[url] = uuid.New().String()
	}

	if err := s.db.SaveNewFullsAndShorts(ctx, newMap, tx); err != nil {
		return nil, err
	}

	if err := s.db.SaveUserUrls(ctx, userID, funk.Uniq(funk.Union(originals, originals)).([]string), tx); err != nil {
		return nil, err
	}

	if err := s.db.CommitTransaction(tx); err != nil {
		return nil, err
	}

	response := make(models.BatchShortenResponse, 0, len(batch))
	for full, short := range existingMap {
		response = append(response, models.BatchShortenResponseItem{
			CorrelationID: corrMap[full],
			ShortURL:      s.GetShortURL(short),
		})
	}
	for full, short := range newMap {
		response = append(response, models.BatchShortenResponseItem{
			CorrelationID: corrMap[full],
			ShortURL:      s.GetShortURL(short),
		})
	}

	return response, nil
}

func (s *Service) GetUserURLs(ctx context.Context, userID string) (models.UserUrls, error) {
	return s.db.GetUserUrls(ctx, userID, s.GetShortURL)
}

// DeleteURLsAsync enqueues a URL deletion job for background processing.
func (s *Service) DeleteURLsAsync(ctx context.Context, userID string, urls models.DeleteURLsRequest) {
	s.urlsRemover.EnqueueJob(&models.URLDeleteJob{
		UserID:       userID,
		URLsToDelete: urls,
	})
}

// GetInternalStats returns statistics such as total shortened URLs and user count.
func (s *Service) GetInternalStats(ctx context.Context) (models.InternalStatsResponse, error) {
	urls, err := s.db.GetNumberOfShortenedURLs(ctx)
	if err != nil {
		return models.InternalStatsResponse{}, err
	}

	users, err := s.db.GetNumberOfUsers(ctx)
	if err != nil {
		return models.InternalStatsResponse{}, err
	}

	return models.InternalStatsResponse{
		URLs:  urls,
		Users: users,
	}, nil
}

func (s *Service) GetShortURL(shortKey string) string {
	return s.shortURLBase + "/" + shortKey
}

func (s *Service) GetShortURLKey(shortURL string) string {
	if shortURL == "" || s.shortURLBase == "" {
		return ""
	}
	base := strings.TrimRight(s.shortURLBase, "/")
	url := strings.TrimPrefix(shortURL, base)
	return strings.TrimPrefix(url, "/")
}

func (s *Service) ExtractFirstURL(urlToShort string) (string, error) {
	match := urlPattern.FindString(urlToShort)
	if match == "" {
		return "", ErrInvalidURLInRequest
	}

	if !isValidURL(match) {
		return "", ErrInvalidURLInRequest
	}

	return match, nil
}

func differenceStringSlices(a, b []string) []string {
	bSet := make(map[string]struct{}, len(b))
	for _, item := range b {
		bSet[item] = struct{}{}
	}

	var diff []string
	for _, item := range a {
		if _, found := bSet[item]; !found {
			diff = append(diff, item)
		}
	}
	return diff
}

func isValidURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil &&
		(u.Scheme == "http" || u.Scheme == "https") &&
		u.Host != ""
}
