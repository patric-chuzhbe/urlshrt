package storage

import (
	"context"
	"database/sql"
	"errors"
	"github.com/patric-chuzhbe/urlshrt/internal/models"
	"github.com/patric-chuzhbe/urlshrt/internal/user"
)

var ErrURLMarkedAsDeleted = errors.New("the URL marked as deleted")

type Storage interface {
	InsertURLMapping(
		ctx context.Context,
		short,
		full string,
		transaction *sql.Tx,
	) error

	Close() error

	FindFullByShort(ctx context.Context, short string) (string, bool, error)

	FindShortByFull(
		ctx context.Context,
		full string,
		transaction *sql.Tx,
	) (string, bool, error)

	IsShortExists(ctx context.Context, short string) (bool, error)

	Ping(ctx context.Context) error

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

	BeginTransaction() (*sql.Tx, error)

	RollbackTransaction(transaction *sql.Tx) error

	CommitTransaction(transaction *sql.Tx) error

	GetUserByID(ctx context.Context, userID int, transaction *sql.Tx) (*user.User, error)

	CreateUser(ctx context.Context, usr *user.User, transaction *sql.Tx) (int, error)

	GetUserUrls(
		ctx context.Context,
		userID int,
		shortURLFormatter func(string) string,
	) (models.UserUrls, error)

	SaveUserUrls(
		ctx context.Context,
		userID int,
		urls []string,
		transaction *sql.Tx,
	) error

	RemoveUsersUrls(
		ctx context.Context,
		usersURLs map[int][]string,
	) error
}
