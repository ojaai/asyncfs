package asyncfs

import (
	"errors"
	"io"
	"os"
	"runtime"
	"sync"
	"syscall"
)

const (
	ModeSync  = 0x1
	ModeAsync = 0x2
)

type (
	File struct {
		fileCtx
		fd               *os.File
		mtx              sync.Mutex
		lastSyncSeek     bool
		mode             uint8
		pos              int64
		path             string
		lastAsyncOpState asyncOpState
	}
)

var ErrNotCompleted = errors.New("operation isn't completed")
var ErrCtxBusy = errors.New("ctx is busy")
var ErrUnknownOperation = errors.New("unknown operation")
var ErrNotSubmittedAio = errors.New("failed aio submit")
var ErrAioError = errors.New("aio error")
var ErrNotSubmittedIoUring = errors.New("failed io_uring submit")
var ErrNotSupported = errors.New("not supported")
var ErrNotImplemented = errors.New("not implemented")
var ErrEOF = io.EOF

func Open(path string, mode int, perm os.FileMode, openMode uint8) (*File, error) {
	return open(path, mode, perm, openMode)
}

func Create(path string, fsMode uint8) (*File, error) {
	return Open(path, syscall.O_RDWR|syscall.O_CREAT|syscall.O_TRUNC, 0666, fsMode)
}

func (f *File) Mode() uint8 {
	return f.mode
}

func (f *File) Path() string {
	return f.path
}

func (f *File) Write(data []uint8) (int, error) {
	var n int
	var err error
	switch f.mode {
	case ModeAsync:
		if err := f.checkAsyncResult(); err != nil {
			return 0, err
		}
		n, err = f.writeAsync(data)
	case ModeSync:
		n, err = f.writeSync(data)
	}
	runtime.KeepAlive(f)
	return n, err
}

func (f *File) WriteSync(data []uint8) (int, error) {
	return f.writeSync(data)
}

func (f *File) Read(data []uint8) (int, error) {
	var n int
	var err error
	switch f.mode {
	case ModeAsync:
		if err := f.checkAsyncResult(); err != nil {
			return 0, err
		}
		n, err = f.readAsync(data)
	case ModeSync:
		n, err = f.readSync(data)
	}
	runtime.KeepAlive(f)
	return n, err
}

func (f *File) ReadSync(data []uint8) (int, error) {
	return f.readSync(data)
}

func (f *File) LastOp() (int, bool, error) {
	if f.Mode() == ModeSync {
		return 0, true, nil
	}
	err := f.checkAsyncResult()
	if err != nil {
		if err == ErrNotCompleted {
			return 0, false, nil
		}
		return 0, false, err
	}
	f.mtx.Lock()
	complete := f.lastAsyncOpState.complete
	res := f.lastAsyncOpState.result
	eof := f.lastAsyncOpState.eof
	f.mtx.Unlock()
	if res < 0 {
		return 0, complete, syscall.Errno(res)
	}
	if eof {
		return 0, complete, ErrEOF
	}
	return int(res), complete, nil
}

func (f *File) Pos() int64 {
	if f.Mode() == ModeSync {
		return f.pos
	}
	_ = f.checkAsyncResult()
	return f.pos
}

func (f *File) Stat() (os.FileInfo, error) {
	if err := f.checkAsyncResult(); err != nil {
		return nil, err
	}

	f.mtx.Lock()
	defer f.mtx.Unlock()

	if !f.lastAsyncOpState.complete {
		return nil, ErrNotCompleted
	}

	return f.fd.Stat()
}

func (f *File) Seek(pos int64, whence int) (int64, error) {
	if f.Mode() == ModeSync {
		n, err := f.fd.Seek(pos, whence)
		if err != nil {
			return n, err
		}
		switch whence {
		case 0:
			f.pos = pos
		case 1:
			f.pos += pos
		case 2:
			f.pos = n
		}
		return f.pos, nil
	}

	if err := f.checkAsyncResult(); err != nil {
		return 0, err
	}

	f.mtx.Lock()
	defer f.mtx.Unlock()

	if !f.lastAsyncOpState.complete {
		return f.pos, nil
	}

	n, err := f.fd.Seek(pos, whence)
	if err != nil {
		return f.pos, err
	}

	switch whence {
	case 0:
		f.pos = pos
	case 1:
		f.pos += pos
	case 2:
		f.pos = n
	default:
		return 0, ErrNotImplemented
	}

	f.lastAsyncOpState.lastOp = OpUnknown
	return f.pos, nil
}

func (f *File) Close() error {
	return f.close()
}

func (f *File) checkAsyncSeek() (int, error) {
	if err := f.checkAsyncResult(); err == ErrNotCompleted {
		return 0, err
	}
	if f.lastSyncSeek {
		if _, err := f.fd.Seek(f.pos, 0); err != nil {
			return 0, err
		}
		f.lastSyncSeek = false
	}
	return 0, nil
}
