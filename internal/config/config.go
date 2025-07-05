package config

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"time"

	"github.com/caarlos0/env/v6"
	"github.com/joho/godotenv"

	"github.com/go-playground/validator/v10"
)

// Config holds the application configuration loaded from environment variables
// and optionally overridden by command-line flags.
type Config struct {
	RunAddr                    string        `env:"SERVER_ADDRESS" envDefault:":8080" validate:"hostname_port"`           // Server address and port (e.g., ":8080")
	ShortURLBase               string        `env:"BASE_URL" envDefault:"http://localhost:8080" validate:"url"`           // Base URL used to build short URLs
	LogLevel                   string        `env:"LOG_LEVEL" envDefault:"info" validate:"loglevel"`                      // Logging level (e.g., "info", "debug")
	DBFileName                 string        `env:"FILE_STORAGE_PATH" envDefault:"" validate:"filepath"`                  // Path to the JSON file storage (used if no DB DSN)
	DatabaseDSN                string        `env:"DATABASE_DSN"`                                                         // DSN for PostgreSQL database connection
	DBConnectionTimeout        time.Duration `env:"DB_CONNECTION_TIMEOUT" envDefault:"10s"`                               // Timeout for DB connection attempts
	AuthCookieName             string        `env:"AUTH_COOKIE_NAME" envDefault:"auth"`                                   // Name of the authentication cookie
	AuthCookieSigningSecretKey string        `env:"AUTH_COOKIE_SIGNING_SECRET_KEY" envDefault:"LduYtmp2gWSRuyQyRHqbog=="` // Secret key for signing auth cookies
	ChannelCapacity            int           `env:"CHANNEL_CAPACITY" envDefault:"1024"`                                   // Channel capacity for background jobs
	DelayBetweenQueueFetches   time.Duration `env:"DELAY_BETWEEN_QUEUE_FETCHES" envDefault:"5s"`                          // Delay between attempts to dequeue jobs
	MigrationsDir              string        `env:"MIGRATIONS_DIR" envDefault:"migrations"`                               // Directory path for database migration files
	EnableHTTPS                bool          `env:"ENABLE_HTTPS" envDefault:"false"`
	CertFile                   string        `env:"CERT_FILE" envDefault:"../../cert/cert.pem"`
	KeyFile                    string        `env:"KEY_FILE" envDefault:"../../cert/key.pem"`
}

type initOptions struct {
	disableFlagsParsing bool
}

// New loads the application configuration from the environment and command-line flags,
// applying optional InitOptions. Returns a validated Config instance.
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
		flag.BoolVar(&values.EnableHTTPS, "s", values.EnableHTTPS, "HTTPS enabling flag")
		flag.Parse()
	}

	err = values.clarifyRunAddr()
	if err != nil {
		return nil, err
	}

	err = values.clarifyShortURLBase()
	if err != nil {
		return nil, err
	}

	return &values, values.Validate()
}

// Validate validates the configuration struct fields using custom and built-in rules.
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

// InitOption is a functional option type for configuring New().
type InitOption func(*initOptions)

// WithDisableFlagsParsing disables parsing of command-line flags when creating a Config.
func WithDisableFlagsParsing(disableFlagsParsing bool) InitOption {
	return func(options *initOptions) {
		options.disableFlagsParsing = disableFlagsParsing
	}
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

func (conf *Config) clarifyRunAddr() error {
	host, port, err := net.SplitHostPort(conf.RunAddr)
	if err != nil {
		return fmt.Errorf("in internal/config/config.go/clarifyRunAddr(): error while `net.SplitHostPort()` calling: %w", err)
	}

	if conf.EnableHTTPS && (port == "8080" || port == "80") {
		port = "443"
	}

	conf.RunAddr = net.JoinHostPort(host, port)

	return nil
}

func (conf *Config) clarifyShortURLBase() error {
	if !conf.EnableHTTPS {
		return nil
	}

	parsed, err := url.Parse(conf.ShortURLBase)
	if err != nil {
		return fmt.Errorf("in internal/config/config.go/clarifyShortURLBase(): error while `url.Parse()` calling: %w", err)
	}
	host, port, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		return fmt.Errorf("in internal/config/config.go/clarifyShortURLBase(): error while `net.SplitHostPort()` calling: %w", err)
	}
	if port == "443" || port == "80" || port == "8080" {
		port = ""
	} else {
		port = fmt.Sprintf(":%s", port)
	}
	conf.ShortURLBase = fmt.Sprintf("https://%s%s", host, port)

	return nil
}
