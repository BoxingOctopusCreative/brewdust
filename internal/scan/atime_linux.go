package scan

import (
	"syscall"
	"time"
)

func atime(st *syscall.Stat_t) time.Time {
	return time.Unix(st.Atim.Unix())
}
