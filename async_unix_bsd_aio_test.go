// +build freebsd darwin

package asyncfs

import (
	"github.com/stretchr/testify/assert"
	"io/ioutil"
	"os"
	"syscall"
	"testing"
	"time"
)

func Test_bsd_asyncRWAio(t *testing.T) {
	prepare(t, 0)

	f, err := Open("/tmp/aio", syscall.O_RDWR|syscall.O_CREAT, 0644, ModeAsync)
	assert.NoError(t, err)
	defer os.Remove(f.path)

	buf := make([]uint8, 2000)
	buf[0] = 'a'
	n, err := f.asyncRWAio(OpWrite, buf)
	assert.Equal(t, 0, n)
	assert.NoError(t, err)
	assert.NotEqual(t, 0, len(c.operationsFd))
}

func Test_bsd_asyncWrite(t *testing.T) {
	prepare(t, 0)

	f, err := Open("/tmp/aio", syscall.O_RDWR|syscall.O_CREAT, 0644, ModeAsync)
	assert.NoError(t, err)
	defer os.Remove(f.path)

	buf := make([]uint8, 2000)
	for i := range buf {
		buf[i] = uint8(i)
	}

	_, err = f.asyncRWAio(OpWrite, buf)
	assert.NoError(t, err)

	t1 := time.Now()
	for i := 0; ; i++ {
		n, ok, err := f.LastOp()
		if !ok {
			if time.Now().Sub(t1) > time.Second {
				t.Fatal("too long")
			}
			continue
		}
		assert.Equal(t, int64(0), n)
		assert.NoError(t, err)
		break
	}

	out, err := ioutil.ReadFile(f.path)
	assert.NoError(t, err)
	for i := range out {
		assert.Equal(t, uint8(i), buf[i])
	}
}

func Test_bsd_asyncRead(t *testing.T) {
	prepare(t, 0)

	f, err := Open("/tmp/aio", syscall.O_RDWR|syscall.O_CREAT, 0644, ModeAsync)
	assert.NoError(t, err)
	defer os.Remove(f.path)

	buf := make([]uint8, 2000)
	for i := range buf {
		buf[i] = uint8(i)
	}

	_, err = f.asyncRWAio(OpWrite, buf)
	f.lastAsyncOpState.lastOp = OpWrite
	f.lastAsyncOpState.complete = false
	assert.NoError(t, err)
	t1 := time.Now()
	for i := 0; ; i++ {
		n, ok, err := f.LastOp()
		if !ok {
			if time.Now().Sub(t1) > time.Second {
				t.Fatal("too long")
			}
			continue
		}
		assert.Equal(t, int64(2000), n)
		assert.NoError(t, err)
		break
	}

	buf = make([]uint8, 2000)
	f.pos = 0
	_, err = f.asyncRWAio(OpRead, buf)
	assert.NoError(t, err)
	f.lastAsyncOpState.complete = false
	f.lastAsyncOpState.lastOp = OpRead
	t1 = time.Now()
	for i := 0; ; i++ {
		err := f.fillOpState()
		assert.NoError(t, err)
		if !f.lastAsyncOpState.complete {
			if time.Now().Sub(t1) > time.Second {
				t.Fatal("too long")
			}
			continue
		}
		break
	}

	for i := range buf {
		assert.Equal(t, uint8(i), buf[i])
	}
}

func Test_fillStatesAio(t *testing.T) {
	prepare(t, 0)

	f, err := Open("/tmp/aio", syscall.O_RDWR|syscall.O_CREAT, 0644, ModeAsync)
	assert.NoError(t, err)
	defer os.Remove(f.path)

	buf := make([]uint8, 4000)
	n, err := f.writeAsync(buf)
	assert.Equal(t, 0, n)
	assert.NoError(t, err)
	assert.False(t, f.lastAsyncOpState.complete)

	t1 := time.Now()
	for i := 0; ; i++ {
		err := f.fillOpState()
		assert.NoError(t, err)
		if !f.lastAsyncOpState.complete {
			if time.Now().Sub(t1) > time.Second {
				t.Fatal("too long")
			}
			continue
		}
		assert.Equal(t, int64(4000), f.pos)
		break
	}
}
