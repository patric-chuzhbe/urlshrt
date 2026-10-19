// Package postgresdb provides a PostgreSQL-based implementation of the storage interface
// for persisting and retrieving URL mappings and user data.
// It supports transactional operations, URL removal, and user-URL relationships.
package postgresdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/pressly/goose/v3"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/patric-chuzhbe/urlshrt/internal/models"
	"github.com/patric-chuzhbe/urlshrt/internal/user"

	"github.com/patric-chuzhbe/urlshrt/internal/db/postgresdb/sqlc"
)

// PostgresDB is a PostgreSQL-backed implementation of a URL shortener storage.
// It handles all persistence operations via a PostgreSQL database connection.
type PostgresDB struct {
	database          *sql.DB
	connectionTimeout time.Duration
	queries           *sqlc.Queries
}

type initOptions struct {
	DBPreReset bool
}

// New establishes a connection to the PostgreSQL database,
// runs schema migrations, and returns a configured PostgresDB instance.
// Optionally accepts initialization options, such as WithDBPreReset.
func New(
	ctx context.Context,
	databaseDSN string,
	connectionTimeout time.Duration,
	migrationsDir string,
	optionsProto ...InitOption,
) (*PostgresDB, error) {
	options := &initOptions{
		DBPreReset: false,
	}
	for _, protoOption := range optionsProto {
		protoOption(options)
	}

	database, err := sql.Open("pgx", databaseDSN)
	if err != nil {
		return nil, err
	}

	result := &PostgresDB{
		database:          database,
		connectionTimeout: connectionTimeout,
		queries:           sqlc.New(database),
	}

	if options.DBPreReset {
		if err := result.resetDB(ctx); err != nil {
			return nil,
				fmt.Errorf(
					"in internal/db/postgresdb/postgresdb.go/New(): error while `result.resetDB()` calling: %w",
					err,
				)
		}
	}

	if err := goose.SetDialect("postgres"); err != nil {
		return nil,
			fmt.Errorf(
				"in internal/db/postgresdb/postgresdb.go/New(): error while `goose.SetDialect()` calling: %w",
				err,
			)
	}

	if err := goose.Up(result.database, migrationsDir); err != nil {
		return nil,
			fmt.Errorf(
				"in internal/db/postgresdb/postgresdb.go/New(): error while `goose.Up()` calling: %w",
				err,
			)
	}

	return result, nil
}

// RemoveUsersUrls marks a batch of URLs as deleted for specified user IDs.
// It executes the updates within a transaction to ensure consistency.
func (db *PostgresDB) RemoveUsersUrls(
	ctx context.Context,
	usersURLs map[string][]string,
) error {
	transaction, err := db.database.Begin()
	if err != nil {
		return err
	}

	qtx := db.queries.WithTx(transaction)

	for userID, urls := range usersURLs {
		for _, url := range urls {
			userIDAsUUID, err := uuid.Parse(userID)
			if err != nil {
				return err
			}
			err = qtx.RemoveUsersUrls(ctx, sqlc.RemoveUsersUrlsParams{
				UserID:   userIDAsUUID,
				ShortUrl: url,
			})
			if err != nil {
				err2 := transaction.Rollback()
				if err2 != nil {
					return err2
				}
				return err
			}
		}
	}

	err = transaction.Commit()
	if err != nil {
		return err
	}

	return nil
}

// SaveUserUrls stores mappings between a user and a list of full URLs.
// It uses an UPSERT strategy and runs within an existing transaction.
func (db *PostgresDB) SaveUserUrls(
	ctx context.Context,
	userID string,
	urls []string,
	transaction *sql.Tx,
) error {
	qtx := db.queries.WithTx(transaction)

	for _, url := range urls {
		userIDAsUUID, err := uuid.Parse(userID)
		if err != nil {
			return err
		}
		err = qtx.SaveUserUrl(ctx, sqlc.SaveUserUrlParams{
			UserID: userIDAsUUID,
			Url:    url,
		})
		if err != nil {
			return err
		}
	}

	return nil
}

