package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/patric-chuzhbe/urlshrt/internal/config"
	"github.com/patric-chuzhbe/urlshrt/internal/db/jsondb"
	"github.com/patric-chuzhbe/urlshrt/internal/db/memorystorage"
	"github.com/patric-chuzhbe/urlshrt/internal/db/postgresdb"
	"github.com/patric-chuzhbe/urlshrt/internal/db/storage"
	"github.com/patric-chuzhbe/urlshrt/internal/logger"
	"github.com/patric-chuzhbe/urlshrt/internal/models"
	"github.com/patric-chuzhbe/urlshrt/internal/router"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

func getAvailableStorageType(cfg *config.Config) int {
	if cfg.DatabaseDSN != "" {
		return models.StorageTypePostgresql
	}

	if cfg.DBFileName != "" {
		return models.StorageTypeFile
	}

	return models.StorageTypeMemory
}

func getStorageByType(cfg *config.Config) (storage.Storage, error) {
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

func main() {
	var db storage.Storage

	var err error

	cfg, err := config.New()
	if err != nil {
		log.Fatal(err)
	}

	err = logger.Init(cfg.LogLevel)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		err := logger.Sync()
		if err != nil {
			fmt.Println("Logger sync error:", err)
		}
	}()

	db, err = getStorageByType(cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		if p := recover(); p != nil {
			err := db.Close()
			if err != nil {
				fmt.Println("Error closing database:", err)
			}
		}
	}()

	httpHandler := router.New(db, cfg.ShortURLBase)

	// Handle SIGINT signal (Ctrl+C)
	termCh := make(chan os.Signal, 1)
	signal.Notify(termCh, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	logger.Log.Infoln(
		"server running",
		"RunAddr", cfg.RunAddr,
	)

	serverErrCh := make(chan error, 1)
	go func() {
		serverErrCh <- http.ListenAndServe(cfg.RunAddr, httpHandler)
	}()

	select {
	case sig := <-termCh:
		logger.Log.Infoln("Received signal:", sig, "Saving database and exiting...")
		err := db.Close()
		if err != nil {
			log.Fatal(err)
		}

	case err := <-serverErrCh:
		logger.Log.Fatal("Server error:", err)
	}
}
