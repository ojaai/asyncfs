//+build freebsd darwin

package asyncfs

//#include <aio.h>
//#include <stdlib.h>
//#include <strings.h>
//#include <sys/errno.h>
//#cgo LDFLAGS: -lrt
import "C"
import (
	"fmt"
	"reflect"
	"unsafe"
)

// https://github.com/freebsd/freebsd-src/blob/098dbd7ff7f3da9dda03802cdb2d8755f816eada/tests/sys/aio/aio_test.c#L260
func (f *File) asyncRWAio(op uint16, data []uint8) (int, error) {
	var cb *C.struct_aiocb
	cb = (*C.struct_aiocb)(C.malloc(C.size_t(C.sizeof_struct_aiocb)))
	C.bzero(unsafe.Pointer(cb), C.size_t(C.sizeof_struct_aiocb))
	cb.aio_buf = unsafe.Pointer(&data[0])
	cb.aio_nbytes = C.size_t(len(data))
	cb.aio_fildes = C.int(f.fd.Fd())
	cb.aio_offset = C.off_t(f.pos)

	var opRes C.int
	switch op {
	case OpRead:
		opRes = C.aio_read(cb)
	case OpWrite:
		opRes = C.aio_write(cb)
	default:
		return 0, fmt.Errorf("undefined operation '%d'", op)
	}

	if int(opRes) < 0 {
		return 0, ErrNotSubmittedAio
	}

	c.Lock()
	c.operationsFd[unsafe.Pointer(cb)] = opCap{
		f:    f,
		data: int64(cap(data)),
	}
	c.Unlock()

	return 0, nil
}

func (f *File) fillStateAio() error {
	c.Lock()
	defer c.Unlock()

	if len(c.operationsFd) == 0 {
		return nil
	}

	for k, fd := range c.operationsFd {
		errState := C.aio_error((*C.struct_aiocb)(k))
		if (int)(errState) == -1 {
			return ErrAioError
		}
		if errState == C.EINPROGRESS {
			continue
		}

		cb := (*C.struct_aiocb)(k)

		aioResult := (int)(C.aio_return(cb))
		if aioResult < 0 {
			return ErrAioError
		}

		fd.f.mtx.Lock()
		if fd.f.lastAsyncOpState.lastOp == OpRead {
			var b []uint8
			sh := (*reflect.SliceHeader)(unsafe.Pointer(&b))
			sh.Data = uintptr(cb.aio_buf)
			sh.Len = int(cb.aio_nbytes)
			sh.Cap = int(fd.data)
			fd.f.lastAsyncOpState.data = b
			fd.f.lastAsyncOpState.eof = cb.aio_nbytes > 0 && aioResult == 0
		}
		fd.f.lastAsyncOpState.result = int64(aioResult)
		fd.f.pos = int64(cb.aio_offset) + int64(aioResult)
		fd.f.lastAsyncOpState.complete = true
		fd.f.mtx.Unlock()

		C.free(k)
		delete(c.operationsFd, k)
	}

	return nil
}
