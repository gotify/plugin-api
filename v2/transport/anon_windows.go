//go:build windows

package transport

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

func NewAnonPipe(rx *uintptr, tx *uintptr, cloexec bool) error {
	var tmp [2]windows.Handle
	var sa windows.SecurityAttributes
	sa.Length = uint32(unsafe.Sizeof(sa))
	if !cloexec {
		sa.InheritHandle = 1
	}
	err := windows.CreatePipe(&tmp[0], &tmp[1], &sa, 0)
	if err != nil {
		return err
	}
	*rx = uintptr(tmp[0])
	*tx = uintptr(tmp[1])
	return nil
}
