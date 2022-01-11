// +build linux android

package asyncfs

import (
	"github.com/stretchr/testify/assert"
	"os"
	"runtime"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func Test_asyncRWAio(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "android" {
		t.Skip("skipping; linux-only test")
	}
	const sz = 8
	var c1 ctx
	err := c1.initAio(sz)
	assert.NoError(t, err)
	c1.sz = sz
	c1.align = 512
	c1.operationsFd = make(map[unsafe.Pointer]*File, sz)
	c1.alignedBuffers = make(map[unsafe.Pointer]slice)
	c = &c1

	f, err := Open("/tmp/aio", syscall.O_RDWR|syscall.O_CREAT, 0644, ModeAsync)
	assert.NoError(t, err)
	defer os.Remove(f.path)

	buf := make([]uint8, 4095)
	n, err := f.asyncRWAio(iocbCmdPwrite, buf)
	assert.Equal(t, 0, n)
	assert.EqualError(t, err, ErrUnalignedData.Error())

	c.align = 11
	buf = make([]uint8, 4096)
	n, err = f.asyncRWAio(iocbCmdPwrite, buf)
	assert.Equal(t, 0, n)
	assert.EqualError(t, err, ErrUnalignedData.Error())

	c.align = 512
	buf = AllocBuf(4096)
	n, err = f.asyncRWAio(iocbCmdPwrite, buf)
	assert.Equal(t, 0, n)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(c.operationsFd))
}

func Test_fillStatesAio(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "android" {
		t.Skip("skipping; linux-only test")
	}

	const sz = 8
	var c1 ctx
	err := c1.initAio(sz)
	assert.NoError(t, err)
	c = &c1
	c.sz = sz
	c.align = 512
	c.operationsFd = make(map[unsafe.Pointer]*File, sz)
	c.alignedBuffers = make(map[unsafe.Pointer]slice)

	f, err := Open("/tmp/aio", syscall.O_RDWR|syscall.O_CREAT, 0644, ModeAsync)
	assert.NoError(t, err)
	defer os.Remove(f.path)

	buf := make([]uint8, 4096)
	n, err := f.writeAsync(buf)
	assert.Equal(t, 0, n)
	assert.NoError(t, err)
	assert.False(t, f.lastAsyncOpState.complete)

	time.Sleep(time.Millisecond * 10)

	err = fillStatesAio(c)
	assert.NoError(t, err)
	assert.True(t, f.lastAsyncOpState.complete)
	assert.Equal(t, int64(4096), f.pos)

	for i := 0; i < sz+1; i++ {
		n, err = f.writeAsync(buf)
		assert.Equal(t, 0, n)
		assert.NoError(t, err)
		f.pos += int64(len(buf))
	}
	f.pos = 0
	time.Sleep(time.Millisecond * 10)
	err = fillStatesAio(c)
	assert.NoError(t, err)
	assert.True(t, f.lastAsyncOpState.complete)
	assert.Equal(t, int64(4096*(sz+1)+4096), f.pos)

	c.aio = 0
	err = fillStatesAio(c)
	assert.Error(t, err)
	assert.EqualError(t, syscall.EINVAL, err.Error())
}
