package main

import (
	"fmt"
	"os"

	"github.com/biubiuqiu/lester-agent/backend/internal/localrun"
)

func main() {
	server := localrun.NewServer(os.Stdout, nil)
	if err := server.Serve(os.Stdin); err != nil {
		fmt.Fprintln(os.Stderr, "desktop runner protocol closed")
		os.Exit(1)
	}
}
