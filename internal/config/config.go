package config

import (
	"flag"
	env "github.com/caarlos0/env/v6"
	validator "github.com/go-playground/validator/v10"
)

type config struct {
	RunAddr      string `env:"SERVER_ADDRESS" validate:"hostname_port"`
	ShortURLBase string `env:"BASE_URL" validate:"url"`
}

var Values config

func validate() error {
	validate := validator.New()
	return validate.Struct(Values)
}

func Init() error {
	Values = config{
		RunAddr:      ":8080",
		ShortURLBase: "http://localhost:8080",
	}

	flag.StringVar(&Values.RunAddr, "a", Values.RunAddr, "address and port to run server")
	flag.StringVar(&Values.ShortURLBase, "b", Values.ShortURLBase, "base address of the resulting shortened URL")
	flag.Parse()

	var valuesFromEnv config
	env.Parse(&valuesFromEnv)

	if valuesFromEnv.RunAddr != "" {
		Values.RunAddr = valuesFromEnv.RunAddr
	}

	if valuesFromEnv.ShortURLBase != "" {
		Values.ShortURLBase = valuesFromEnv.ShortURLBase
	}

	return validate()
}
