// +build amd64 amd64p32 arm64 arm64be ppc64 ppc64le mips64 mips64le mips64p32 mips64p32le s390x sparc64
// +build linux android freebsd darwin

package asyncfs

import "syscall"

func newIovec(b *uint8, l int) syscall.Iovec {
	return syscall.Iovec{
		Base: b,
		Len:  uint64(l),
	}
}
