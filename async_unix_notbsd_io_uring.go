// +build linux android

package asyncfs

// #include <signal.h>
// #include <syscall.h>
//
// int nsig() {
//     return _NSIG;
// }
//
// int syscall_nums(int *io_uring_setup, int *io_uring_enter) {
// #if defined(__NR_io_uring_setup) && defined(__NR_io_uring_enter)
//     *io_uring_setup = __NR_io_uring_setup;
//     *io_uring_enter = __NR_io_uring_enter;
// #else
//	   *io_uring_setup = -1;
//	   *io_uring_enter = -1;
// #endif
// }
import "C"

import (
	"errors"
	"reflect"
	"syscall"
	"unsafe"
)

const (
	ioringOpNop    = 0
	ioringOpReadv  = 1
	ioringOpWritev = 2
)

const (
	ioringFeatSingleMmap = uint32(0x1)
)

const (
	ioringOffSqRing = uint64(0x0)
	ioringOffCqRing = uint64(0x8000000)
	ioringOffSqes   = uint64(0x10000000)
)

var (
	nSig            int
	ioUringSetupSys int
	ioUringEnterSys int
)

var errFailedSq = errors.New("bad sync internal state with kernel ring state on the SQ side")
var errBadSize = errors.New("bad size of the queue")

func init() {
	nSig = (int)(C.nsig())
	var ioSetup, ioEnter C.int
	C.syscall_nums(&ioSetup, &ioEnter)
	ioUringSetupSys, ioUringEnterSys = (int)(ioSetup), (int)(ioEnter)
}

type (
	sqe struct {
		opcode   uint8  /* type of operation for this sqe */
		flags    uint8  /* IOSQE_ flags */
		ioprio   uint16 /* ioprio for the request */
		fd       int32  /* file descriptor to do IO on */
		off      uint64 /* offset into file */
		addr     uint64 /* pointer to buffer or iovecs */
		len      uint32 /* buffer size or number of iovecs */
		sqeFlags uint32
		userData uint64 /* data to be passed back at completion time */
		pad      [3]uint64
	}

	sQueue struct {
		khead        *uint32
		ktail        *uint32
		kringMask    *uint32
		kringEntries *uint32
		kflags       *uint32
		kdropped     *uint32
		array        []uint32
		sqes         []sqe
		sqeHead      uint32
		sqeTail      uint32
		ringSz       uint32
		sqRingFd     unsafe.Pointer
		pad          [4]uint32
	}

	// IO completion data structure (Completion Queue Entry)
	cqe struct {
		userData uint64
		res      int32
		flags    uint32
	}

	cQueue struct {
		khead        *uint32
		ktail        *uint32
		kringMask    *uint32
		kringEntries *uint32
		kflags       *uint32
		koverflow    *uint32
		cqes         []cqe
		ringSz       uint32
		cqRingFd     unsafe.Pointer
		pad          [4]uint32
	}

	// offsets for mmap
	ioSqOffsets struct {
		head        uint32
		tail        uint32
		ringMask    uint32
		ringEntries uint32
		flags       uint32
		dropped     uint32
		array       uint32
		resv1       uint32
		resv2       uint64
	}

	ioCqOffsets struct {
		head        uint32
		tail        uint32
		ringMask    uint32
		ringEntries uint32
		overflow    uint32
		cqes        uint32
		resv        [2]uint64
	}

	ioParams struct {
		sqEntries    uint32
		cqEntries    uint32
		flags        uint32
		sqThreadCpu  uint32
		sqThreadIdle uint32
		features     uint32
		resv         [4]uint32
		sqOff        ioSqOffsets
		cqOff        ioCqOffsets
	}

	ring struct {
		sq       sQueue
		cq       cQueue
		flags    uint32
		ringFd   int
		features uint32
	}

	cqUserData struct {
		buf syscall.Iovec
		cap int
	}
)

func unmap(sq *sQueue, cq *cQueue) {
	_, _, _ = syscall.RawSyscall(syscall.SYS_MUNMAP, uintptr(sq.sqRingFd), uintptr(sq.ringSz), 0)
	if cq.cqRingFd != nil && cq.cqRingFd != sq.sqRingFd {
		_, _, _ = syscall.RawSyscall(syscall.SYS_MUNMAP, uintptr(cq.cqRingFd), uintptr(cq.ringSz), 0)
	}
}

