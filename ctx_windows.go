// +build windows

package asyncfs

import (
	"fmt"
	"syscall"
	"unsafe"
)

type (
	ctx struct {
		baseCtx
		nCreateEvent         uint64
		nWriteFile           uint64
		nReadFile            uint64
		nGetOverlappedResult uint64
		operationsFd         map[unsafe.Pointer]*File
	}
)

func (c *ctx) initCtx(sz int) error {
	k32, err := syscall.LoadLibrary("kernel32.dll")
	if err != nil {
		return err
	}

	ce, err := getProcAddr(k32, "CreateEventW")
	if err != nil {
		return fmt.Errorf("CreateEvent: '%s'", err)
	}
	c.nCreateEvent = ce

	wf, err := getProcAddr(k32, "WriteFile")
	if err != nil {
		return fmt.Errorf("WriteFile: '%s'", err)
	}
	c.nWriteFile = wf

	rf, err := getProcAddr(k32, "ReadFile")
	if err != nil {
		return fmt.Errorf("ReadFile: '%s'", err)
	}
	c.nReadFile = rf

	gor, err := getProcAddr(k32, "GetOverlappedResult")
	if err != nil {
		return fmt.Errorf("GetOverlappedResult: '%s'", err)
	}
	c.nGetOverlappedResult = gor

	c.operationsFd = make(map[unsafe.Pointer]*File, sz)

	return nil
}

func (c *ctx) haveToAlign() bool {
	return false
}

func (c *ctx) busy() bool {
	return false
}

func (c *ctx) allocBuf(sz int) []uint8 {
	var buf []uint8
	if c.bufPoller != nil {
		buf = c.bufPoller(sz)
		if len(buf) != sz {
			buf = append(buf, make([]uint8, sz-len(buf))...)
		}
	} else {
		buf = make([]uint8, sz)
	}
	return buf
}

func (c *ctx) releaseBuf(b []uint8) {
	buf := b
	for i := range buf {
		buf[i] = 0x0
	}
	buf = buf[:0]
	if c.bufReleaser != nil {
		c.bufReleaser(buf)
	}
}

func getProcAddr(lib syscall.Handle, name string) (uint64, error) {
	addr, err := syscall.GetProcAddress(lib, name)
	if err != nil {
		return 0, err
	}
	return uint64(addr), nil
}
