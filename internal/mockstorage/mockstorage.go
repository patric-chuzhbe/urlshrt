// Package mockstorage provides a testify-based mock implementation
// of the internal storage interfaces used by the router package.
// It is used for unit testing HTTP handlers by simulating storage behavior.
package mockstorage

import (
	"context"
	"database/sql"

	"github.com/stretchr/testify/mock"

	"github.com/patric-chuzhbe/urlshrt/internal/models"
	"github.com/patric-chuzhbe/urlshrt/internal/user"
)

// StorageMock is a testify mock that implements all interfaces
// used by the router for storage operations.
//
// Use it in router tests to simulate database behavior.
type StorageMock struct {
	mock.Mock
}

// Ping mocks the pinger interface to simulate a health check.
func (m *StorageMock) Ping(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

// BeginTransaction mocks the beginning of a transaction.
func (m *StorageMock) BeginTransaction() (*sql.Tx, error) {
	args := m.Called()
	tx, _ := args.Get(0).(*sql.Tx)
	return tx, args.Error(1)
}

// CommitTransaction mocks committing a transaction.
func (m *StorageMock) CommitTransaction(tx *sql.Tx) error {
	args := m.Called(tx)
	return args.Error(0)
}

// RollbackTransaction mocks rolling back a transaction.
func (m *StorageMock) RollbackTransaction(tx *sql.Tx) error {
	args := m.Called(tx)
	return args.Error(0)
}

// GetUserUrls mocks fetching a user's associated shortened URLs.
func (m *StorageMock) GetUserUrls(
	ctx context.Context,
	userID string,
	shortURLFormatter models.URLFormatter,
) (models.UserUrls, error) {
	args := m.Called(ctx, userID, shortURLFormatter)
	return args.Get(0).(models.UserUrls), args.Error(1)
}

// SaveUserUrls mocks storing a set of URLs for a user.
func (m *StorageMock) SaveUserUrls(
	ctx context.Context,
	userID string,
	urls []string,
	tx *sql.Tx,
) error {
	args := m.Called(ctx, userID, urls, tx)
	return args.Error(0)
}

// FindShortsByFulls mocks reverse lookup: full URLs to short URLs.
func (m *StorageMock) FindShortsByFulls(
	ctx context.Context,
	originalUrls []string,
	tx *sql.Tx,
) (map[string]string, error) {
	args := m.Called(ctx, originalUrls, tx)
	return args.Get(0).(map[string]string), args.Error(1)
}

// SaveNewFullsAndShorts mocks batch saving of new URL mappings.
func (m *StorageMock) SaveNewFullsAndShorts(
	ctx context.Context,
	unexistentFullsToShortsMap map[string]string,
	tx *sql.Tx,
) error {
	args := m.Called(ctx, unexistentFullsToShortsMap, tx)
	return args.Error(0)
}

// FindFullByShort mocks finding the full URL for a given short code.
func (m *StorageMock) FindFullByShort(ctx context.Context, short string) (string, bool, error) {
	args := m.Called(ctx, short)
	return args.String(0), args.Bool(1), args.Error(2)
}

// FindShortByFull mocks finding the short code for a full URL.
func (m *StorageMock) FindShortByFull(ctx context.Context, full string, tx *sql.Tx) (string, bool, error) {
	args := m.Called(ctx, full, tx)
	return args.String(0), args.Bool(1), args.Error(2)
}

// InsertURLMapping mocks inserting a new short-full mapping.
func (m *StorageMock) InsertURLMapping(ctx context.Context, short, full string, tx *sql.Tx) error {
	args := m.Called(ctx, short, full, tx)
	return args.Error(0)
}

// CreateUser mocks user creation and returns a generated ID.
func (m *StorageMock) CreateUser(ctx context.Context, usr *user.User, tx *sql.Tx) (string, error) {
	args := m.Called(ctx, usr, tx)
	return args.String(0), args.Error(1)
}

// GetUserByID mocks fetching a user by their ID.
func (m *StorageMock) GetUserByID(ctx context.Context, userID string, tx *sql.Tx) (*user.User, error) {
	args := m.Called(ctx, userID, tx)
	return args.Get(0).(*user.User), args.Error(1)
}

// Close mocks closing the storage and releasing resources.
func (m *StorageMock) Close() error {
	args := m.Called()
	return args.Error(0)
}