func setup(entries uint32, r *ring, flags uint32) error {
	if entries != 0 && (entries&(entries-1)) != 0 {
		return errBadSize
	}

	if ioUringSetupSys <= 0 || ioUringEnterSys <= 0 {
		return ErrNotSupported
	}

	var p ioParams
	r1, _, e := syscall.RawSyscall(uintptr(ioUringSetupSys), uintptr(entries), uintptr(unsafe.Pointer(&p)), 0)
	if e != 0 {
		if e == syscall.ENOSYS {
			return ErrNotSupported
		}
		return e
	}

	r.ringFd = int(r1)
	r.sq.ringSz = p.sqOff.array + p.sqEntries*uint32(unsafe.Sizeof(uint32(0)))
	r.cq.ringSz = p.cqOff.cqes + p.cqEntries*uint32(unsafe.Sizeof(cqe{}))

	sqPtr, _, e := syscall.RawSyscall6(
		syscall.SYS_MMAP,
		0,
		uintptr(r.sq.ringSz),
		syscall.PROT_READ|syscall.PROT_WRITE,
		syscall.MAP_SHARED|syscall.MAP_POPULATE,
		uintptr(r.ringFd),
		uintptr(ioringOffSqRing))
	if e != 0 {
		return e
	}
	r.sq.sqRingFd = unsafe.Pointer(sqPtr)

	if p.features&ioringFeatSingleMmap != 0 {
		r.cq.cqRingFd = r.sq.sqRingFd
	} else {
		cqPtr, _, e := syscall.RawSyscall6(
			syscall.SYS_MMAP,
			0,
			uintptr(r.cq.ringSz),
			syscall.PROT_READ|syscall.PROT_WRITE,
			syscall.MAP_SHARED|syscall.MAP_POPULATE,
			uintptr(r.ringFd),
			uintptr(ioringOffCqRing))
		if e != 0 {
			unmap(&r.sq, &r.cq)
			return e
		}
		r.cq.cqRingFd = unsafe.Pointer(cqPtr)
	}

	sq := &r.sq
	sq.khead = (*uint32)(unsafe.Pointer(uintptr(r.sq.sqRingFd) + uintptr(p.sqOff.head)))
	sq.ktail = (*uint32)(unsafe.Pointer(uintptr(r.sq.sqRingFd) + uintptr(p.sqOff.tail)))
	sq.kringMask = (*uint32)(unsafe.Pointer(uintptr(r.sq.sqRingFd) + uintptr(p.sqOff.ringMask)))
	sq.kringEntries = (*uint32)(unsafe.Pointer(uintptr(r.sq.sqRingFd) + uintptr(p.sqOff.ringEntries)))
	sq.kflags = (*uint32)(unsafe.Pointer(uintptr(r.sq.sqRingFd) + uintptr(p.sqOff.flags)))
	sq.kdropped = (*uint32)(unsafe.Pointer(uintptr(r.sq.sqRingFd) + uintptr(p.sqOff.dropped)))
	var arr []uint32
	arrHdr := (*reflect.SliceHeader)(unsafe.Pointer(&arr))
	arrHdr.Data = uintptr(r.sq.sqRingFd) + uintptr(p.sqOff.array)
	arrHdr.Cap = int(entries)
	arrHdr.Len = int(entries)
	sq.array = arr

	sqes, _, e := syscall.RawSyscall6(
		syscall.SYS_MMAP,
		0,
		uintptr(p.sqEntries*uint32(unsafe.Sizeof(sqe{}))),
		syscall.PROT_READ|syscall.PROT_WRITE,
		syscall.MAP_SHARED|syscall.MAP_POPULATE,
		uintptr(r.ringFd),
		uintptr(ioringOffSqes))
	if e != 0 {
		unmap(&r.sq, &r.cq)
		return e
	}
	var b []sqe
	hdr := (*reflect.SliceHeader)(unsafe.Pointer(&b))
	hdr.Data = sqes
	hdr.Cap = int(p.sqEntries)
	hdr.Len = int(p.sqEntries)
	sq.sqes = b

	cq := &r.cq
	cq.khead = (*uint32)(unsafe.Pointer(uintptr(r.cq.cqRingFd) + uintptr(p.cqOff.head)))
	cq.ktail = (*uint32)(unsafe.Pointer(uintptr(r.cq.cqRingFd) + uintptr(p.cqOff.tail)))
	cq.kringMask = (*uint32)(unsafe.Pointer(uintptr(r.cq.cqRingFd) + uintptr(p.cqOff.ringMask)))
	cq.kringEntries = (*uint32)(unsafe.Pointer(uintptr(r.cq.cqRingFd) + uintptr(p.cqOff.ringEntries)))
	cq.koverflow = (*uint32)(unsafe.Pointer(uintptr(r.cq.cqRingFd) + uintptr(p.cqOff.overflow)))
	var cqeSlice []cqe
	cqHdr := (*reflect.SliceHeader)(unsafe.Pointer(&cqeSlice))
	cqHdr.Data = uintptr(r.cq.cqRingFd) + uintptr(p.cqOff.cqes)
	cqHdr.Len = int(p.cqEntries)
	cqHdr.Cap = int(p.cqEntries)
	cq.cqes = cqeSlice

	p.flags = flags
	r.features = p.features

	return nil
}

