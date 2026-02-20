package main

import (
	"fmt"
	"os"

	"film-heatmap/internal/app"
)

func main() {
	if err := app.RunAuto(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
