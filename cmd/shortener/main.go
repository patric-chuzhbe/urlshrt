package main

import (
	"context"
	"fmt"
	"github.com/patric-chuzhbe/urlshrt/internal/config"
	"github.com/patric-chuzhbe/urlshrt/internal/logger"
	"github.com/patric-chuzhbe/urlshrt/internal/memorystorage"
	"github.com/patric-chuzhbe/urlshrt/internal/postgresdb"
	"github.com/patric-chuzhbe/urlshrt/internal/router"
	"github.com/patric-chuzhbe/urlshrt/internal/simplejsondb"
	"github.com/patric-chuzhbe/urlshrt/internal/storage"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

var theDB storage.Storage

const (
	StorageTypePostgresql = iota
	StorageTypeFile
	StorageTypeMemory
)

func getTheMostWantedOfAvailableStorageType() int {
	if config.Values.DatabaseDSN != "" {
		return StorageTypePostgresql
	}

	if config.Values.DBFileName != "" {
		return StorageTypeFile
	}

	return StorageTypeMemory
}

func getTheMostWantedOfAvailableStorage() (storage.Storage, error) {
	switch getTheMostWantedOfAvailableStorageType() {
	case StorageTypePostgresql:
		return postgresdb.New(
			context.Background(),
			postgresdb.Config{
				FileStoragePath:   config.Values.DBFileName,
				DatabaseDSN:       config.Values.DatabaseDSN,
				ConnectionTimeout: config.Values.DBConnectionTimeout,
			},
		)
	case StorageTypeFile:
		return simplejsondb.New(config.Values.DBFileName)
	}

	//case StorageTypeMemory:
	return memorystorage.New()
}

func main() {
	var err error

	err = config.Init()
	if err != nil {
		panic(err)
	}

	err = logger.Init(config.Values.LogLevel)
	if err != nil {
		panic(err)
	}
	defer func() {
		err := logger.Sync()
		if err != nil {
			panic(err)
		}
	}()

	theDB, err = getTheMostWantedOfAvailableStorage()
	if err != nil {
		panic(err)
	}
	defer func() {
		if p := recover(); p != nil {
			err := theDB.Close()
			if err != nil {
				fmt.Println("Error closing database:", err)
			}
		}
	}()

	httpHandler := router.New(theDB)

	// Handle SIGINT signal (Ctrl+C)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	//signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigCh
		logger.Log.Infoln(
			"Received interrupt signal, saving database and exiting...",
		)
		err := theDB.Close()
		if err != nil {
			panic(err)
		}
		os.Exit(0)
	}()

	logger.Log.Infoln(
		"server running",
		"RunAddr", config.Values.RunAddr,
	)
	err = http.ListenAndServe(config.Values.RunAddr, httpHandler)
	if err != nil {
		panic(err)
	}
}
