package config

import (
	"flag"
	env "github.com/caarlos0/env/v6"
	validator "github.com/go-playground/validator/v10"
	"github.com/joho/godotenv"
	"log"
	"os"
	"time"
)

type config struct {
	RunAddr             string        `env:"SERVER_ADDRESS" validate:"hostname_port"`
	ShortURLBase        string        `env:"BASE_URL" validate:"url"`
	LogLevel            string        `env:"LOG_LEVEL" validate:"loglevel"`
	DBFileName          string        `env:"FILE_STORAGE_PATH" validate:"filepath"`
	DatabaseDSN         string        `env:"DATABASE_DSN"`
	DBConnectionTimeout time.Duration `env:"DB_CONNECTION_TIMEOUT"`
}

var Values config

func validateFilePath(fieldLevel validator.FieldLevel) bool {
	path := fieldLevel.Field().String()
	_, err := os.Stat(path)

	return err == nil || os.IsNotExist(err)
}

func validateLogLevel(fieldLevel validator.FieldLevel) bool {
	value := fieldLevel.Field().String()

	allowedLogLevels := map[string]bool{
		"debug":   true,
		"info":    true,
		"warning": true,
		"error":   true,
		"fatal":   true,
	}

	return allowedLogLevels[value]
}

func validate() error {
	validate := validator.New()

	err := validate.RegisterValidation("loglevel", validateLogLevel)
	if err != nil {
		return err
	}

	err = validate.RegisterValidation("filepath", validateFilePath)
	if err != nil {
		return err
	}

	return validate.Struct(Values)
}

type InitOption func(*initOptions)

type initOptions struct {
	disableFlagsParsing bool
}

func WithDisableFlagsParsing(disableFlagsParsing bool) InitOption {
	return func(options *initOptions) {
		options.disableFlagsParsing = disableFlagsParsing
	}
}

func Init(optionsProto ...InitOption) error {
	options := &initOptions{
		disableFlagsParsing: false,
	}
	for _, protoOption := range optionsProto {
		protoOption(options)
	}

	err := godotenv.Load()
	if err != nil {
		log.Printf("Unable to load .env file: %v", err)
	}

	Values = config{
		RunAddr:             ":8080",
		ShortURLBase:        "http://localhost:8080",
		LogLevel:            "info",
		DBFileName:          "",
		DatabaseDSN:         "",
		DBConnectionTimeout: 10,
	}
	if !options.disableFlagsParsing {
		flag.StringVar(&Values.RunAddr, "a", Values.RunAddr, "address and port to run server")
		flag.StringVar(&Values.ShortURLBase, "b", Values.ShortURLBase, "base address of the resulting shortened URL")
		flag.StringVar(&Values.LogLevel, "l", Values.LogLevel, "logger level")
		flag.StringVar(&Values.DBFileName, "f", Values.DBFileName, "JSON file name with database")
		flag.StringVar(&Values.DatabaseDSN, "d", Values.DatabaseDSN, "A string with the database connection details")
		flag.Parse()
	}

	var valuesFromEnv config
	err = env.Parse(&valuesFromEnv)
	if err != nil {
		return err
	}

	if valuesFromEnv.RunAddr != "" {
		Values.RunAddr = valuesFromEnv.RunAddr
	}

	if valuesFromEnv.ShortURLBase != "" {
		Values.ShortURLBase = valuesFromEnv.ShortURLBase
	}

	if valuesFromEnv.LogLevel != "" {
		Values.LogLevel = valuesFromEnv.LogLevel
	}

	if valuesFromEnv.DBFileName != "" {
		Values.DBFileName = valuesFromEnv.DBFileName
	}

	if valuesFromEnv.DatabaseDSN != "" {
		Values.DatabaseDSN = valuesFromEnv.DatabaseDSN
	}

	if valuesFromEnv.DBConnectionTimeout != 0 {
		Values.DBConnectionTimeout = valuesFromEnv.DBConnectionTimeout
	}

	return validate()
}