// GetUserUrls retrieves all short-to-full URL mappings for a given user.
// Optionally applies a formatter to each short URL before returning.
func (db *PostgresDB) GetUserUrls(
	ctx context.Context,
	userID string,
	shortURLFormatter models.URLFormatter,
) (models.UserUrls, error) {
	formatter := func(str string) string { return str }
	if shortURLFormatter != nil {
		formatter = shortURLFormatter
	}

	userIDAsUUID, err := uuid.Parse(userID)
	if err != nil {
		return nil, err
	}

	rows, err := db.queries.GetUserUrls(ctx, userIDAsUUID)
	if err != nil {
		return nil, err
	}

	result := models.UserUrls{}
	for _, row := range rows {
		result = append(result, models.UserURL{
			ShortURL:    formatter(row.Short),
			OriginalURL: row.OriginalUrl,
		})
	}

	return result, nil
}

// CreateUser inserts a new user record into the database.
// Returns the created user ID or an error if insertion fails.
func (db *PostgresDB) CreateUser(ctx context.Context, usr *user.User, transaction *sql.Tx) (string, error) {
	var queries *sqlc.Queries
	if transaction != nil {
		queries = db.queries.WithTx(transaction)
	} else {
		queries = db.queries
	}

	userID, err := queries.CreateUser(ctx)
	if err != nil {
		return "", err
	}

	return userID.String(), nil
}

// GetUserByID fetches a user by their UUID from the database.
// If the user does not exist, it returns a user with an empty ID field.
func (db *PostgresDB) GetUserByID(ctx context.Context, userID string, transaction *sql.Tx) (*user.User, error) {
	if userID == "" {
		return &user.User{ID: ""}, nil
	}

	var queries *sqlc.Queries
	if transaction != nil {
		queries = db.queries.WithTx(transaction)
	} else {
		queries = db.queries
	}

	userIDAsUUID, err := uuid.Parse(userID)
	if err != nil {
		return nil, err
	}

	userIDFromDB, err := queries.GetUserByID(ctx, userIDAsUUID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return &user.User{ID: ""}, nil
		}
		return &user.User{ID: ""}, err
	}

	return &user.User{ID: userIDFromDB.String()}, nil
}

// CommitTransaction commits the given SQL transaction.
// Returns an error if the commit operation fails.
func (db *PostgresDB) CommitTransaction(transaction *sql.Tx) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic occurred while committing transaction: %v", r)
		}
	}()

	return transaction.Commit()
}

// RollbackTransaction rolls back the given SQL transaction.
// If rollback fails, the returned error describes the issue.
func (db *PostgresDB) RollbackTransaction(transaction *sql.Tx) error {
	return transaction.Rollback()
}

// BeginTransaction starts a new SQL transaction and returns it.
// The caller is responsible for committing or rolling it back.
func (db *PostgresDB) BeginTransaction() (*sql.Tx, error) {
	return db.database.Begin()
}

// SaveNewFullsAndShorts stores a set of full-to-short URL mappings that
// do not yet exist in the database. It is used to avoid duplicate inserts.
// This operation is performed within the provided transaction.
func (db *PostgresDB) SaveNewFullsAndShorts(
	ctx context.Context,
	newURLs map[string]string,
	transaction *sql.Tx,
) error {
	if len(newURLs) == 0 {
		return nil
	}

	var queries *sqlc.Queries
	if transaction != nil {
		queries = db.queries.WithTx(transaction)
	} else {
		queries = db.queries
	}

	for full, short := range newURLs {
		err := queries.SaveURLMapping(ctx, sqlc.SaveURLMappingParams{
			Short:       short,
			OriginalUrl: full,
		})
		if err != nil {
			return err
		}
	}

	return nil
}

