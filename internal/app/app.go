// Package app initializes and runs the main application service.
// It configures logging, Storage, authentication, and routing,
// and handles graceful shutdown.
package app

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"

	"github.com/patric-chuzhbe/urlshrt/internal/grpcserver"

	"github.com/patric-chuzhbe/urlshrt/internal/service"

	"github.com/patric-chuzhbe/urlshrt/internal/ipchecker"

	"go.uber.org/zap"

	"github.com/patric-chuzhbe/urlshrt/internal/auth"
	"github.com/patric-chuzhbe/urlshrt/internal/router"

	"github.com/patric-chuzhbe/urlshrt/internal/config"
	"github.com/patric-chuzhbe/urlshrt/internal/db/jsondb"
	"github.com/patric-chuzhbe/urlshrt/internal/db/memorystorage"
	"github.com/patric-chuzhbe/urlshrt/internal/db/postgresdb"
	"github.com/patric-chuzhbe/urlshrt/internal/logger"
	"github.com/patric-chuzhbe/urlshrt/internal/models"
	"github.com/patric-chuzhbe/urlshrt/internal/urlsremover"
	"github.com/patric-chuzhbe/urlshrt/internal/user"
)

// UserKeeper is an interface for handling user-related operations
// such as creating and retrieving users.
type UserKeeper interface {
	// CreateUser generates a new user ID, stores the user, and returns the ID.
	CreateUser(ctx context.Context, usr *user.User, transaction *sql.Tx) (string, error)

	// GetUserByID retrieves a user by their ID. If not found, returns a user with an empty ID.
	GetUserByID(ctx context.Context, userID string, transaction *sql.Tx) (*user.User, error)
}

// UserUrlsKeeper is an interface that defines methods for managing URLs associated with users.
type UserUrlsKeeper interface {
	// GetUserUrls retrieves all short-to-full URL mappings for a given user.
	// Optionally applies a formatter to each short URL before returning.
	GetUserUrls(
		ctx context.Context,
		userID string,
		shortURLFormatter models.URLFormatter,
	) (models.UserUrls, error)

	// SaveUserUrls stores mappings between a user and a list of full URLs.
	// It uses an UPSERT strategy and runs within an existing transaction.
	SaveUserUrls(
		ctx context.Context,
		userID string,
		urls []string,
		transaction *sql.Tx,
	) error

	// RemoveUsersUrls removes URLs for a given user.
	RemoveUsersUrls(
		ctx context.Context,
		usersURLs map[string][]string,
	) error

	GetNumberOfShortenedURLs(ctx context.Context) (int64, error)

	GetNumberOfUsers(ctx context.Context) (int64, error)
}

// Transactioner defines methods for handling database transactions.
type Transactioner interface {
	// BeginTransaction starts a new transaction and returns it.
	BeginTransaction() (*sql.Tx, error)

	// RollbackTransaction rolls back the given transaction.
	RollbackTransaction(transaction *sql.Tx) error

	// CommitTransaction commits the given transaction.
	CommitTransaction(transaction *sql.Tx) error
}

// URLsMapper is an interface for mapping between full URLs and short URLs.
type URLsMapper interface {
	// FindShortsByFulls retrieves all known short URLs for the given list of full URLs.
	FindShortsByFulls(
		ctx context.Context,
		originalUrls []string,
		transaction *sql.Tx,
	) (map[string]string, error)

	// SaveNewFullsAndShorts stores new full-to-short URL mappings.
	SaveNewFullsAndShorts(
		ctx context.Context,
		unexistentFullsToShortsMap map[string]string,
		transaction *sql.Tx,
	) error

	// FindFullByShort retrieves the full URL associated with the given short URL.
	FindFullByShort(ctx context.Context, short string) (string, bool, error)

	// FindShortByFull retrieves the short URL associated with the given full URL.
	FindShortByFull(
		ctx context.Context,
		full string,
		transaction *sql.Tx,
	) (string, bool, error)

	// InsertURLMapping stores a mapping from short to full URL.
	InsertURLMapping(
		ctx context.Context,
		short,
		full string,
		transaction *sql.Tx,
	) error
}

// Pinger is an interface for pinging a storage to check its health.
type Pinger interface {
	// Ping checks the storage's health.
	Ping(ctx context.Context) error
}

