package config

import (
	"flag"
	env "github.com/caarlos0/env/v6"
	validator "github.com/go-playground/validator/v10"
)

type config struct {
	RunAddr      string `env:"SERVER_ADDRESS" validate:"hostname_port"`
	ShortURLBase string `env:"BASE_URL" validate:"url"`
	LogLevel     string `env:"LOG_LEVEL" validate:"loglevel"`
}

var Values config

func validateLogLevel(fl validator.FieldLevel) bool {
	value := fl.Field().String()

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
	Values = config{
		RunAddr:      ":8080",
		ShortURLBase: "http://localhost:8080",
		LogLevel:     "info",
	}
	if !options.disableFlagsParsing {
		flag.StringVar(&Values.RunAddr, "a", Values.RunAddr, "address and port to run server")
		flag.StringVar(&Values.ShortURLBase, "b", Values.ShortURLBase, "base address of the resulting shortened URL")
		flag.StringVar(&Values.LogLevel, "l", Values.LogLevel, "logger level")
		flag.Parse()
	}

	var valuesFromEnv config
	err := env.Parse(&valuesFromEnv)
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

	return validate()
}
