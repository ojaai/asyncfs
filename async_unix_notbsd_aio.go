// +build linux android

package asyncfs

import (
	"errors"
	"reflect"
	"syscall"
	"unsafe"
)

const (
	iocbCmdPread  = uint16(0)
	iocbCmdPwrite = uint16(1)
	iocbCmdFsync  = uint16(2)
	iocbCmdFdsync = uint16(3)
)

var bigEndian = (*(*[2]uint8)(unsafe.Pointer(&[]uint16{1}[0])))[0] == 0
var (
	ErrUnalignedData = errors.New("data is unaligned")
)

type (
	// https://github.com/torvalds/linux/blob/fcadab740480e0e0e9fa9bd272acd409884d431a/include/uapi/linux/aio_abi.h#L73
	// https://android.googlesource.com/kernel/mediatek/+/android-mediatek-sprout-3.4-kitkat-mr2/include/linux/aio_abi.h
	aiocb struct {
		aioData uint64 // data to be returned in event's data

		// key consists  of two fields: the key and the flags, both are 32bits ; order is depends of endianness
		// https://github.com/torvalds/linux/blob/fcadab740480e0e0e9fa9bd272acd409884d431a/include/uapi/linux/aio_abi.h#L77
		// we MUST serialize it before syscall
		aioKey uint64 // the kernel sets aio_key to the req

		aioOpcode    uint16 // Operation to be performed
		aioReqPrio   int16  // Request priority
		aioFields    uint32 // File descriptor
		aioBuf       uint64 // Location of buffer
		aioNbytes    uint64 // Length of transfer
		aioOffset    int64  // File offset
		aioReserved2 uint64 // reserved
		aioFlags     int32  // flags for the "struct iocb"
		aioResfd     uint32 // if the IOCB_FLAG_RESFD flag of "aio_flags" is set, this is an eventfd to signal AIO readiness to
	}

	aioEvent struct {
		data uint64 // the data field from the iocb
		obj  uint64 // what iocb this event came from
		res  int64  // result code for this event
		res2 int64  // secondary result
	}
)

func (f *File) asyncRWAio(op uint16, data []uint8) (int, error) {
	sz := len(data)
	if sz == 0 {
		return 0, nil
	}

	if len(data)%c.align != 0 || uint64(uintptr(unsafe.Pointer(&data[0])))%uint64(c.align) != 0 {
		return 0, ErrUnalignedData
	}

	cb := &aiocb{
		aioFields: uint32(f.fd.Fd()),
		aioOpcode: op,
		aioBuf:    uint64(uintptr(unsafe.Pointer(&data[0]))),
		aioNbytes: uint64(sz),
		aioOffset: f.pos,
		aioData:   uint64(cap(data)),
	}
	c.Lock()
	c.operationsFd[unsafe.Pointer(cb)] = f
	c.Unlock()
	n, _, e1 := syscall.RawSyscall(syscall.SYS_IO_SUBMIT, uintptr(c.aio), 1, uintptr(unsafe.Pointer(&cb)))
	if e1 != 0 {
		return 0, e1
	}
	if n != 1 {
		return 0, ErrNotSubmittedAio
	}
	return 0, nil
}

func (f *File) asyncWriteAio(data []uint8) (int, error) {
	return f.asyncRWAio(iocbCmdPwrite, data)
}

func (f *File) asyncReadAio(data []uint8) (int, error) {
	return f.asyncRWAio(iocbCmdPread, data)
}

func fillStatesAio(c *ctx) error {
	var ts = unsafe.Pointer(&syscall.Timespec{
		Sec:  0,
		Nsec: 0,
	})
	for {
		sz := uintptr(c.sz)
		if c.sz > 64 {
			sz = uintptr(64)
		}
		e := make([]aioEvent, sz)
		n, _, e1 := syscall.Syscall6(syscall.SYS_IO_GETEVENTS, uintptr(c.aio), 1, sz, uintptr(unsafe.Pointer(&e[0])), uintptr(ts), 0)
		if e1 != 0 {
			return e1
		}
		if n > 0 {
			c.Lock()
			for i := 0; i < int(n); i++ {
				objId := e[i].obj
				aioCbPtr := unsafe.Pointer(uintptr(objId))
				fd, ok := c.operationsFd[aioCbPtr]
				if !ok {
					continue
				}
				aioCb := (*aiocb)(aioCbPtr)

				fd.mtx.Lock()
				if fd.lastAsyncOpState.lastOp == OpRead {
					var b []uint8
					sh := (*reflect.SliceHeader)(unsafe.Pointer(&b))
					sh.Data = uintptr(aioCb.aioBuf)
					sh.Len = int(aioCb.aioNbytes)
					sh.Cap = int(e[i].data)
					fd.lastAsyncOpState.data = b
					fd.lastAsyncOpState.eof = aioCb.aioNbytes > 0 && e[i].res == 0
				}
				fd.lastAsyncOpState.result = e[i].res
				fd.pos = aioCb.aioOffset + e[i].res
				fd.lastAsyncOpState.complete = true
				fd.mtx.Unlock()

				delete(c.operationsFd, aioCbPtr)
			}
			c.Unlock()
			if n >= sz {
				continue
			}
		}
		break
	}
	return nil
}
