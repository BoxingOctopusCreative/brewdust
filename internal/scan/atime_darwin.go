// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package scan

import (
	"syscall"
	"time"
)

func atime(st *syscall.Stat_t) time.Time {
	return time.Unix(st.Atimespec.Unix())
}
