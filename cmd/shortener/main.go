package main

import (
	"fmt"
	"github.com/patric-chuzhbe/urlshrt/internal/config"
	"github.com/patric-chuzhbe/urlshrt/internal/logger"
	"github.com/patric-chuzhbe/urlshrt/internal/postgresdb"
	"github.com/patric-chuzhbe/urlshrt/internal/router"
	"github.com/patric-chuzhbe/urlshrt/internal/storage"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

var theDB storage.Storage

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

	theDB, err = postgresdb.New(postgresdb.Config{
		FileStoragePath:   config.Values.DBFileName,
		DatabaseDSN:       config.Values.DatabaseDSN,
		ConnectionTimeout: config.Values.DBConnectionTimeout,
	})
	if err != nil {
		panic(err)
	}
	defer func() {
		if p := recover(); p != nil {
			err := theDB.Close()
			if err != nil {
				fmt.Println("Error saving database to file:", err)
			}
		}
	}()

	httpHandler := router.New(theDB)

	// Handle SIGINT signal (Ctrl+C)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

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
