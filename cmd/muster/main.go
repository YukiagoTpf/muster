// Command muster launches coding agents with a chosen credential and shows
// every subscription's quota at a glance.
package main

import (
	"os"

	"github.com/YukiagoTpf/muster/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:], cli.StdIO()))
}
