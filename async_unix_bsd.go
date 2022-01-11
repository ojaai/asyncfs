//+build freebsd darwin

package asyncfs

import (
	"os"
)

func openAsync(path string, flag int, perm os.FileMode) (*os.File, error) {
	return os.OpenFile(path, flag, perm)
}

func (f *File) writeAsync(data []uint8) (int, error) {
	f.mtx.Lock()
	f.lastSyncSeek = true
	n, err := f.asyncRWAio(OpWrite, data)
	if err == nil {
		f.lastAsyncOpState.complete = false
		f.lastAsyncOpState.lastOp = OpWrite
	}
	f.mtx.Unlock()
	return n, err
}

func (f *File) readAsync(data []uint8) (int, error) {
	f.mtx.Lock()
	f.lastSyncSeek = true
	n, err := f.asyncRWAio(OpRead, data)
	if err == nil {
		f.lastAsyncOpState.complete = false
		f.lastAsyncOpState.lastOp = OpRead
	}
	f.mtx.Unlock()
	return n, err
}

func (f *File) fillOpState() error {
	return f.fillStateAio()
}

func (f *File) close() error {
	return nil
}
