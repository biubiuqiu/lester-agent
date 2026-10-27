package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/biubiuqiu/lester-agent/backend/internal/commandtask"
	"github.com/biubiuqiu/lester-agent/backend/internal/githubsandbox"
	"github.com/biubiuqiu/lester-agent/backend/internal/toolboxfs"
)

func main() {
	var err error
	if len(os.Args) > 1 && strings.HasPrefix(os.Args[1], "github-") {
		err = githubsandbox.Run(os.Args[1:], os.Stdout)
	} else if len(os.Args) > 1 && strings.HasPrefix(os.Args[1], "task-") {
		err = commandtask.RunCLI(os.Args[1:], os.Stdin, os.Stdout)
	} else {
		err = toolboxfs.Run(os.Args[1:], os.Stdin, os.Stdout)
	}
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
