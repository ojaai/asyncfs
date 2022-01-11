// +build linux android freebsd darwin

package asyncfs

import (
	"fmt"
	"io"
	"os"
	"syscall"
	"unsafe"
)

const (
	asyncAio     = 0x1
	asyncIoUring = 0x2
)

type (
	fileCtx struct{}
)

func open(path string, flag int, perm os.FileMode, fType uint8) (*File, error) {
	var resFile File
	var fd *os.File
	var mode uint8

	switch fType {
	case ModeAsync:
		f, err := openAsync(path, flag, perm)
		if err != nil {
			return nil, err
		}
		fd = f
		mode = ModeAsync
		resFile.lastAsyncOpState.complete = true
	case ModeSync:
		f, err := os.OpenFile(path, flag, perm)
		if err != nil {
			return nil, err
		}
		fd = f
		mode = ModeSync
	default:
		return nil, fmt.Errorf("unknown type '%v'", fType)
	}

	resFile.path = path
	resFile.fd = fd
	resFile.mode = mode

	return &resFile, nil
}

func (f *File) writeSync(data []uint8) (int, error) {
	if f.mode == ModeAsync {
		if n, err := f.checkAsyncSeek(); err != nil {
			return n, err
		}
	}

	if !c.newThread(len(data)) {
		nn, _, e := syscall.RawSyscall(syscall.SYS_WRITE, f.fd.Fd(), uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)))
		if e != 0 {
			return int(nn), e
		}
		if int(nn) != len(data) {
			return int(nn), io.ErrShortWrite
		}
		f.mtx.Lock()
		f.pos += int64(nn)
		f.mtx.Unlock()
		return int(nn), nil
	}

	n, err := f.fd.Write(data)
	if err != nil {
		return n, err
	}
	f.mtx.Lock()
	f.pos += int64(n)
	f.lastAsyncOpState.lastOp = OpUnknown
	f.mtx.Unlock()
	return n, nil
}

func (f *File) readSync(data []uint8) (int, error) {
	if f.mode == ModeAsync {
		if n, err := f.checkAsyncSeek(); err != nil {
			return n, err
		}
	}

	if !c.newThread(len(data)) {
		nn, _, e := syscall.RawSyscall(syscall.SYS_READ, f.fd.Fd(), uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)))
		if e != 0 {
			return int(nn), e
		}
		if nn == 0 && len(data) > 0 {
			return 0, ErrEOF
		}
		f.mtx.Lock()
		f.pos += int64(nn)
		f.mtx.Unlock()
		return int(nn), nil
	}

	n, err := f.fd.Read(data)
	if err != nil {
		return n, err
	}
	f.mtx.Lock()
	f.pos += int64(n)
	f.lastAsyncOpState.lastOp = OpUnknown
	f.mtx.Unlock()
	return n, nil
}

func (f *File) checkAsyncResult() error {
	if f.Mode() == ModeSync {
		return nil
	}
	f.mtx.Lock()
	if f.lastAsyncOpState.lastOp == OpUnknown {
		f.mtx.Unlock()
		return nil
	}
	if !f.lastAsyncOpState.complete {
		f.mtx.Unlock()
		if err := f.fillOpState(); err != nil {
			return err
		}
		f.mtx.Lock()
		if !f.lastAsyncOpState.complete {
			f.mtx.Unlock()
			return ErrNotCompleted
		}
	}
	if f.lastAsyncOpState.result < 0 {
		f.mtx.Unlock()
		return fmt.Errorf("async error: %d", f.lastAsyncOpState.result)
	}
	f.mtx.Unlock()
	return nil
}
