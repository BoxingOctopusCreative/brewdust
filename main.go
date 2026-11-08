// brewdust reports how much disk space each Homebrew package uses and how
// long it has been since it was last used, and helps clean up what isn't.
package main

import (
	"os"

	"github.com/BoxingOctopusCreative/brewdust/internal/cli"
)

var version = "dev"

func main() {
	os.Exit(cli.Execute(version))
}
