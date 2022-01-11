// +build linux android

package asyncfs

import (
	"fmt"
	"os"
	"syscall"
)

func openAsync(path string, flag int, perm os.FileMode) (*os.File, error) {
	if c.asyncMode == asyncAio {
		flag |= syscall.O_DIRECT
	}
	return os.OpenFile(path, flag, perm)
}

func (f *File) writeAsync(data []uint8) (int, error) {
	if c.busy() {
		return 0, ErrCtxBusy
	}

	f.lastSyncSeek = true

	f.mtx.Lock()
	defer f.mtx.Unlock()

	var err error
	switch c.asyncMode {
	case asyncIoUring:
		_, err = f.asyncWriteIoUring(data)
	case asyncAio:
		_, err = f.asyncWriteAio(data)
	default:
		return 0, fmt.Errorf("unknown async mode '%v'", c.asyncMode)
	}
	if err != nil {
		return 0, err
	}

	f.lastAsyncOpState.complete = false
	f.lastAsyncOpState.lastOp = OpWrite

	return 0, nil
}

func (f *File) readAsync(data []uint8) (int, error) {
	if c.busy() {
		return 0, ErrCtxBusy
	}

	f.mtx.Lock()
	defer f.mtx.Unlock()

	f.lastSyncSeek = true

	var err error
	switch c.asyncMode {
	case asyncIoUring:
		_, err = f.asyncReadIoUring(data)
	case asyncAio:
		_, err = f.asyncReadAio(data)
	default:
		return 0, fmt.Errorf("unknown async mode '%v'", c.asyncMode)
	}
	if err != nil {
		return 0, err
	}

	f.lastAsyncOpState.complete = false
	f.lastAsyncOpState.lastOp = OpRead

	return 0, nil
}

func (f *File) fillOpState() error {
	var err error
	switch c.asyncMode {
	case asyncIoUring:
		fillStatesIoUring(c)
	case asyncAio:
		err = fillStatesAio(c)
	default:
		err = fmt.Errorf("unknown async mode '%v'", c.asyncMode)
	}
	return err
}

func (f *File) close() error {
	switch c.asyncMode {
	case asyncIoUring, asyncAio:
		return f.fd.Close()
	default:
		return fmt.Errorf("unknown async mode '%v'", c.asyncMode)
	}
}
