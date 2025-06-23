// The application provides a custom Go static analysis tool that combines
// standard analyzers from the Go toolchain, third-party analyzers, and project-specific
// analyzers into a single `multichecker.Main` invocation.
//
// The static analyzer list can be extended or filtered via a config file (config.json),
// which lists the names of staticcheck analyzers to be enabled.
//
// This package is intended to be compiled into a standalone binary used to enforce
// coding rules and catch potential bugs across a Go project.
package main

import (
	// Standard analyzers from the Go toolchain.
	"golang.org/x/tools/go/analysis/passes/copylock"
	"golang.org/x/tools/go/analysis/passes/loopclosure"
	"golang.org/x/tools/go/analysis/passes/lostcancel"
	"golang.org/x/tools/go/analysis/passes/printf"
	"golang.org/x/tools/go/analysis/passes/structtag"
	"golang.org/x/tools/go/analysis/passes/unmarshal"
	"golang.org/x/tools/go/analysis/passes/unreachable"

	// Third-party analyzers.
	"github.com/gordonklaus/ineffassign/pkg/ineffassign"
	"github.com/gostaticanalysis/nilerr"

	// Custom analyzer.
	"github.com/patric-chuzhbe/urlshrt/cmd/staticlint/noosexit"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/multichecker"
	"honnef.co/go/tools/staticcheck"

	"encoding/json"
	"os"
	"path/filepath"
)

// Config is the name of the JSON configuration file that lists enabled staticcheck analyzers.
const Config = `config.json`

// ConfigData describes the structure of the configuration file.
// The Staticcheck field contains the names of enabled staticcheck analyzers, e.g., "SA1000", "SA4010".
type ConfigData struct {
	Staticcheck []string
}

// main is the entry point for the static analysis binary.
// It loads a configuration file, collects analyzers, and launches them using multichecker.Main.
//
// It includes:
//   - Standard Go analyzers for detecting common bugs.
//   - Third-party analyzers like ineffassign and nilerr.
//   - A custom analyzer that disallows use of os.Exit in main.main.
//   - A configurable set of staticcheck analyzers.
func main() {
	appfile, err := os.Executable()
	if err != nil {
		panic(err)
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(appfile), Config))
	if err != nil {
		panic(err)
	}
	var cfg ConfigData
	if err = json.Unmarshal(data, &cfg); err != nil {
		panic(err)
	}

	// Standard and custom analyzers that are always run.
	myChecks := []*analysis.Analyzer{
		copylock.Analyzer,    // Checks for copying of locks by value.
		loopclosure.Analyzer, // Detects references to loop variables inside closures.
		lostcancel.Analyzer,  // Finds contexts that are not canceled.
		printf.Analyzer,      // Verifies format strings.
		structtag.Analyzer,   // Checks for incorrect struct field tags.
		unmarshal.Analyzer,   // Detects unused fields in JSON unmarshal targets.
		unreachable.Analyzer, // Detects unreachable code.

		ineffassign.Analyzer, // Detects ineffective assignments.
		nilerr.Analyzer,      // Flags returning nil after an error was created.

		noosexit.Analyzer, // Project-specific: forbids use of os.Exit in main.main.
	}

	checks := make(map[string]bool)
	for _, v := range cfg.Staticcheck {
		checks[v] = true
	}

	for _, v := range staticcheck.Analyzers {
		if checks[v.Analyzer.Name] {
			myChecks = append(myChecks, v.Analyzer)
		}
	}

	multichecker.Main(myChecks...)
}
