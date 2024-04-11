package config

import "flag"

type config struct {
	RunAddr      string
	ShortURLBase string
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
}
