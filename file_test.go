package asyncfs

import (
	"bytes"
	"fmt"
	"github.com/stretchr/testify/assert"
	"io"
	"os"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"
)

const sameThreadLim = 1024 * 1024

func TestFile_Write_Async(t *testing.T) {
	for _, x := range steps() {
		prepare(t, x)
		func() {
			path := "./asyncWrite"
			f, err := Open(path, syscall.O_RDWR|syscall.O_CREAT, 0644, ModeAsync)
			assert.NoError(t, err)
			defer func() {
				f.Close()
				os.Remove(f.path)
			}()

			buf := AllocBuf(2048)
			for i := range buf {
				buf[i] = uint8(i)
			}
			n, err := f.Write(buf)
			assert.Equal(t, 0, n)
			assert.NoError(t, err)
			assert.False(t, f.lastAsyncOpState.complete)
			assert.Equal(t, OpWrite, f.lastAsyncOpState.lastOp)
			assert.Equal(t, int64(0), f.pos)

			t1 := time.Now()
			for i := 0; ; i++ {
				n, ok, err := f.LastOp()
				assert.NoError(t, err)
				if !ok {
					if time.Now().Sub(t1) > time.Second {
						t.Fatal("too long")
					}
					continue
				}
				assert.Equal(t, len(buf), n)
				break
			}

			assert.True(t, f.lastAsyncOpState.complete)
			assert.Equal(t, OpWrite, f.lastAsyncOpState.lastOp)
			assert.Equal(t, int64(len(buf)), f.pos)

			f.pos = 0
			_, err = f.Seek(0, 0)
			assert.NoError(t, err)
			out := AllocBuf(2048)
			n, err = f.ReadSync(out)
			assert.NoError(t, err)
			assert.Equal(t, 2048, n)
			for i := range out {
				assert.Equal(t, buf[i], out[i])
			}
		}()
	}
}

func TestFile_Write_Async_Concurrent(t *testing.T) {
	for _, x := range steps() {
		prepare(t, x)
		func() {
			sz := 100
			fds := make([]*File, 0, sz)
			path := "./asyncWrite_concurrent_"

			for i := 0; i < sz; i++ {
				f, err := Open(path+strconv.Itoa(i), syscall.O_RDWR|syscall.O_CREAT, 0644, ModeAsync)
				if err != nil {
					t.Error(err.Error())
					continue
				}
				fds = append(fds, f)
			}
			defer func() {
				for i := range fds {
					fds[i].Close()
					assert.NoError(t, os.Remove(fds[i].path))
				}
			}()

			buf := AllocBuf(2048)
			for i := range buf {
				buf[i] = uint8(i)
			}
			m := sync.Mutex{}
			cond := sync.NewCond(&m)

			wg := &sync.WaitGroup{}
			fn := func(f *File) {
				defer wg.Done()
				cond.L.Lock()
				cond.Wait()
				cond.L.Unlock()
				n, err := f.Write(buf)
				assert.NoError(t, err)
				assert.Equal(t, 0, n)
				for {
					n, ok, err := f.LastOp()
					assert.NoError(t, err)
					if !ok {
						continue
					}
					assert.Equal(t, len(buf), n)
					break
				}
			}

			wg.Add(sz)
			for i := 0; i < sz; i++ {
				go fn(fds[i])
			}
			time.Sleep(time.Millisecond * 100)
			cond.L.Lock()
			cond.Broadcast()
			cond.L.Unlock()

			wg.Wait()

			for i := 0; i < sz; i++ {
				assert.True(t, fds[i].lastAsyncOpState.complete)
				assert.Equal(t, OpWrite, fds[i].lastAsyncOpState.lastOp)
				assert.Equal(t, int64(len(buf)), fds[i].pos)

				_, err := fds[i].Seek(0, 0)
				assert.NoError(t, err)
				out := AllocBuf(2048)
				n, err := fds[i].ReadSync(out)
				assert.NoError(t, err)
				assert.Equal(t, 2048, n)
				for i := range out {
					assert.Equal(t, buf[i], out[i])
				}
			}
		}()
	}
}

