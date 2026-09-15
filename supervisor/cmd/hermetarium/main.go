package main

import (
	"os"

	"github.com/pihme/hermetarium/supervisor"
)

func main() {
	os.Exit(supervisor.Run(os.Args[1:]))
}
