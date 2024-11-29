package postgresdb

import (
	"context"
	"database/sql"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/patric-chuzhbe/urlshrt/internal/simplejsondb"
	"time"
)

type PostgresDB struct {
	*simplejsondb.SimpleJSONDB
	database *sql.DB
	config   Config
}

type Config struct {
	FileStoragePath   string
	DatabaseDSN       string
	ConnectionTimeout time.Duration
}

func New(config Config) (*PostgresDB, error) {
	simpleDB, err := simplejsondb.New(config.FileStoragePath)
	if err != nil {
		return nil, err
	}

	database, err := sql.Open("pgx", config.DatabaseDSN)
	if err != nil {
		return nil, err
	}

	return &PostgresDB{
		SimpleJSONDB: simpleDB,
		database:     database,
		config:       config,
	}, nil
}

func (pgdb *PostgresDB) Ping(outerCtx context.Context) error {
	ctxWithTimeout, cancel := context.WithTimeout(outerCtx, pgdb.config.ConnectionTimeout*time.Second)
	defer cancel()

	return pgdb.database.PingContext(ctxWithTimeout)
}

func (pgdb *PostgresDB) Close() error {
	err := pgdb.SimpleJSONDB.Close()
	if err != nil {
		return err
	}

	err = pgdb.database.Close()
	if err != nil {
		return err
	}

	return nil
}
