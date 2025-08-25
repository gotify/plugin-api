//go:build unix

package plugin

import (
	"golang.org/x/sys/unix"
)

func NewAnonPipe(rx *uintptr, tx *uintptr, cloexec bool) error {
	var tmp [2]int
	var flags int
	if cloexec {
		flags = unix.O_CLOEXEC
	}
	if err := unix.Pipe2(tmp[:], flags); err != nil {
		return err
	}
	*rx = uintptr(tmp[0])
	*tx = uintptr(tmp[1])
	return nil
}
