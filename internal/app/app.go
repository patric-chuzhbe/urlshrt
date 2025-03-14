package app

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/patric-chuzhbe/urlshrt/internal/auth"
	"github.com/patric-chuzhbe/urlshrt/internal/config"
	"github.com/patric-chuzhbe/urlshrt/internal/db/jsondb"
	"github.com/patric-chuzhbe/urlshrt/internal/db/memorystorage"
	"github.com/patric-chuzhbe/urlshrt/internal/db/postgresdb"
	"github.com/patric-chuzhbe/urlshrt/internal/logger"
	"github.com/patric-chuzhbe/urlshrt/internal/models"
	"github.com/patric-chuzhbe/urlshrt/internal/router"
	"github.com/patric-chuzhbe/urlshrt/internal/urlsremover"
	"github.com/patric-chuzhbe/urlshrt/internal/user"
	"go.uber.org/zap"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

type userKeeper interface {
	CreateUser(ctx context.Context, usr *user.User, transaction *sql.Tx) (string, error)
	GetUserByID(ctx context.Context, userID string, transaction *sql.Tx) (*user.User, error)
}

type userUrlsKeeper interface {
	GetUserUrls(
		ctx context.Context,
		userID string,
		shortURLFormatter func(string) string,
	) (models.UserUrls, error)

	SaveUserUrls(
		ctx context.Context,
		userID string,
		urls []string,
		transaction *sql.Tx,
	) error

	RemoveUsersUrls(
		ctx context.Context,
		usersURLs map[string][]string,
	) error
}

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

type pinger interface {
	Ping(ctx context.Context) error
}

type storage interface {
	userKeeper
	userUrlsKeeper
	transactioner
	urlsMapper
	pinger
	Close() error
}

type App struct {
	cfg             *config.Config
	db              storage
	urlsRemover     *urlsremover.UrlsRemover
	stopUrlsRemover context.CancelFunc
	httpHandler     http.Handler
}

func getAvailableStorageType(cfg *config.Config) int {
	if cfg.DatabaseDSN != "" {
		return models.StorageTypePostgresql
	}

	if cfg.DBFileName != "" {
		return models.StorageTypeFile
	}

	return models.StorageTypeMemory
}

func getStorageByType(cfg *config.Config) (storage, error) {
	switch getAvailableStorageType(cfg) {
	case models.StorageTypeUnknown:
		return nil, errors.New("unknown storage type")

	case models.StorageTypePostgresql:
		return postgresdb.New(
			context.Background(),
			cfg.DatabaseDSN,
			cfg.DBConnectionTimeout,
		)

	case models.StorageTypeFile:
		return jsondb.New(cfg.DBFileName)
	}

	return memorystorage.New()
}

func New() (*App, error) {
	var err error
	app := &App{}

	app.cfg, err = config.New()
	if err != nil {
		return nil, err
	}

	err = logger.Init(app.cfg.LogLevel)
	if err != nil {
		return nil, err
	}

	app.db, err = getStorageByType(app.cfg)
	if err != nil {
		return nil, err
	}

	authCookieSigningSecretKey, err := base64.URLEncoding.DecodeString(app.cfg.AuthCookieSigningSecretKey)
	if err != nil {
		return nil, err
	}

	app.urlsRemover = urlsremover.New(
		app.db,
		app.cfg.ChannelCapacity,
		app.cfg.DelayBetweenQueueFetches,
	)
	urlsRemoverRunCtx, stopUrlsRemover := context.WithCancel(context.Background())
	app.stopUrlsRemover = stopUrlsRemover

	app.urlsRemover.Run(urlsRemoverRunCtx)
	app.urlsRemover.ListenErrors(func(err error) {
		logger.Log.Debugln("Error passed from the `app.urlsRemover.ListenErrors()`:", zap.Error(err))
	})

	app.httpHandler = router.New(
		app.db,
		app.cfg.ShortURLBase,
		auth.New(
			app.db,
			app.cfg.AuthCookieName,
			authCookieSigningSecretKey,
		),
		app.urlsRemover,
	)

	return app, nil
}

func (a *App) Run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logger.Log.Infoln("server running", "RunAddr", a.cfg.RunAddr)

	server := &http.Server{
		Addr:    a.cfg.RunAddr,
		Handler: a.httpHandler,
	}

	serverErrCh := make(chan error, 1)
	go func() {
		serverErrCh <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		logger.Log.Infoln("Received shutdown signal. Saving database and exiting...")
		a.stopUrlsRemover()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("server shutdown error: %w", err)
		}

		return a.db.Close()

	case err := <-serverErrCh:
		return fmt.Errorf("server error: %w", err)
	}
}

func (a *App) Close() {
	if err := logger.Sync(); err != nil {
		fmt.Println("Logger sync error:", err)
	}
}
