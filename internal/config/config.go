package config

import (
	"flag"
	env "github.com/caarlos0/env/v6"
)

type config struct {
	RunAddr      string `env:"SERVER_ADDRESS"`
	ShortURLBase string `env:"BASE_URL"`
}

var Values config

func Init() {
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
}