func TestFile_Read_Async(t *testing.T) {
	for _, x := range steps() {
		prepare(t, x)
		func() {
			path := "./asyncRead"

			f, err := Open(path, syscall.O_RDWR|syscall.O_CREAT, 0644, ModeAsync)
			assert.NoError(t, err)
			defer func() {
				f.Close()
				os.Remove(f.path)
			}()

			buf := AllocBuf(2048)
			for i := range buf {
				buf[i] = uint8(i)
			}
			n, err := f.WriteSync(buf)
			assert.Equal(t, len(buf), n)
			assert.NoError(t, err)
			assert.Equal(t, int64(len(buf)), f.Pos())
			assert.Equal(t, OpUnknown, f.lastAsyncOpState.lastOp)
			out := AllocBuf(len(buf))
			f.pos = 0
			n, err = f.Read(out)
			assert.NoError(t, err)
			assert.Equal(t, 0, n)
			assert.Equal(t, OpRead, f.lastAsyncOpState.lastOp)
			assert.Equal(t, int64(0), f.pos)
			assert.False(t, f.lastAsyncOpState.complete)
			t1 := time.Now()
			for i := 0; ; i++ {
				n, ok, err := f.LastOp()
				assert.NoError(t, err)
				if !ok {
					if time.Now().Sub(t1) > time.Second {
						t.Fatal("too long")
					}
					continue
				}
				assert.Equal(t, len(buf), n)
				break
			}
			assert.True(t, f.lastAsyncOpState.complete)
			assert.Equal(t, OpRead, f.lastAsyncOpState.lastOp)
			assert.Equal(t, int64(len(out)), f.pos)
			for i := range out {
				assert.Equal(t, buf[i], out[i])
			}

			out = AllocBuf(len(buf))
			newPos := int64(len(buf) - 512)
			f.pos = newPos
			n, err = f.Read(out)
			assert.NoError(t, err)
			assert.Equal(t, 0, n)
			assert.Equal(t, OpRead, f.lastAsyncOpState.lastOp)
			assert.Equal(t, newPos, f.pos)
			assert.False(t, f.lastAsyncOpState.complete)
			t1 = time.Now()
			for i := 0; ; i++ {
				n, ok, err := f.LastOp()
				assert.NoError(t, err)
				if !ok {
					if time.Now().Sub(t1) > time.Second {
						t.Fatal("too long")
					}
					continue
				}
				assert.Equal(t, 512, n)
				break
			}
			assert.True(t, f.lastAsyncOpState.complete)
			assert.Equal(t, OpRead, f.lastAsyncOpState.lastOp)

			out = AllocBuf(len(buf))
			n, err = f.Read(out)
			assert.NoError(t, err)
			assert.Equal(t, 0, n)
			assert.Equal(t, OpRead, f.lastAsyncOpState.lastOp)
			assert.False(t, f.lastAsyncOpState.complete)
			t1 = time.Now()
			for i := 0; ; i++ {
				n, ok, err := f.LastOp()
				if !ok {
					if time.Now().Sub(t1) > time.Second {
						t.Fatal("too long")
					}
					continue
				}
				assert.EqualError(t, err, io.EOF.Error())
				assert.Equal(t, 0, n)
				break
			}
		}()
	}
}

