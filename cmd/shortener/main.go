package main

import (
	"log"

	"github.com/patric-chuzhbe/urlshrt/internal/app"
)

func main() {
	a, err := app.New()
	if err != nil {
		log.Fatal(err)
	}
	defer a.Close()

	if err := a.Run(); err != nil {
		log.Fatal(err)
	}
}
