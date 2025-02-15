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
	"strconv"
	"strings"
	"time"
)

type PostgresDB struct {
	database          *sql.DB
	connectionTimeout time.Duration
}

const (
	ShortToFullURLMapTableName = "short_to_full_url_map_s1ble3"
)

type queryer interface {
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type executor interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
}

func getUsersUrlsTableValues(userID int, urls []string) [][]string {
	result := [][]string{}
	for _, url := range urls {
		result = append(result, []string{strconv.Itoa(userID), url})
	}

	return result
}

func (db *PostgresDB) SaveUserUrls(
	ctx context.Context,
	userID int,
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
	userID int,
	shortURLFormatter func(string) string,
) (models.UserUrls, error) {
	formatter := func(str string) string { return str }
	if shortURLFormatter != nil {
		formatter = shortURLFormatter
	}

	rows, err := db.database.QueryContext(
		ctx,
		`
			SELECT short_to_full_url_map_s1ble3.full, short_to_full_url_map_s1ble3.short 
				FROM short_to_full_url_map_s1ble3
					JOIN users_urls ON users_urls.url = short_to_full_url_map_s1ble3.full
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

func (db *PostgresDB) CreateUser(ctx context.Context, usr *user.User, transaction *sql.Tx) (int, error) {
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
	var userIDFromDB int
	err := row.Scan(&userIDFromDB)
	if err != nil {
		return 0, err
	}

	return userIDFromDB, nil
}

func (db *PostgresDB) GetUserByID(ctx context.Context, userID int, transaction *sql.Tx) (*user.User, error) {
	var database queryer

	if transaction == nil {
		database = db.database
	} else {
		database = transaction
	}

	row := database.QueryRowContext(
		ctx,
		`SELECT id FROM users WHERE id = $1`,
		userID,
	)
	var userIDFromDB int
	err := row.Scan(&userIDFromDB)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return &user.User{ID: 0}, nil
		}
		return &user.User{ID: 0}, err
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
			`INSERT INTO "%s" ("short", "full") VALUES %s`,
			ShortToFullURLMapTableName,
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
			`SELECT "short", "full" FROM "%s" WHERE "full" IN (%s)`,
			ShortToFullURLMapTableName,
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
		fmt.Sprintf(
			`INSERT INTO "%s" ("short", "full") VALUES ($1, $2)`,
			ShortToFullURLMapTableName,
		),
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
		fmt.Sprintf(
			`SELECT "full" FROM "%s" WHERE "short" = $1`,
			ShortToFullURLMapTableName,
		),
		short,
	)
	var full string
	err := row.Scan(&full)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
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
		fmt.Sprintf(
			`SELECT "short" FROM "%s" WHERE "full" = $1`,
			ShortToFullURLMapTableName,
		),
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
		fmt.Sprintf(
			`SELECT COUNT(*) FROM "%s" WHERE "short" = $1`,
			ShortToFullURLMapTableName,
		),
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
		fmt.Sprintf(
			`
				CREATE TABLE "%s" (
					"short" VARCHAR(255) NOT NULL,
					"full" VARCHAR(255) NOT NULL,
					CONSTRAINT pk_full PRIMARY KEY ("full"),
					CONSTRAINT uq_short UNIQUE ("short")
				);
				CREATE TABLE users (
				   id SERIAL NOT NULL,
				   CONSTRAINT PK_USERS PRIMARY KEY (id)
				);
				CREATE TABLE users_urls (
				   user_id INT4 NOT NULL,
				   url VARCHAR(255) NOT NULL,
				   CONSTRAINT PK_USERS_URLS PRIMARY KEY (user_id, url)
				);
				ALTER TABLE users_urls
				   ADD CONSTRAINT FK_USERS_UR_REFERENCE_USERS FOREIGN KEY (user_id)
					  REFERENCES USERS (id)
					  ON DELETE CASCADE ON UPDATE CASCADE;
				ALTER TABLE users_urls
				   ADD CONSTRAINT FK_USERS_UR_REFERENCE_SHORT_TO FOREIGN KEY (url)
					  REFERENCES "%s" ("full")
					  ON DELETE CASCADE ON UPDATE CASCADE;
			`,
			ShortToFullURLMapTableName,
			ShortToFullURLMapTableName,
		),
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
				WHERE schemaname = 'public' AND tablename = $1
		`,
		ShortToFullURLMapTableName,
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
	ctxWithTimeout, cancel := context.WithTimeout(ctx, db.connectionTimeout*time.Second)
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
