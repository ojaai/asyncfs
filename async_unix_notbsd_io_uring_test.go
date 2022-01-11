// +build linux android

package asyncfs

import (
	"github.com/stretchr/testify/assert"
	"os"
	"runtime"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func TestFile_Setup(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "android" {
		t.Skip("skipping; linux-only test")
	}
	if ioUringSetupSys <= 0 || ioUringEnterSys <= 0 {
		t.Skip("io_uring doesn't supported")
	}

	const sz = 16
	var r ring

	err := setup(sz, &r, 0)
	assert.NoError(t, err)
	assert.True(t, r.ringFd > 0)
	assert.True(t, r.sq.ringSz > 0)
	assert.True(t, r.cq.ringSz > 0)
	assert.NotNil(t, r.sq.sqRingFd)

	assert.NotNil(t, r.cq.cqRingFd)
	if r.features&ioringFeatSingleMmap != 0 {
		assert.Equal(t, r.sq.sqRingFd, r.cq.cqRingFd)
	}

	assert.Equal(t, sz, len(r.sq.array))
	assert.Equal(t, sz, len(r.sq.sqes))

	assert.Greater(t, len(r.cq.cqes), sz)
	assert.True(t, len(r.cq.cqes)%2 == 0)
}

func Test_rw(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "android" {
		t.Skip("skipping; linux-only test")
	}
	if ioUringSetupSys <= 0 || ioUringEnterSys <= 0 {
		t.Skip("io_uring doesn't supported")
	}

	assert.False(t, rw(0x0, nil, 0, nil, nil, 0, 0))
	assert.False(t, rw(ioringOpReadv, nil, 0, nil, nil, 0, 0))
	assert.False(t, rw(ioringOpReadv, &sqe{}, 0, nil, nil, 0, 0))
	assert.False(t, rw(ioringOpReadv, &sqe{}, 0, unsafe.Pointer(uintptr(0x1)), nil, 0, 0))
	assert.False(t, rw(ioringOpReadv, &sqe{}, 0, nil, unsafe.Pointer(uintptr(0x1)), 0, 0))
	assert.True(t, rw(ioringOpReadv, &sqe{}, 0, unsafe.Pointer(uintptr(0x1)), unsafe.Pointer(uintptr(0x1)), 0, 0))
}

func Test_asyncRWIoUring(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "android" {
		t.Skip("skipping; linux-only test")
	}
	if ioUringSetupSys <= 0 || ioUringEnterSys <= 0 {
		t.Skip("io_uring doesn't supported")
	}

	const sz = 16
	var r ring

	err := setup(sz, &r, 0)
	assert.NoError(t, err)

	c = new(ctx)
	c.ioUringUserDataPool = sync.Pool{
		New: func() interface{} {
			return &cqUserData{}
		},
	}
	c.r = &r

	f := &File{
		pos: 100,
	}
	c.operationsFd = make(map[unsafe.Pointer]*File)
	n, err := f.asyncRWIoUring(ioringOpReadv, []uint8{0x0, 0x0, 0x0})
	assert.Equal(t, 0, n)
	assert.NoError(t, err)
	assert.Equal(t, 1, c.currentCnt)
	assert.GreaterOrEqual(t, len(c.operationsFd), 1)

	c.ioUringUserDataPool = sync.Pool{
		New: func() interface{} {
			return cqUserData{} // not a pointer
		},
	}
	n, err = f.asyncRWIoUring(ioringOpNop, []uint8{0x0, 0x0, 0x0})
	assert.Equal(t, 0, n)
	assert.Equal(t, ErrNotSubmittedIoUring, err)
	assert.Equal(t, 1, c.currentCnt)

	// flush -> toSubmit == 0
	*(c.r.sq.ktail) = 0
	*(c.r.sq.khead) = 1
	c.r.sq.sqeTail = 0
	c.r.sq.sqeHead = 0
	n, err = f.asyncRWIoUring(ioringOpWritev, []uint8{0x0, 0x0, 0x0})
	assert.Equal(t, 0, n)
	assert.Equal(t, errFailedSq, err)
	assert.Equal(t, 2, c.currentCnt)
}

func TestFile_fullQueue(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "android" {
		t.Skip("skipping; linux-only test")
	}
	if ioUringSetupSys <= 0 || ioUringEnterSys <= 0 {
		t.Skip("io_uring doesn't supported")
	}
	const sz = 8
	var err error
	err = NewCtx(sz, sameThreadLim, nil, nil)
	if c.r == nil {
		t.Skip("io_uring doesn't supported")
	}
	assert.NoError(t, err)

	f, err := Open("/tmp/io_uring_write", syscall.O_RDWR|syscall.O_CREAT, 0644, ModeAsync)
	assert.NoError(t, err)
	defer os.Remove(f.path)

	buf := make([]uint8, 4096)
	for i := 0; i < sz; i++ {
		_, err = f.writeAsync(buf)
		assert.NoError(t, err)
	}
	_, err = f.Write(buf)
	assert.Error(t, err)
	assert.Equal(t, sz, c.currentCnt)

	time.Sleep(time.Millisecond * 100)

	err = f.checkAsyncResult()
	assert.NoError(t, err)
	assert.Equal(t, 0, c.currentCnt)

	_, err = f.writeAsync(buf)
	assert.NoError(t, err)
	assert.Equal(t, 1, c.currentCnt)
}

func TestFile_cap(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "android" {
		t.Skip("skipping; linux-only test")
	}
	if ioUringSetupSys <= 0 || ioUringEnterSys <= 0 {
		t.Skip("io_uring doesn't supported")
	}
	const sz = 8
	var err error
	err = NewCtx(sz, sameThreadLim, nil, nil)
	if c.r == nil {
		t.Skip("io_uring doesn't supported")
	}
	assert.NoError(t, err)

	f, err := Open("/tmp/io_uring_read", syscall.O_RDWR|syscall.O_CREAT, 0644, ModeAsync)
	assert.NoError(t, err)
	defer os.Remove(f.path)

	n, err := f.WriteSync(make([]uint8, 4096))
	assert.Equal(t, 4096, n)
	assert.NoError(t, err)
	assert.Equal(t, int64(4096), f.pos)

	buf := make([]uint8, 2048, 3000)
	m, err := f.Seek(0, 0)
	assert.Equal(t, int64(0), m)
	assert.NoError(t, err)
	assert.Equal(t, int64(0), f.pos)

	_, err = f.Read(buf)
	assert.NoError(t, err)
	time.Sleep(time.Millisecond * 100)
	err = f.checkAsyncResult()
	assert.NoError(t, err)
}
