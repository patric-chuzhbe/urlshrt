package storage

import (
	"context"
	"database/sql"
)

type Storage interface {
	Insert(
		outerCtx context.Context,
		short,
		full string,
		transaction *sql.Tx,
	) error

	Close() error

	FindFullByShort(outerCtx context.Context, short string) (string, bool, error)

	FindShortByFull(
		outerCtx context.Context,
		full string,
		transaction *sql.Tx,
	) (string, bool, error)

	IsShortExists(outerCtx context.Context, short string) (bool, error)

	Ping(outerCtx context.Context) error

	FindShortsByFulls(
		outerCtx context.Context,
		originalUrls []string,
		transaction *sql.Tx,
	) (map[string]string, error)

	SaveNewFullsAndShorts(
		outerCtx context.Context,
		unexistentFullsToShortsMap map[string]string,
		transaction *sql.Tx,
	) error

	BeginTransaction() (*sql.Tx, error)

	RollbackTransaction(transaction *sql.Tx) error

	CommitTransaction(transaction *sql.Tx) error
}
