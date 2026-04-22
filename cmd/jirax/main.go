package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"jirax/internal/jirax"
)

var version = "dev"

func main() {
	ctx := context.Background()
	app, err := jirax.NewApp()
	if err != nil {
		fatal(err)
	}

	if err := app.Run(ctx, os.Args[1:]); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	var usageErr *jirax.UsageError
	if errors.As(err, &usageErr) {
		fmt.Fprintln(os.Stderr, usageErr.Error())
		os.Exit(2)
	}
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
