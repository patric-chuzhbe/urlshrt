package config

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"reflect"
	"time"

	"github.com/caarlos0/env/v6"
	"github.com/joho/godotenv"

	"github.com/go-playground/validator/v10"
)

// Config holds the application configuration loaded from environment variables
// and optionally overridden by command-line flags.
type Config struct {
	RunAddr                    string        `env:"SERVER_ADDRESS" validate:"hostname_port" json:"server_address"`   // Server address and port (e.g., ":8080")
	ShortURLBase               string        `env:"BASE_URL" validate:"url" json:"base_url"`                         // Base URL used to build short URLs
	LogLevel                   string        `env:"LOG_LEVEL"  validate:"loglevel"`                                  // Logging level (e.g., "info", "debug")
	DBFileName                 string        `env:"FILE_STORAGE_PATH"  validate:"filepath" json:"file_storage_path"` // Path to the JSON file storage (used if no DB DSN)
	DatabaseDSN                string        `env:"DATABASE_DSN" json:"database_dsn"`                                // DSN for PostgreSQL database connection
	DBConnectionTimeout        time.Duration `env:"DB_CONNECTION_TIMEOUT"`                                           // Timeout for DB connection attempts
	AuthCookieName             string        `env:"AUTH_COOKIE_NAME"`                                                // Name of the authentication cookie
	AuthCookieSigningSecretKey string        `env:"AUTH_COOKIE_SIGNING_SECRET_KEY"`                                  // Secret key for signing auth cookies
	ChannelCapacity            int           `env:"CHANNEL_CAPACITY"`                                                // Channel capacity for background jobs
	DelayBetweenQueueFetches   time.Duration `env:"DELAY_BETWEEN_QUEUE_FETCHES"`                                     // Delay between attempts to dequeue jobs
	MigrationsDir              string        `env:"MIGRATIONS_DIR"`                                                  // Directory path for database migration files
	EnableHTTPS                bool          `env:"ENABLE_HTTPS"  json:"enable_https"`
	CertFile                   string        `env:"CERT_FILE"`
	KeyFile                    string        `env:"KEY_FILE"`
	JSONConfigFilePath         string        `env:"CONFIG"`
	TrustedSubnet              string        `env:"TRUSTED_SUBNET" json:"trusted_subnet"`
	GRPCEnabled                bool          `env:"GRPC_ENABLED"`
	GRPCAddress                string        `env:"GRPC_ADDRESS"`
}

var defaultConfig = Config{
	RunAddr:                    ":8080",
	ShortURLBase:               "http://localhost:8080",
	LogLevel:                   "info",
	DBFileName:                 "",
	DBConnectionTimeout:        10 * time.Second,
	AuthCookieName:             "auth",
	AuthCookieSigningSecretKey: "LduYtmp2gWSRuyQyRHqbog==",
	ChannelCapacity:            1024,
	DelayBetweenQueueFetches:   5 * time.Second,
	MigrationsDir:              "migrations",
	EnableHTTPS:                false,
	CertFile:                   "../../cert/cert.pem",
	KeyFile:                    "../../cert/key.pem",
	JSONConfigFilePath:         "config.json",
	TrustedSubnet:              "127.0.0.0/8",
	GRPCEnabled:                false,
	GRPCAddress:                ":50051",
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

	config := &Config{}

	parseJSON(config)

	err := parseENV(config)
	if err != nil {
		return nil, err
	}

	if !options.disableFlagsParsing {
		parseFlags(config)
	}

	applyDefaults(config, defaultConfig)

	err = config.clarifyRunAddr()
	if err != nil {
		return nil, err
	}

	err = config.clarifyShortURLBase()
	if err != nil {
		return nil, err
	}

	return config, config.Validate()

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

	host := parsed.Hostname()
	port := parsed.Port()

	switch port {
	case "443", "80", "8080":
		port = ""
	case "":
	default:
		port = ":" + port
	}

	conf.ShortURLBase = fmt.Sprintf("https://%s%s", host, port)
	return nil
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

func applyDefaults(config *Config, defaultConfig Config) {
	vCfg := reflect.ValueOf(config).Elem()
	vDef := reflect.ValueOf(defaultConfig)

	for i := 0; i < vCfg.NumField(); i++ {
		field := vCfg.Field(i)
		defVal := vDef.Field(i)

		if field.CanSet() && field.IsZero() {
			field.Set(defVal)
		}
	}
}

func parseFlags(config *Config) {
	flag.StringVar(&config.RunAddr, "a", config.RunAddr, "address and port to run server")
	flag.StringVar(&config.ShortURLBase, "b", config.ShortURLBase, "base address of the resulting shortened URL")
	flag.StringVar(&config.LogLevel, "l", config.LogLevel, "logger level")
	flag.StringVar(&config.DBFileName, "f", config.DBFileName, "JSON file name with database")
	flag.StringVar(&config.DatabaseDSN, "d", config.DatabaseDSN, "A string with the database connection details")
	flag.BoolVar(&config.EnableHTTPS, "s", config.EnableHTTPS, "HTTPS enabling flag")

	JSONConfigFilePathDesc := "JSON configuration file path"
	flag.StringVar(&config.JSONConfigFilePath, "c", config.JSONConfigFilePath, JSONConfigFilePathDesc)
	flag.StringVar(&config.JSONConfigFilePath, "config", config.JSONConfigFilePath, JSONConfigFilePathDesc)

	flag.StringVar(&config.TrustedSubnet, "t", config.TrustedSubnet, "CIDR for the trusted subnet")

	flag.Parse()
}

func parseENV(config *Config) error {
	err := godotenv.Load()
	if err != nil {
		log.Printf("Unable to load .env file: %v", err)
	}

	err = env.Parse(config)
	if err != nil {
		return err
	}

	return nil
}

func parseJSON(values *Config) {
	var configFileName string
	if val := os.Getenv("CONFIG"); val != "" {
		configFileName = val
	}
	for i, arg := range os.Args {
		if (arg == "-c" || arg == "-config") && i+1 < len(os.Args) {
			configFileName = os.Args[i+1]
		}
	}

	if file, err := os.Open(configFileName); err == nil {
		defer file.Close()
		if err := json.NewDecoder(file).Decode(values); err != nil {
			log.Printf("Failed to parse %s: %v", configFileName, err)
		} else {
			log.Printf("Loaded config from %s", configFileName)
		}
	} else if !os.IsNotExist(err) {
		log.Printf("Failed to open %s: %v", configFileName, err)
	}
}
