// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

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
