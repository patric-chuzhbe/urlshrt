package postgresdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/patric-chuzhbe/urlshrt/internal/models"
	"github.com/patric-chuzhbe/urlshrt/internal/user"
	"github.com/thoas/go-funk"
	"strings"
	"time"
)

type PostgresDB struct {
	database          *sql.DB
	connectionTimeout time.Duration
}

type queryer interface {
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type executor interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
}

func getUsersUrlsForRemoving(usersURLs map[string][]string) [][]string {
	result := [][]string{}
	for userID, urls := range usersURLs {
		for _, url := range urls {
			result = append(result, []string{userID, url})
		}
	}

	return result
}

func (db *PostgresDB) RemoveUsersUrls(
	ctx context.Context,
	usersURLs map[string][]string,
) error {
	usersUrlsForRemoving := getUsersUrlsForRemoving(usersURLs)
	if len(usersUrlsForRemoving) == 0 {
		return nil
	}
	usersURLsPlaceholders := make([]string, len(usersUrlsForRemoving))
	for i := range usersURLsPlaceholders {
		usersURLsPlaceholders[i] = fmt.Sprintf("($%d, $%d)", i*2+1, i*2+2)
	}
	usersURLsPlaceholdersAsStr := strings.Join(usersURLsPlaceholders, ",")
	queryParams := funk.Flatten(usersUrlsForRemoving).([]string)
	_, err := db.database.ExecContext(
		ctx,
		fmt.Sprintf(
			`
				UPDATE short_to_full_url_map
					SET is_deleted = true
					FROM users_urls
					WHERE short_to_full_url_map.full = users_urls.url
						AND (users_urls.user_id, short_to_full_url_map.short) IN (%s)
			`,
			usersURLsPlaceholdersAsStr,
		),
		func(strSlice []string) []interface{} {
			result := make([]interface{}, len(strSlice))
			for i, v := range strSlice {
				result[i] = v
			}

			return result
		}(queryParams)...,
	)
	if err != nil {
		return err
	}

	return nil
}

func getUsersUrlsTableValues(userID string, urls []string) [][]string {
	result := [][]string{}
	for _, url := range urls {
		result = append(result, []string{userID, url})
	}

	return result
}

func (db *PostgresDB) SaveUserUrls(
	ctx context.Context,
	userID string,
	urls []string,
	transaction *sql.Tx,
) error {
	var database executor
	if transaction == nil {
		database = db.database
	} else {
		database = transaction
	}
	usersUrlsTableValues := getUsersUrlsTableValues(userID, urls)
	if len(usersUrlsTableValues) == 0 {
		return nil
	}
	usersUrlsTableValuesPlaceholders := make([]string, len(usersUrlsTableValues))
	for i := range usersUrlsTableValuesPlaceholders {
		usersUrlsTableValuesPlaceholders[i] = fmt.Sprintf("($%d, $%d)", i*2+1, i*2+2)
	}
	usersUrlsTableValuesPlaceholdersAsString := strings.Join(usersUrlsTableValuesPlaceholders, ",")
	queryParams := funk.Flatten(usersUrlsTableValues).([]string)
	_, err := database.ExecContext(
		ctx,
		fmt.Sprintf(
			`
				INSERT INTO users_urls (user_id, url)
					VALUES %s
					ON CONFLICT (user_id, url) DO UPDATE
					SET 
						user_id = EXCLUDED.user_id, 
						url = EXCLUDED.url;
			`,
			usersUrlsTableValuesPlaceholdersAsString,
		),
		func(strSlice []string) []interface{} {
			result := make([]interface{}, len(strSlice))
			for i, v := range strSlice {
				result[i] = v
			}

			return result
		}(queryParams)...,
	)
	if err != nil {
		return err
	}

	return nil
}

