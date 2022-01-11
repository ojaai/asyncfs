// +build linux android

package asyncfs

import (
	"reflect"
	"sync"
	"syscall"
	"unsafe"
)

type (
	ctx struct {
		baseCtx
		operationsFd        map[unsafe.Pointer]*File
		alignedBuffers      map[unsafe.Pointer]slice
		aio                 uint64
		r                   *ring
		ioUringUserDataPool sync.Pool
	}
)

func (c *ctx) initCtx(sz int) error {
	c.operationsFd = make(map[unsafe.Pointer]*File, sz)

	// check io_uring
	if err := c.initIoUring(sz); err == nil {
		c.ioUringUserDataPool = sync.Pool{
			New: func() interface{} {
				return &cqUserData{}
			},
		}
		return nil
	}

	// use aio
	return c.initAio(sz)
}

func (c *ctx) initIoUring(sz int) error {
	if ioUringSetupSys == -1 || ioUringEnterSys == -1 {
		return ErrNotSupported
	}
	var r ring
	if err := setup(uint32(sz), &r, 0); err != nil {
		return err
	}
	c.r = &r
	c.asyncMode = asyncIoUring
	return nil
}

func (c *ctx) initAio(sz int) error {
	var aio uint64
	_, _, e1 := syscall.RawSyscall(syscall.SYS_IO_SETUP, uintptr(sz), uintptr(unsafe.Pointer(&aio)), 0)
	if e1 != 0 {
		return e1
	}
	c.aio = aio
	c.asyncMode = asyncAio
	c.align = 512
	c.alignedBuffers = make(map[unsafe.Pointer]slice)
	return nil
}

func (c *ctx) allocBuf(sz int) []uint8 {
	alloc := func(fullSz int) []uint8 {
		var buf []uint8
		if c.bufPoller != nil {
			buf = c.bufPoller(fullSz)
			if len(buf) != sz {
				buf = append(buf, make([]uint8, sz-len(buf))...)
			}
		} else {
			buf = make([]uint8, fullSz)
		}
		return buf
	}
	if c.asyncMode == asyncIoUring {
		return alloc(sz)
	}
	buf := alloc(sz + c.align - 1)
	bufLen := len(buf)
	bufCap := cap(buf)
	rawBuf := unsafe.Pointer(&buf[0])
	hdr := (*reflect.SliceHeader)(unsafe.Pointer(&buf))
	hdr.Data = uintptr((uint64(hdr.Data) + uint64(c.align-1)) & ^(uint64(c.align) - 1))
	hdr.Cap -= c.align - 1
	hdr.Len -= c.align - 1
	c.alignedBuffers[unsafe.Pointer(&buf[0])] = slice{
		b:   rawBuf,
		len: bufLen,
		cap: bufCap,
	}
	return buf
}

func (c *ctx) releaseBuf(b []uint8) {
	buf := b
	if c.alignedBuffers != nil {
		toRelease, ok := c.alignedBuffers[unsafe.Pointer(&b[0])]
		if ok {
			hdr := (*reflect.SliceHeader)(unsafe.Pointer(&buf))
			hdr.Data = uintptr(toRelease.b)
			hdr.Len = toRelease.len
			hdr.Cap = toRelease.cap
			delete(c.alignedBuffers, unsafe.Pointer(&b[0]))
		}
	}
	for i := range buf {
		buf[i] = 0x0
	}
	buf = buf[:0]
	if c.bufReleaser != nil {
		c.bufReleaser(buf)
	}
}

func (c *ctx) busy() bool {
	if c.asyncMode == asyncIoUring {
		c.Lock()
		res := c.currentCnt >= c.sz
		c.Unlock()
		return res
	}
	return false
}
