package main

import "os"

func main() {
	os.Exit(1) // want "avoid using os.Exit in main.main"
}