func getSqe(r *ring) *sqe {
	sq := &r.sq
	head := *sq.khead
	next := sq.sqeTail + 1
	var s *sqe
	if next-head <= *sq.kringEntries {
		s = &sq.sqes[sq.sqeTail&*sq.kringMask]
		sq.sqeTail = next
	}
	return s
}

func rw(op uint8, ioSqe *sqe, fd int, addr unsafe.Pointer, userData unsafe.Pointer, len uint32, offset int64) bool {
	if op != ioringOpReadv && op != ioringOpWritev {
		return false
	}
	if ioSqe == nil {
		return false
	}
	if addr == nil || userData == nil {
		return false
	}
	ioSqe.opcode = op
	ioSqe.flags = 0
	ioSqe.ioprio = 0
	ioSqe.fd = int32(fd)
	ioSqe.off = uint64(offset)
	ioSqe.addr = uint64(uintptr(addr))
	ioSqe.len = len
	ioSqe.sqeFlags = 0
	ioSqe.userData = uint64(uintptr(userData))
	ioSqe.pad[0] = 0
	ioSqe.pad[1] = 0
	ioSqe.pad[2] = 0
	return true
}

func (f *File) asyncRWIoUring(op uint8, data []uint8) (int, error) {
	userData, ok := c.ioUringUserDataPool.Get().(*cqUserData)
	if !ok || userData == nil {
		userData = &cqUserData{}
	}
	userData.buf = newIovec(&data[0], len(data))
	userData.cap = cap(data)

	c.Lock()
	c.currentCnt++
	if !rw(op, getSqe(c.r), int(f.fd.Fd()), unsafe.Pointer(&userData.buf), unsafe.Pointer(userData), 1, f.pos) {
		c.currentCnt--
		c.Unlock()
		return 0, ErrNotSubmittedIoUring
	}
	c.operationsFd[unsafe.Pointer(userData)] = f
	submit := flushSq(c.r)
	c.Unlock()
	if submit == 0 {
		return 0, errFailedSq
	}
	_, _, e := syscall.RawSyscall6(uintptr(ioUringEnterSys), uintptr(c.r.ringFd), uintptr(submit), 0, 0, 0, uintptr(nSig/8))
	if e != 0 {
		return 0, e
	}
	return 0, nil
}

func (f *File) asyncWriteIoUring(data []uint8) (int, error) {
	return f.asyncRWIoUring(ioringOpWritev, data)
}

func (f *File) asyncReadIoUring(data []uint8) (int, error) {
	return f.asyncRWIoUring(ioringOpReadv, data)
}

func flushSq(r *ring) int {
	sq := &r.sq
	mask := *sq.kringMask
	ktail := *sq.ktail
	toSubmit := sq.sqeTail - sq.sqeHead
	for ; toSubmit > 0; toSubmit-- {
		sq.array[ktail&mask] = sq.sqeHead & mask
		ktail++
		sq.sqeHead++
	}
	*sq.ktail = ktail
	return int(ktail - *sq.khead)
}

func fillStatesIoUring(c *ctx) {
	c.Lock()
	defer c.Unlock()

	r := c.r
	head := *r.cq.khead
	available := *r.cq.ktail - head

	for ; ; head++ {
		if *r.cq.ktail-head == 0 {
			break
		}
		cqe := r.cq.cqes[head&(*r.cq.kringMask)]
		ptrData := unsafe.Pointer(uintptr(cqe.userData))
		fd, ok := c.operationsFd[ptrData]
		if !ok {
			continue
		}
		userData := (*cqUserData)(ptrData)

		fd.mtx.Lock()
		if fd.lastAsyncOpState.lastOp == OpRead {
			var b []uint8
			hdr := (*reflect.SliceHeader)(unsafe.Pointer(&b))
			hdr.Data = uintptr(unsafe.Pointer(userData.buf.Base))
			hdr.Len = int(userData.buf.Len)
			hdr.Cap = userData.cap
			fd.lastAsyncOpState.data = b
			fd.lastAsyncOpState.eof = userData.buf.Len > 0 && cqe.res == 0
		}
		fd.lastAsyncOpState.result = int64(cqe.res)
		fd.pos += int64(cqe.res)
		fd.lastAsyncOpState.complete = true
		fd.mtx.Unlock()

		userData.cap = 0
		userData.buf.Len = 0
		userData.buf.Base = nil
		c.ioUringUserDataPool.Put(userData)

		delete(c.operationsFd, ptrData)
	}
	*r.cq.khead = *r.cq.khead + available

	c.currentCnt -= int(available)
}
