package main

import (
	"os"

	"hermetarium/supervisor"
)

func main() {
	os.Exit(supervisor.Run(os.Args[1:]))
}
