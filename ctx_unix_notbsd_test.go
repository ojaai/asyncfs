// +build linux android

package asyncfs

import (
	"github.com/stretchr/testify/assert"
	"runtime"
	"sync"
	"testing"
	"unsafe"
)

func steps() []int {
	return []int{asyncAio, asyncIoUring}
}

func prepare(t *testing.T, mode int) {
	var sz int

	if mode == 0 { // windows/bsd
		sz = 8
		err := NewCtx(sz, sameThreadLim, nil, nil)
		assert.NoError(t, err)
		return
	}

	if mode == asyncAio {
		sz = 8
		c = new(ctx)
		err := c.initAio(sz)
		assert.NoError(t, err)
	} else {
		if ioUringSetupSys <= 0 || ioUringEnterSys <= 0 {
			t.Skip("io_uring doesn't supported")
		}
		sz = 128
		var r ring
		err := setup(uint32(sz), &r, 0)
		assert.NoError(t, err)
		c = new(ctx)
		c.ioUringUserDataPool = sync.Pool{
			New: func() interface{} {
				return &cqUserData{}
			},
		}
		c.r = &r
	}

	c.sz = sz
	c.currentCnt = 0
	c.align = 512
	c.asyncMode = mode
	c.operationsFd = make(map[unsafe.Pointer]*File, sz)
	c.alignedBuffers = make(map[unsafe.Pointer]slice)
	c.newThread = func(int) bool {
		return false
	}
}

func TestCtx_initIoUring(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "android" {
		t.Skip("skipping; linux-only test")
	}
	var c ctx
	err := c.initIoUring(7)
	assert.EqualError(t, err, errBadSize.Error())

	err = c.initIoUring(8)
	assert.NoError(t, err)
	assert.NotNil(t, c.r)
	assert.Equal(t, asyncIoUring, c.asyncMode)
}

func TestCtx_initAio(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "android" {
		t.Skip("skipping; linux-only test")
	}
	var c ctx
	err := c.initAio(8)
	assert.NoError(t, err)
	assert.NotEqual(t, 0, c.aio)
	assert.Equal(t, asyncAio, c.asyncMode)
}
