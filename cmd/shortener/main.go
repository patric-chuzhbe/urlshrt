package main

import (
	"log"
	"os"
	"text/template"

	"github.com/patric-chuzhbe/urlshrt/internal/app"
)

var (
	buildVersion string
	buildDate    string
	buildCommit  string
)

func showGreeting() {
	err := template.
		Must(
			template.
				New("greeting").
				Parse(`Build version: {{if .BuildVersion}}{{.BuildVersion}}{{else}}N/A{{end}}
Build date: {{if .BuildDate}}{{.BuildDate}}{{else}}N/A{{end}}
Build commit: {{if .BuildCommit}}{{.BuildCommit}}{{else}}N/A{{end}}
`),
		).
		Execute(
			os.Stdout,
			struct {
				BuildVersion string
				BuildDate    string
				BuildCommit  string
			}{
				BuildVersion: buildVersion,
				BuildDate:    buildDate,
				BuildCommit:  buildCommit,
			},
		)
	if err != nil {
		log.Fatal(err)
	}
}

func main() {
	showGreeting()

	a, err := app.New()
	if err != nil {
		log.Fatal(err)
	}
	defer a.Close()

	if err := a.Run(); err != nil {
		log.Fatal(err)
	}
}
