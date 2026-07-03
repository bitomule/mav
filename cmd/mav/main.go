package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/bitomule/mav/internal/mav"
)

func main() {
	ctx := context.Background()
	if err := mav.Run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		var failed mav.CommandFailed
		if errors.As(err, &failed) {
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