func TestFile_Read_Sync_Async(t *testing.T) {
	for _, x := range steps() {
		prepare(t, x)
		func() {
			path := "./syncAsyncRead"
			fd, err := os.OpenFile(path, syscall.O_RDWR|syscall.O_CREAT, 0644)
			assert.NoError(t, err)
			defer func() {
				fd.Close()
				os.Remove(path)
			}()

			buf := make([]uint8, 2048)
			for i := range buf {
				buf[i] = uint8(i)
			}
			n, err := fd.Write(buf)
			assert.NoError(t, err)
			assert.Equal(t, 2048, n)

			f, err := Open(path, syscall.O_RDONLY, 0644, ModeAsync)
			defer f.Close()
			assert.NoError(t, err)

			out := AllocBuf(512)
			n, err = f.Read(out)
			assert.NoError(t, err)
			assert.Equal(t, 0, n)
			assert.Equal(t, OpRead, f.lastAsyncOpState.lastOp)
			assert.Equal(t, int64(0), f.pos)
			assert.False(t, f.lastAsyncOpState.complete)
			t1 := time.Now()
			for i := 0; ; i++ {
				n, ok, err := f.LastOp()
				assert.NoError(t, err)
				if !ok {
					if time.Now().Sub(t1) > time.Second {
						t.Fatal("too long")
					}
					continue
				}
				assert.Equal(t, len(out), n)
				break
			}
			assert.True(t, f.lastAsyncOpState.complete)
			assert.Equal(t, OpRead, f.lastAsyncOpState.lastOp)
			assert.Equal(t, int64(512), f.pos)
			for i := range out {
				assert.Equal(t, buf[i], out[i])
			}

			n, err = f.ReadSync(out)
			assert.NoError(t, err)
			assert.Equal(t, 512, n)
			assert.Equal(t, int64(1024), f.pos)
			for i := range out {
				assert.Equal(t, buf[i+512], out[i])
			}

			out = AllocBuf(1536)
			n, err = f.Read(out)
			assert.NoError(t, err)
			assert.Equal(t, 0, n)
			assert.Equal(t, OpRead, f.lastAsyncOpState.lastOp)
			assert.Equal(t, int64(1024), f.pos)
			assert.False(t, f.lastAsyncOpState.complete)
			t1 = time.Now()
			for i := 0; ; i++ {
				n, ok, err := f.LastOp()
				assert.NoError(t, err)
				if !ok {
					if time.Now().Sub(t1) > time.Second {
						t.Fatal("too long")
					}
					continue
				}
				assert.Equal(t, 1024, n)
				break
			}
			assert.True(t, f.lastAsyncOpState.complete)
			assert.Equal(t, OpRead, f.lastAsyncOpState.lastOp)
			assert.Equal(t, int64(2048), f.pos)
			for i := range out[:1024] {
				assert.Equal(t, buf[i+1024], out[i])
			}

			n, err = f.ReadSync(out)
			assert.EqualError(t, err, ErrEOF.Error())
			assert.Equal(t, 0, n)
			assert.Equal(t, int64(2048), f.pos)

			out = AllocBuf(len(buf))
			n, err = f.Read(out)
			assert.NoError(t, err)
			assert.Equal(t, 0, n)
			assert.Equal(t, OpRead, f.lastAsyncOpState.lastOp)
			assert.False(t, f.lastAsyncOpState.complete)
			t1 = time.Now()
			for i := 0; ; i++ {
				n, ok, err := f.LastOp()
				if !ok {
					if time.Now().Sub(t1) > time.Second {
						t.Fatal("too long")
					}
					continue
				}
				assert.EqualError(t, err, io.EOF.Error())
				assert.Equal(t, 0, n)
				break
			}
		}()
	}
}

func TestFile_Big(t *testing.T) {
	for _, x := range steps() {
		prepare(t, x)
		// 1mb, 128mb, 512mb, 1gb, 2gb
		for _, sz := range []int{1024 * 1024, 1024 * 1024 * 128, 1024 * 1024 * 512 /* 1024*1024*1024, /* 1024*1024*1024*2 */} { // check ur free RAM
			func() {
				defer func() {
					if e := recover(); e != nil {
						t.Skip(fmt.Sprintf("recovered: %+v", e))
					}
				}()
				path := "./fileBig"
				f, err := Open(path, syscall.O_RDWR|syscall.O_CREAT, 0644, ModeAsync)
				assert.NoError(t, err)
				defer func() {
					f.Close()
					os.Remove(path)
				}()

				buf := AllocBuf(sz)
				for i := range buf {
					buf[i] = uint8(i)
				}
				n, err := f.Write(buf)
				assert.Equal(t, 0, n)
				assert.NoError(t, err)

				t1 := time.Now()
				for {
					n, ok, err := f.LastOp()
					assert.NoError(t, err)
					if !ok {
						if time.Now().Sub(t1) > time.Second*5 {
							t.Fatal("too long")
						}
						continue
					}
					assert.Equal(t, sz, n)
					break
				}
				_, err = f.Seek(0, 0)
				assert.NoError(t, err)
				out := AllocBuf(sz)
				n, err = f.ReadSync(out)
				assert.NoError(t, err)
				assert.Equal(t, sz, n)
				if !bytes.Equal(buf, out) {
					t.Fatal("buf isn't equal out")
				}
			}()
		}
	}
}

func TestFile_Sync(t *testing.T) {
	for _, x := range steps() {
		prepare(t, x)
		func() {
			path := "./sync"
			f, err := Open(path, syscall.O_RDWR|syscall.O_CREAT, 0644, ModeSync)
			assert.NoError(t, err)
			defer func() {
				f.Close()
				os.Remove(f.path)
			}()

			out := AllocBuf(1024)
			n, err := f.Read(out)
			assert.Equal(t, 0, n)
			assert.EqualError(t, err, ErrEOF.Error())

			in := AllocBuf(1024)
			for i := 0; i < len(in); i++ {
				in[i] = uint8(i)
			}
			n, err = f.Write(in)
			assert.Equal(t, 1024, n)
			assert.NoError(t, err)

			n, err = f.Read(out)
			assert.Equal(t, 0, n)
			assert.EqualError(t, err, ErrEOF.Error())
			assert.Equal(t, int64(1024), f.pos)

			pos, err := f.Seek(512, 0)
			assert.Equal(t, int64(512), pos)
			assert.Equal(t, int64(512), f.pos)
			assert.NoError(t, err)

			n, err = f.Read(out)
			assert.Equal(t, 512, n)
			assert.NoError(t, err)
			assert.Equal(t, int64(1024), f.pos)

			n, err = f.Read(out)
			assert.Equal(t, 0, n)
			assert.EqualError(t, err, ErrEOF.Error())
			assert.Equal(t, int64(1024), f.pos)
		}()
	}
}

