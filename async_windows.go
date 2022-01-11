package asyncfs

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

func openAsync(path string, flag int, perm os.FileMode) (*os.File, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := syscall.CreateFile(p,
		syscall.GENERIC_READ|syscall.GENERIC_WRITE,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE,
		nil,
		syscall.OPEN_ALWAYS,
		syscall.FILE_ATTRIBUTE_NORMAL|syscall.FILE_FLAG_OVERLAPPED,
		0)

	if err != nil {
		return nil, err
	}

	return os.NewFile(uintptr(h), path), nil
}

func (f *File) setOverlapped(r bool, w bool) {
	if r {
		f.ro = &syscall.Overlapped{}
	}
	if w {
		f.wo = &syscall.Overlapped{}
	}
}

func (f *File) rwAsync(op uint64, data []uint8) (int, error) {
	var n uint64
	var lastOp int
	var ov *syscall.Overlapped

	switch op {
	case c.nWriteFile:
		lastOp = OpWrite
		ov = f.wo
	case c.nReadFile:
		lastOp = OpRead
		ov = f.ro
	default:
		return 0, ErrUnknownOperation
	}

	f.mtx.Lock()
	defer f.mtx.Unlock()

	f.wait = false
	f.processedBytes = 0
	f.lastSyncSeek = true
	f.toRwBytes = uint64(len(data))
	f.lastAsyncOpState.complete = false
	f.lastAsyncOpState.lastOp = lastOp

	ov.Internal = 0
	ov.InternalHigh = 0
	ov.HEvent = 0
	ov.Offset = uint32(f.pos)
	ov.OffsetHigh = uint32(f.pos >> 32)

	r1, _, e := syscall.Syscall6(uintptr(op), 5, f.fd.Fd(), uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)), uintptr(unsafe.Pointer(&n)), uintptr(unsafe.Pointer(ov)), 0)
	if r1 == 0 {
		if e == syscall.ERROR_IO_PENDING {
			f.processedBytes = n
			return int(n), nil
		} else if e == syscall.ERROR_HANDLE_EOF {
			f.lastAsyncOpState.eof = true
			return 0, ErrEOF
		} else {
			return 0, fmt.Errorf("async error: '%s'", e.Error())
		}
	}

	f.processedBytes = n
	f.lastAsyncOpState.complete = true
	f.lastAsyncOpState.result = int64(len(data))
	f.lastAsyncOpState.eof = f.lastAsyncOpState.lastOp == OpRead && n == 0 && f.toRwBytes > 0

	return int(n), nil
}

func (f *File) writeAsync(data []uint8) (int, error) {
	return f.rwAsync(c.nWriteFile, data)
}

func (f *File) readAsync(data []uint8) (int, error) {
	return f.rwAsync(c.nReadFile, data)
}
