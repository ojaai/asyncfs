// +build freebsd darwin

package asyncfs

import (
	"unsafe"
)

type (
	opCap struct {
		f    *File
		data int64
	}

	ctx struct {
		baseCtx
		operationsFd map[unsafe.Pointer]opCap
	}
)

func (c *ctx) initCtx(sz int) error {
	c.asyncMode = asyncAio
	c.operationsFd = make(map[unsafe.Pointer]opCap, sz)
	return nil
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

func (c *ctx) busy() bool {
	return false
}