func (db *PostgresDB) GetUserUrls(
	ctx context.Context,
	userID string,
	shortURLFormatter func(string) string,
) (models.UserUrls, error) {
	formatter := func(str string) string { return str }
	if shortURLFormatter != nil {
		formatter = shortURLFormatter
	}

	rows, err := db.database.QueryContext(
		ctx,
		`
			SELECT short_to_full_url_map.full, short_to_full_url_map.short 
				FROM short_to_full_url_map
					JOIN users_urls ON users_urls.url = short_to_full_url_map.full
						AND users_urls.user_id = $1
		`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := models.UserUrls{}
	for rows.Next() {
		var short, full string
		err = rows.Scan(&full, &short)
		if err != nil {
			return nil, err
		}

		result = append(
			result,
			models.UserURL{
				ShortURL:    formatter(short),
				OriginalURL: full,
			},
		)
	}

	err = rows.Err()
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (db *PostgresDB) CreateUser(ctx context.Context, usr *user.User, transaction *sql.Tx) (string, error) {
	var database queryer
	if transaction == nil {
		database = db.database
	} else {
		database = transaction
	}

	row := database.QueryRowContext(
		ctx,
		`INSERT INTO users DEFAULT VALUES RETURNING id`,
	)
	var userIDFromDB string
	err := row.Scan(&userIDFromDB)
	if err != nil {
		return "", err
	}

	return userIDFromDB, nil
}

func (db *PostgresDB) GetUserByID(ctx context.Context, userID string, transaction *sql.Tx) (*user.User, error) {
	var database queryer

	if transaction == nil {
		database = db.database
	} else {
		database = transaction
	}

	if userID == "" {
		return &user.User{ID: ""}, nil
	}

	row := database.QueryRowContext(
		ctx,
		`SELECT id FROM users WHERE id = $1`,
		userID,
	)
	var userIDFromDB string
	err := row.Scan(&userIDFromDB)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return &user.User{ID: ""}, nil
		}
		return &user.User{ID: ""}, err
	}

	return &user.User{ID: userIDFromDB}, nil
}

func (db *PostgresDB) CommitTransaction(transaction *sql.Tx) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic occurred while committing transaction: %v", r)
		}
	}()

	return transaction.Commit()
}

func (db *PostgresDB) RollbackTransaction(transaction *sql.Tx) error {
	return transaction.Rollback()
}

func (db *PostgresDB) BeginTransaction() (*sql.Tx, error) {
	return db.database.Begin()
}

func prepareURLMapping(newURLs map[string]string) [][]string {
	result := [][]string{}
	for full, short := range newURLs {
		result = append(result, []string{short, full})
	}

	return result
}

func (db *PostgresDB) SaveNewFullsAndShorts(
	ctx context.Context,
	newURLs map[string]string,
	transaction *sql.Tx,
) error {
	shortToFullURLMapTableValues := prepareURLMapping(newURLs)
	shortToFullURLMapTableValuesLen := len(shortToFullURLMapTableValues)
	if shortToFullURLMapTableValuesLen == 0 {
		return nil
	}
	shortToFullURLMapTableValuesPlaceholders := make([]string, len(shortToFullURLMapTableValues))
	for i := range shortToFullURLMapTableValuesPlaceholders {
		shortToFullURLMapTableValuesPlaceholders[i] = fmt.Sprintf("($%d, $%d)", i*2+1, i*2+2)
	}
	shortToFullURLMapTableValuesPlaceholdersAsString := strings.Join(shortToFullURLMapTableValuesPlaceholders, ",")
	queryParams := funk.Flatten(shortToFullURLMapTableValues).([]string)

	var database executor
	if transaction == nil {
		database = db.database
	} else {
		database = transaction
	}

	_, err := database.ExecContext(
		ctx,
		fmt.Sprintf(
			`INSERT INTO short_to_full_url_map ("short", "full") VALUES %s`,
			shortToFullURLMapTableValuesPlaceholdersAsString,
		),
		func(strSlice []string) []interface{} {
			result := make([]interface{}, len(strSlice))
			for i, v := range strSlice {
				result[i] = v
			}

			return result
		}(queryParams)...,
	)
	if err != nil {
		return err
	}

	return nil
}

func (db *PostgresDB) FindShortsByFulls(
	ctx context.Context,
	urls []string,
	transaction *sql.Tx,
) (map[string]string, error) {
	originalUrlsLen := len(urls)
	if originalUrlsLen == 0 {
		return map[string]string{}, nil
	}
	originalUrlsPlaceholdersSlice := make([]string, originalUrlsLen)
	for i := range urls {
		originalUrlsPlaceholdersSlice[i] = fmt.Sprintf("$%d", i+1)
	}
	originalUrlsPlaceholders := strings.Join(originalUrlsPlaceholdersSlice, ",")

	var database queryer

	if transaction == nil {
		database = db.database
	} else {
		database = transaction
	}

	rows, err := database.QueryContext(
		ctx,
		fmt.Sprintf(
			`SELECT "short", "full" FROM short_to_full_url_map WHERE "full" IN (%s)`,
			originalUrlsPlaceholders,
		),
		func(strSlice []string) []interface{} {
			result := make([]interface{}, len(strSlice))
			for i, v := range strSlice {
				result[i] = v
			}

			return result
		}(urls)...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := map[string]string{}
	for rows.Next() {
		var short, full string
		err = rows.Scan(&short, &full)
		if err != nil {
			return nil, err
		}

		result[full] = short
	}

	err = rows.Err()
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (db *PostgresDB) InsertURLMapping(
	ctx context.Context,
	short,
	full string,
	transaction *sql.Tx,
) error {
	var database executor

	if transaction == nil {
		database = db.database
	} else {
		database = transaction
	}

	_, err := database.ExecContext(
		ctx,
		`INSERT INTO short_to_full_url_map ("short", "full") VALUES ($1, $2)`,
		short,
		full,
	)
	if err != nil {
		return err
	}

	return nil
}

func (db *PostgresDB) FindFullByShort(ctx context.Context, short string) (string, bool, error) {
	row := db.database.QueryRowContext(
		ctx,
		`SELECT "full", is_deleted FROM short_to_full_url_map WHERE "short" = $1`,
		short,
	)
	var full string
	var isDeleted bool
	err := row.Scan(&full, &isDeleted)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}

	if isDeleted {
		return full, true, models.ErrURLMarkedAsDeleted
	}

	return full, true, nil
}

func (db *PostgresDB) FindShortByFull(
	ctx context.Context,
	full string,
	transaction *sql.Tx,
) (string, bool, error) {
	var database queryer

	if transaction == nil {
		database = db.database
	} else {
		database = transaction
	}

	row := database.QueryRowContext(
		ctx,
		`SELECT "short" FROM short_to_full_url_map WHERE "full" = $1`,
		full,
	)
	var short string
	err := row.Scan(&short)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}

	return short, true, nil
}

