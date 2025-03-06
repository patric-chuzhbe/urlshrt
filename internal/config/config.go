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

type Config struct {
	RunAddr                    string        `env:"SERVER_ADDRESS" validate:"hostname_port"`
	ShortURLBase               string        `env:"BASE_URL" validate:"url"`
	LogLevel                   string        `env:"LOG_LEVEL" validate:"loglevel"`
	DBFileName                 string        `env:"FILE_STORAGE_PATH" validate:"filepath"`
	DatabaseDSN                string        `env:"DATABASE_DSN"`
	DBConnectionTimeout        time.Duration `env:"DB_CONNECTION_TIMEOUT"`
	AuthCookieName             string        `env:"AUTH_COOKIE_NAME"`
	AuthCookieSigningSecretKey string        `env:"AUTH_COOKIE_SIGNING_SECRET_KEY"`
	ChannelCapacity            int           `env:"CHANNEL_CAPACITY"`
	DelayBetweenQueueFetches   time.Duration `env:"DELAY_BETWEEN_QUEUE_FETCHES"`
}

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

func (conf *Config) Validate() error {
	validate := validator.New()

	err := validate.RegisterValidation("loglevel", validateLogLevel)
	if err != nil {
		return err
	}

	err = validate.RegisterValidation("filepath", validateFilePath)
	if err != nil {
		return err
	}

	return validate.Struct(conf)
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

func New(optionsProto ...InitOption) (*Config, error) {
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

	values := Config{
		RunAddr:                    ":8080",
		ShortURLBase:               "http://localhost:8080",
		LogLevel:                   "info",
		DBFileName:                 "",
		DatabaseDSN:                "",
		DBConnectionTimeout:        10,
		AuthCookieName:             "auth",
		AuthCookieSigningSecretKey: "LduYtmp2gWSRuyQyRHqbog==",
		ChannelCapacity:            1024,
		DelayBetweenQueueFetches:   5,
	}
	if !options.disableFlagsParsing {
		flag.StringVar(&values.RunAddr, "a", values.RunAddr, "address and port to run server")
		flag.StringVar(&values.ShortURLBase, "b", values.ShortURLBase, "base address of the resulting shortened URL")
		flag.StringVar(&values.LogLevel, "l", values.LogLevel, "logger level")
		flag.StringVar(&values.DBFileName, "f", values.DBFileName, "JSON file name with database")
		flag.StringVar(&values.DatabaseDSN, "d", values.DatabaseDSN, "A string with the database connection details")
		flag.Parse()
	}

	var valuesFromEnv Config
	err = env.Parse(&valuesFromEnv)
	if err != nil {
		return nil, err
	}

	if valuesFromEnv.RunAddr != "" {
		values.RunAddr = valuesFromEnv.RunAddr
	}

	if valuesFromEnv.ShortURLBase != "" {
		values.ShortURLBase = valuesFromEnv.ShortURLBase
	}

	if valuesFromEnv.LogLevel != "" {
		values.LogLevel = valuesFromEnv.LogLevel
	}

	if valuesFromEnv.DBFileName != "" {
		values.DBFileName = valuesFromEnv.DBFileName
	}

	if valuesFromEnv.DatabaseDSN != "" {
		values.DatabaseDSN = valuesFromEnv.DatabaseDSN
	}

	if valuesFromEnv.DBConnectionTimeout != 0 {
		values.DBConnectionTimeout = valuesFromEnv.DBConnectionTimeout
	}

	if valuesFromEnv.AuthCookieName != "" {
		values.AuthCookieName = valuesFromEnv.AuthCookieName
	}

	if valuesFromEnv.AuthCookieSigningSecretKey != "" {
		values.AuthCookieSigningSecretKey = valuesFromEnv.AuthCookieSigningSecretKey
	}

	if valuesFromEnv.ChannelCapacity != 0 {
		values.ChannelCapacity = valuesFromEnv.ChannelCapacity
	}

	if valuesFromEnv.DelayBetweenQueueFetches != 0 {
		values.DelayBetweenQueueFetches = valuesFromEnv.DelayBetweenQueueFetches
	}

	return &values, values.Validate()
}