func TestFile_Stat(t *testing.T) {
	for _, x := range steps() {
		prepare(t, x)
		func() {
			path := "./stat"
			f, err := Open(path, syscall.O_RDWR|syscall.O_CREAT, 0644, ModeAsync)
			assert.NoError(t, err)
			defer func() {
				f.Close()
				os.Remove(f.path)
			}()

			buf := AllocBuf(1048576)
			for i := range buf {
				buf[i] = uint8(i)
			}
			n, err := f.writeSync(buf)
			assert.Equal(t, 1048576, n)
			assert.NoError(t, err)
			assert.Equal(t, int64(len(buf)), f.Pos())
			assert.Equal(t, OpUnknown, f.lastAsyncOpState.lastOp)

			info, err := f.Stat()
			assert.NoError(t, err)
			assert.Equal(t, "stat", info.Name())
			assert.Equal(t, int64(1048576), info.Size())
			assert.Equal(t, false, info.IsDir())

			_, _ = f.Seek(0, 0)

			out := AllocBuf(1048576)
			_, _ = f.Read(out)
			info, err = f.Stat()
			if err != nil {
				assert.EqualError(t, err, ErrNotCompleted.Error())
			}
		}()
	}
}

func TestFile_Seek(t *testing.T) {
	for _, x := range steps() {
		prepare(t, x)
		func() {
			path := "./seek"

			f, err := Open(path, syscall.O_RDWR|syscall.O_CREAT, 0644, ModeAsync)
			assert.NoError(t, err)
			defer func() {
				f.Close()
				os.Remove(f.path)
			}()

			buf := AllocBuf(2048)
			for i := range buf {
				buf[i] = uint8(i)
			}
			n, err := f.Write(buf)
			assert.Equal(t, 0, n)
			assert.NoError(t, err)
			assert.False(t, f.lastAsyncOpState.complete)
			assert.Equal(t, OpWrite, f.lastAsyncOpState.lastOp)
			assert.Equal(t, int64(0), f.pos)

			t1 := time.Now()
			for i := 0; ; i++ {
				n, ok, err := f.LastOp()
				assert.NoError(t, err)
				if !ok {
					if time.Now().Sub(t1) > time.Second {
						t.Fatal("too long")
					}
					continue
				}
				assert.Equal(t, len(buf), n)
				break
			}

			assert.Equal(t, int64(len(buf)), f.pos)

			newPos := int64(512)
			pos, err := f.Seek(newPos, 0)
			assert.NoError(t, err)
			assert.Equal(t, newPos, pos)
			assert.Equal(t, OpUnknown, f.lastAsyncOpState.lastOp)

			buf1 := AllocBuf(512)
			for i := range buf1 {
				buf1[i] = 'f'
			}
			n, err = f.Write(buf1)
			assert.Equal(t, 0, n)
			assert.NoError(t, err)
			assert.False(t, f.lastAsyncOpState.complete)
			assert.Equal(t, OpWrite, f.lastAsyncOpState.lastOp)
			assert.Equal(t, newPos, f.pos)

			t1 = time.Now()
			for i := 0; ; i++ {
				n, ok, err := f.LastOp()
				assert.NoError(t, err)
				if !ok {
					if time.Now().Sub(t1) > time.Second {
						t.Fatal("too long")
					}
					continue
				}
				assert.Equal(t, len(buf1), n)
				break
			}
			time.Sleep(time.Millisecond * 100)

			f.pos = 0
			_, err = f.Seek(0, 0)
			assert.NoError(t, err)
			out := AllocBuf(2048)
			n, err = f.ReadSync(out)
			assert.NoError(t, err)
			assert.Equal(t, 2048, n)
			assert.Equal(t, len(buf), len(out))

			for i := range out[:newPos] {
				assert.Equal(t, buf[i], out[i])
			}
			for i, x := range out[newPos : int(newPos)+len(buf1)] {
				assert.Equal(t, buf1[i], x)
			}
			for i, x := range out[int(newPos)+len(buf1):] {
				assert.Equal(t, buf[int64(i)+newPos], x)
			}
		}()
	}
}
