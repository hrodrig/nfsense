// Command nfsense manages nftables intended state on a single Debian host.
package main

import (
	"os"

	"github.com/hrodrig/nfsense/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:]))
}
