package postgresdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/thoas/go-funk"
	"strings"
	"time"
)

type PostgresDB struct {
	database *sql.DB
	config   Config
}

type Config struct {
	FileStoragePath   string
	DatabaseDSN       string
	ConnectionTimeout time.Duration
}

const (
	ShortToFullURLMapTableName = "short_to_full_url_map_s1ble3"
)

type queryer interface {
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
}

type executor interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
}

func (pgdb *PostgresDB) BeginTransaction() (*sql.Tx, error) {
	return pgdb.database.Begin()
}

func getShortToFullURLMapTableValues(unexistentFullsToShortsMap map[string]string) [][]string {
	result := [][]string{}
	for full, short := range unexistentFullsToShortsMap {
		result = append(result, []string{short, full})
	}

	return result
}

func (pgdb *PostgresDB) SaveNewFullsAndShorts(
	outerCtx context.Context,
	unexistentFullsToShortsMap map[string]string,
	transaction *sql.Tx,
) error {
	shortToFullURLMapTableValues := getShortToFullURLMapTableValues(unexistentFullsToShortsMap)
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
		database = pgdb.database
	} else {
		database = transaction
	}

	_, err := database.ExecContext(
		outerCtx,
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

func (pgdb *PostgresDB) FindShortsByFulls(
	outerCtx context.Context,
	originalUrls []string,
	transaction *sql.Tx,
) (map[string]string, error) {
	originalUrlsLen := len(originalUrls)
	if originalUrlsLen == 0 {
		return map[string]string{}, nil
	}
	originalUrlsPlaceholdersSlice := make([]string, originalUrlsLen)
	for i := range originalUrls {
		originalUrlsPlaceholdersSlice[i] = fmt.Sprintf("$%d", i+1)
	}
	originalUrlsPlaceholders := strings.Join(originalUrlsPlaceholdersSlice, ",")

	var database queryer

	if transaction == nil {
		database = pgdb.database
	} else {
		database = transaction
	}

	rows, err := database.QueryContext(
		outerCtx,
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
		}(originalUrls)...,
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

func (pgdb *PostgresDB) Insert(outerCtx context.Context, short, full string) error {
	_, err := pgdb.database.ExecContext(
		outerCtx,
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

func (pgdb *PostgresDB) FindFullByShort(outerCtx context.Context, short string) (string, bool, error) {
	row := pgdb.database.QueryRowContext(
		outerCtx,
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

func (pgdb *PostgresDB) FindShortByFull(outerCtx context.Context, full string) (string, bool, error) {
	row := pgdb.database.QueryRowContext(
		outerCtx,
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

func (pgdb *PostgresDB) IsShortExists(outerCtx context.Context, short string) (bool, error) {
	row := pgdb.database.QueryRowContext(
		outerCtx,
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

func (pgdb *PostgresDB) createDBStructure(outerCtx context.Context) error {
	_, err := pgdb.database.ExecContext(
		outerCtx,
		fmt.Sprintf(
			`
				CREATE TABLE "%s" (
					"short" VARCHAR(255) NOT NULL,
					"full" VARCHAR(255) NOT NULL,
					CONSTRAINT pk_full PRIMARY KEY ("full"),
					CONSTRAINT uq_short UNIQUE ("short")
				)
			`,
			ShortToFullURLMapTableName,
		),
	)
	if err != nil {
		return err
	}

	return nil
}

func (pgdb *PostgresDB) checkDBStructure(outerCtx context.Context) (bool, error) {
	row := pgdb.database.QueryRowContext(
		outerCtx,
		`
			SELECT COUNT(*)
				FROM pg_tables 
				WHERE schemaname = 'public' AND tablename = $1
		`,
		ShortToFullURLMapTableName,
	)
	var amountOfExistentTables int
	err := row.Scan(&amountOfExistentTables)
	if err != nil {
		return false, err
	}

	return amountOfExistentTables > 0, nil
}

func (pgdb *PostgresDB) checkOrCreateDBStructure(outerCtx context.Context) error {
	isDBStructureOk, err := pgdb.checkDBStructure(outerCtx)
	if err != nil {
		return err
	}
	if !isDBStructureOk {
		err := pgdb.createDBStructure(outerCtx)
		if err != nil {
			return err
		}
	}

	return nil
}

func New(outerCtx context.Context, config Config) (*PostgresDB, error) {
	database, err := sql.Open("pgx", config.DatabaseDSN)
	if err != nil {
		return nil, err
	}

	result := &PostgresDB{
		database: database,
		config:   config,
	}

	err = result.checkOrCreateDBStructure(outerCtx)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (pgdb *PostgresDB) Ping(outerCtx context.Context) error {
	ctxWithTimeout, cancel := context.WithTimeout(outerCtx, pgdb.config.ConnectionTimeout*time.Second)
	defer cancel()

	return pgdb.database.PingContext(ctxWithTimeout)
}

func (pgdb *PostgresDB) Close() error {
	err := pgdb.database.Close()
	if err != nil {
		return err
	}

	return nil
}
