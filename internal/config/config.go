package config

import (
	"flag"
	"github.com/caarlos0/env/v6"
	"github.com/go-playground/validator/v10"
	"github.com/joho/godotenv"
	"log"
	"os"
	"time"
)

type Config struct {
	RunAddr                    string        `env:"SERVER_ADDRESS" envDefault:":8080" validate:"hostname_port"`
	ShortURLBase               string        `env:"BASE_URL" envDefault:"http://localhost:8080" validate:"url"`
	LogLevel                   string        `env:"LOG_LEVEL" envDefault:"info" validate:"loglevel"`
	DBFileName                 string        `env:"FILE_STORAGE_PATH" envDefault:"" validate:"filepath"`
	DatabaseDSN                string        `env:"DATABASE_DSN"`
	DBConnectionTimeout        time.Duration `env:"DB_CONNECTION_TIMEOUT" envDefault:"10s"`
	AuthCookieName             string        `env:"AUTH_COOKIE_NAME" envDefault:"auth"`
	AuthCookieSigningSecretKey string        `env:"AUTH_COOKIE_SIGNING_SECRET_KEY" envDefault:"LduYtmp2gWSRuyQyRHqbog=="`
	ChannelCapacity            int           `env:"CHANNEL_CAPACITY" envDefault:"1024"`
	DelayBetweenQueueFetches   time.Duration `env:"DELAY_BETWEEN_QUEUE_FETCHES" envDefault:"5s"`
	MigrationsDir              string        `env:"MIGRATIONS_DIR" envDefault:"migrations"`
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

	values := Config{}

	err = env.Parse(&values)
	if err != nil {
		return nil, err
	}

	if !options.disableFlagsParsing {
		flag.StringVar(&values.RunAddr, "a", values.RunAddr, "address and port to run server")
		flag.StringVar(&values.ShortURLBase, "b", values.ShortURLBase, "base address of the resulting shortened URL")
		flag.StringVar(&values.LogLevel, "l", values.LogLevel, "logger level")
		flag.StringVar(&values.DBFileName, "f", values.DBFileName, "JSON file name with database")
		flag.StringVar(&values.DatabaseDSN, "d", values.DatabaseDSN, "A string with the database connection details")
		flag.Parse()
	}

	return &values, values.Validate()
}