func (db *PostgresDB) IsShortExists(ctx context.Context, short string) (bool, error) {
	row := db.database.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM short_to_full_url_map WHERE "short" = $1`,
		short,
	)
	var shortCount int
	err := row.Scan(&shortCount)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}

		return false, err
	}

	return shortCount > 0, nil
}

func (db *PostgresDB) createDBStructure(ctx context.Context) error {
	_, err := db.database.ExecContext(
		ctx,
		`
			CREATE TABLE short_to_full_url_map (
			   "full"               VARCHAR(255)         NOT NULL,
			   short                VARCHAR(255)         NOT NULL,
			   is_deleted           BOOL                 NOT NULL DEFAULT FALSE,
			   CONSTRAINT PK_SHORT_TO_FULL_URL_MAP PRIMARY KEY ("full")
			);
			
			CREATE UNIQUE INDEX uq_short ON short_to_full_url_map (short);
			
			CREATE TABLE USERS (
			   id UUID NOT NULL DEFAULT gen_random_uuid(),
			   CONSTRAINT PK_USERS PRIMARY KEY (id)
			);
			
			CREATE TABLE users_urls (
			   user_id              UUID                 NOT NULL,
			   url                  VARCHAR(255)         NOT NULL,
			   CONSTRAINT PK_USERS_URLS PRIMARY KEY (user_id, url)
			);
			
			ALTER TABLE users_urls
			   ADD CONSTRAINT FK_USERS_UR_REFERENCE_USERS FOREIGN KEY (user_id)
				  REFERENCES users (id)
				  ON DELETE CASCADE ON UPDATE CASCADE;
			
			ALTER TABLE users_urls
			   ADD CONSTRAINT FK_USERS_UR_REFERENCE_SHORT_TO FOREIGN KEY (url)
				  REFERENCES short_to_full_url_map ("full")
				  ON DELETE CASCADE ON UPDATE CASCADE;
		`,
	)
	if err != nil {
		return err
	}

	return nil
}

func (db *PostgresDB) checkDBStructure(ctx context.Context) (bool, error) {
	row := db.database.QueryRowContext(
		ctx,
		`
			SELECT COUNT(*)
				FROM pg_tables 
				WHERE schemaname = 'public' AND tablename = 'short_to_full_url_map'
		`,
	)
	var tableCount int
	err := row.Scan(&tableCount)
	if err != nil {
		return false, err
	}

	return tableCount > 0, nil
}

func (db *PostgresDB) checkOrCreateDBStructure(ctx context.Context) error {
	isDBStructureOk, err := db.checkDBStructure(ctx)
	if err != nil {
		return err
	}
	if !isDBStructureOk {
		err := db.createDBStructure(ctx)
		if err != nil {
			return err
		}
	}

	return nil
}

func New(
	ctx context.Context,
	databaseDSN string,
	connectionTimeout time.Duration,
) (*PostgresDB, error) {
	database, err := sql.Open("pgx", databaseDSN)
	if err != nil {
		return nil, err
	}

	result := &PostgresDB{
		database:          database,
		connectionTimeout: connectionTimeout,
	}

	err = result.checkOrCreateDBStructure(ctx)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (db *PostgresDB) Ping(ctx context.Context) error {
	ctxWithTimeout, cancel := context.WithTimeout(ctx, db.connectionTimeout)
	defer cancel()

	return db.database.PingContext(ctxWithTimeout)
}

func (db *PostgresDB) Close() error {
	err := db.database.Close()
	if err != nil {
		return err
	}

	return nil
}