// FindShortsByFulls returns a mapping from full URLs to their corresponding
// short URLs for the given input list. If a URL does not exist, it will be omitted.
func (db *PostgresDB) FindShortsByFulls(
	ctx context.Context,
	urls []string,
	transaction *sql.Tx,
) (map[string]string, error) {
	if len(urls) == 0 {
		return map[string]string{}, nil
	}

	var queries *sqlc.Queries
	if transaction != nil {
		queries = db.queries.WithTx(transaction)
	} else {
		queries = db.queries
	}

	rows, err := queries.FindShortsByFulls(ctx, urls)
	if err != nil {
		return nil, err
	}

	result := make(map[string]string, len(rows))
	for _, row := range rows {
		result[row.OriginalUrl] = row.Short
	}

	return result, nil
}

// InsertURLMapping creates a new short-to-full URL mapping in the database.
func (db *PostgresDB) InsertURLMapping(
	ctx context.Context,
	short,
	full string,
	transaction *sql.Tx,
) error {
	var queries *sqlc.Queries
	if transaction != nil {
		queries = db.queries.WithTx(transaction)
	} else {
		queries = db.queries
	}

	err := queries.InsertURLMapping(ctx, sqlc.InsertURLMappingParams{
		Short:       short,
		OriginalUrl: full,
	})

	return err
}

// FindFullByShort retrieves the full URL associated with the given short URL.
// If the short URL is marked as deleted, it returns true and an error.
func (db *PostgresDB) FindFullByShort(ctx context.Context, short string) (string, bool, error) {
	row, err := db.queries.FindFullByShort(ctx, short)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}

	if row.IsDeleted {
		return row.OriginalUrl, true, models.ErrURLMarkedAsDeleted
	}

	return row.OriginalUrl, true, nil
}

// FindShortByFull retrieves the short URL corresponding to the given full URL.
// Returns a boolean indicating presence and an error if applicable.
func (db *PostgresDB) FindShortByFull(
	ctx context.Context,
	full string,
	transaction *sql.Tx,
) (string, bool, error) {
	var queries *sqlc.Queries
	if transaction != nil {
		queries = db.queries.WithTx(transaction)
	} else {
		queries = db.queries
	}

	short, err := queries.FindShortByFull(ctx, full)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}

	return short, true, nil
}

// IsShortExists checks if the specified short URL exists in the database.
func (db *PostgresDB) IsShortExists(ctx context.Context, short string) (bool, error) {
	return db.queries.IsShortExists(ctx, short)
}

// InitOption defines a functional option for configuring database initialization.
type InitOption func(*initOptions)

// WithDBPreReset enables or disables resetting the database schema before migration.
// It can be used for test setups or development purposes.
func WithDBPreReset(value bool) InitOption {
	return func(options *initOptions) {
		options.DBPreReset = value
	}
}

// Ping verifies connectivity with the PostgreSQL database within the configured timeout.
func (db *PostgresDB) Ping(ctx context.Context) error {
	ctxWithTimeout, cancel := context.WithTimeout(ctx, db.connectionTimeout)
	defer cancel()

	return db.database.PingContext(ctxWithTimeout)
}

// Close closes the database connection and releases any associated resources.
func (db *PostgresDB) Close() error {
	err := db.database.Close()
	if err != nil {
		return err
	}

	return nil
}

// GetNumberOfUsers returns the total number of user records
// in the "users" table of the PostgreSQL database.
func (db *PostgresDB) GetNumberOfUsers(ctx context.Context) (int64, error) {
	return db.queries.GetNumberOfUsers(ctx)
}

// GetNumberOfShortenedURLs returns the total count of shortened URLs
// that have not been marked as deleted in the "url_redirects" table.
func (db *PostgresDB) GetNumberOfShortenedURLs(ctx context.Context) (int64, error) {
	return db.queries.GetNumberOfShortenedURLs(ctx)
}

func (db *PostgresDB) resetDB(ctx context.Context) error {
	err := db.queries.ResetDB(ctx)
	if err != nil {
		return fmt.Errorf(
			"in internal/db/postgresdb/postgresdb.go/resetDB(): error while db.queries.ResetDB() calling: %w",
			err,
		)
	}

	return nil
}
