package main

import (
	"familytunnel/internal/app"
	"fmt"
	"os"
)

func main() {
	if err := app.Run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "FamilyTunnel:", err)
		os.Exit(1)
	}
}
