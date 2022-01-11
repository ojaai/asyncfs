// +build 386 arm armbe mips mipsle ppc s390 sparc
// +build linux android freebsd darwin

package asyncfs

import "syscall"

func newIovec(b *uint8, l int) syscall.Iovec {
	return syscall.Iovec{
		Base: b,
		Len:  uint32(l),
	}
}