// Storage defines the interface for interacting with user data, URLs, and transactions.
// It includes methods for managing users, URLs, transactions, and health checks.
type Storage interface {
	UserKeeper
	UserUrlsKeeper
	Transactioner
	URLsMapper
	Pinger
	Close() error
}

// Remover is an interface for handling background URL deletion jobs.
type Remover interface {
	// ListenErrors listens for errors and passes them to the provided callback function.
	ListenErrors(callback func(error))

	// Run starts the background job processing.
	Run(ctx context.Context)

	// EnqueueJob adds a new job to the queue.
	EnqueueJob(job *models.URLDeleteJob)
}

type ipChecker interface {
	IsTrustedSubnetEmpty() bool

	GetClientIP(request *http.Request) (net.IP, error)

	Check(clientIP net.IP) bool
}

// App encapsulates the configuration, HTTP handler, Storage backend,
// and background services (such as URL remover) needed to run the URL shortener service.
type App struct {
	cfg             *config.Config
	db              Storage
	urlsRemover     Remover
	stopUrlsRemover context.CancelFunc
	httpHandler     http.Handler
	server          *http.Server
	ipChecker       ipChecker
	grpcServer      *grpc.Server
	grpcListener    net.Listener
}

// New initializes a new instance of App by:
// - loading configuration
// - initializing logger
// - selecting and setting up Storage
// - setting up the background URL remover
// - setting up the router and middleware
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

	ipChecker, err := ipchecker.New(app.cfg.TrustedSubnet)
	if err != nil {
		return nil, err
	}

	s := service.New(
		app.db,
		app.urlsRemover,
		app.cfg.ShortURLBase,
	)

	authenticator := auth.New(
		app.db,
		app.cfg.AuthCookieName,
		authCookieSigningSecretKey,
	)

	app.httpHandler = router.New(
		app.db,
		authenticator,
		ipChecker,
		s,
	)

	app.server = &http.Server{
		Addr:    app.cfg.RunAddr,
		Handler: app.httpHandler,
	}

	if app.cfg.GRPCEnabled {
		app.grpcServer, app.grpcListener, err = grpcserver.NewGRPCServer(
			app.cfg.GRPCAddress,
			grpcserver.NewShortenerHandler(s),
			authenticator,
			app.db,
		)
		if err != nil {
			return nil, err
		}
	}

	return app, nil
}

// Run starts the HTTP server with graceful shutdown support.
// It listens for system signals and cleans up resources upon termination.
func (a *App) Run() error {
	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGQUIT,
	)
	defer stop()

	logger.Log.Infoln("server running", "RunAddr", a.cfg.RunAddr)

	serverErrCh := make(chan error, 1)
	go func() {
		if a.cfg.EnableHTTPS {
			serverErrCh <- a.server.ListenAndServeTLS(a.cfg.CertFile, a.cfg.KeyFile)
		} else {
			serverErrCh <- a.server.ListenAndServe()
		}
	}()

	grpcErrCh := make(chan error, 1)
	if a.cfg.GRPCEnabled {
		go func() {
			grpcErrCh <- a.grpcServer.Serve(a.grpcListener)
		}()
	}

	select {
	case <-ctx.Done():
		logger.Log.Infoln("Received shutdown signal. Saving database and exiting...")
		a.stopUrlsRemover()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := a.server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("server shutdown error: %w", err)
		}

		if a.grpcServer != nil {
			a.grpcServer.GracefulStop()
			logger.Log.Infoln("gRPC server stopped")
		}

		return a.db.Close()

	case err := <-serverErrCh:
		return fmt.Errorf("HTTP server error: %w", err)

	case err := <-grpcErrCh:
		return fmt.Errorf("gRPC server error: %w", err)
	}
}

// Close finalizes resources used by App such as logging.
func (a *App) Close() {
	if err := logger.Sync(); err != nil {
		fmt.Println("Logger sync error:", err)
	}
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

func getStorageByType(cfg *config.Config) (Storage, error) {
	switch getAvailableStorageType(cfg) {
	case models.StorageTypeUnknown:
		return nil, errors.New("unknown storage type")

	case models.StorageTypePostgresql:
		return postgresdb.New(
			context.Background(),
			cfg.DatabaseDSN,
			cfg.DBConnectionTimeout,
			cfg.MigrationsDir,
		)

	case models.StorageTypeFile:
		return jsondb.New(cfg.DBFileName)
	}

	return memorystorage.New()
}
