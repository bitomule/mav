package main

import (
	"context"
	"fmt"
	"os"

	"github.com/bitomule/mav/internal/mav"
)

func main() {
	ctx := context.Background()
	if err := mav.Run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
