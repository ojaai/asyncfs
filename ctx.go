package asyncfs

import (
	"sync"
	"unsafe"
)

type (
	asyncOpState struct {
		data     []uint8
		result   int64
		lastCap  int
		lastOp   int
		complete bool
		eof      bool
	}

	slice struct {
		b   unsafe.Pointer
		len int
		cap int
	}

	baseCtx struct {
		bufPoller   func(int) []uint8
		bufReleaser func([]uint8)
		newThread   func(int) bool
		asyncMode   int
		align       int
		sz          int
		currentCnt  int
		sync.Mutex
	}
)

const (
	OpUnknown = 0x0
	OpRead    = 0x1
	OpWrite   = 0x2
)

var c *ctx

func NewCtx(sz int, sameThreadLim int, bufPoller func(int) []uint8, bufReleaser func([]uint8)) error {
	c = new(ctx)
	if err := c.initCtx(sz); err != nil {
		return err
	}
	c.sz = sz
	c.newThread = func(sz int) bool {
		return sz > sameThreadLim
	}
	c.bufPoller = bufPoller
	c.bufReleaser = bufReleaser
	return nil
}

func AllocBuf(sz int) []uint8 {
	return c.allocBuf(sz)
}

func ReleaseBuf(b []uint8) {
	c.releaseBuf(b)
}

func Align() int {
	return c.align
}
