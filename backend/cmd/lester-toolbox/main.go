package main

import (
	"fmt"
	"os"

	"github.com/biubiuqiu/lester-agent/backend/internal/toolboxfs"
)

func main() {
	if err := toolboxfs.Run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
