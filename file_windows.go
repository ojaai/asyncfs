// +build windows

package asyncfs

import (
	"fmt"
	"os"
	"runtime"
	"syscall"
	"unsafe"
)

type (
	fileCtx struct {
		ro             *syscall.Overlapped
		wo             *syscall.Overlapped
		processedBytes uint64
		toRwBytes      uint64
		wait           bool
	}
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
		resFile.setOverlapped(true, true)
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
		return f.rwSync(c.nWriteFile, data)
	}
	n, err := f.fd.Write(data)
	if err != nil {
		return n, err
	}
	f.mtx.Lock()
	f.pos += int64(n)
	f.lastAsyncOpState.lastOp = OpUnknown
	f.mtx.Unlock()
	return n, err
}

func (f *File) readSync(data []uint8) (int, error) {
	if f.mode == ModeAsync {
		if n, err := f.checkAsyncSeek(); err != nil {
			return n, err
		}
		return f.rwSync(c.nReadFile, data)
	}
	n, err := f.fd.Read(data)
	if err != nil {
		return n, err
	}
	f.mtx.Lock()
	f.pos += int64(n)
	f.lastAsyncOpState.lastOp = OpUnknown
	f.mtx.Unlock()
	return n, err
}

func (f *File) rwSync(nOp uint64, data []uint8) (int, error) {
	if err := f.checkAsyncResult(); err != nil {
		return 0, err
	}
	n, err := f.rwAsync(nOp, data)
	if err != nil {
		return 0, err
	}
	f.mtx.Lock()
	f.wait = true
	f.mtx.Unlock()
	for {
		_, ok, err := f.LastOp()
		if err != nil {
			return 0, err
		}
		if ok {
			break
		}
		runtime.Gosched()
	}
	f.mtx.Lock()
	f.pos += int64(n)
	f.lastAsyncOpState.lastOp = OpUnknown
	f.mtx.Unlock()
	return int(f.processedBytes), nil
}

func (f *File) checkAsyncResult() error {
	if f.Mode() == ModeSync {
		return nil
	}

	f.mtx.Lock()
	defer f.mtx.Unlock()

	if f.lastAsyncOpState.complete {
		f.processedBytes = 0
		return nil
	}

	var n uint64
	var ov *syscall.Overlapped

	switch f.lastAsyncOpState.lastOp {
	case OpRead:
		ov = f.ro
	case OpWrite:
		ov = f.wo
	default:
		return nil
	}

	var wait int32
	if f.wait {
		wait = 1
	}

	r1, _, e := syscall.Syscall6(uintptr(c.nGetOverlappedResult), 4, f.fd.Fd(), uintptr(unsafe.Pointer(ov)), uintptr(unsafe.Pointer(&n)), uintptr(wait), 0, 0)
	f.processedBytes += n
	if r1 == 0 {
		if e == syscall.ERROR_IO_PENDING || e == 996 { // WSA_IO_INCOMPLETE(996)
			if f.wait {
				return e
			}
			return ErrNotCompleted
		}

		f.lastAsyncOpState.complete = true

		if e == syscall.ERROR_HANDLE_EOF {
			f.lastAsyncOpState.eof = true
			return ErrEOF
		}

		return e
	}

	f.lastAsyncOpState.complete = true
	f.pos += int64(f.processedBytes)
	f.lastAsyncOpState.result = int64(n)
	f.lastAsyncOpState.eof = f.lastAsyncOpState.lastOp == OpRead && n == 0 && f.toRwBytes > 0

	return nil
}

func (f *File) close() error {
	if f.mode == ModeAsync {
		f.ro = nil
		f.wo = nil
	}
	return f.fd.Close()
}
