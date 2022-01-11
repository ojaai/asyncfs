# Asyncfs is a library for asynchronous file I/O
# Description
There is a problem in the Go runtime: since the goroutine scheduler works only in the context of the Go code itself, it needs to make a separate OS thread to call external functions such as cgo or syscalls. A typical example of syscalls is I/O operations with files. Unfortunately, epoll/kqueue can’t work with regular files, thus Go’s netpoller can’t help the Go's scheduler. The scheduler can’t switch a goroutine which execute the syscall and it is not clear how long it takes. Performing such operations in a separate OS thread is the only option the Go's scheduler has. If all the threads are busy, the new thread will be made. That is why if cgo is used frequently as well as with long syscalls (except for sockets, since there is a netpoller there) your Go program can create a lot of unnecessary OS threads.
Asyncfs allows you to perform I/O operations with files using asynchronous interfaces: 
- **Linux, Android** - aio/io_uring;
- **FreeBSD, MacOS** - aio;
- **Windows** - OVERLAPPED;

So syscalls are processed in a short time and unnecessary OS threads are not created. The work with the files occurs at the core OS level and the goroutines are not blocked. Also you can work using synchronous mode. In this case you have an ability to set a limit that regulates the need to switch to a separate thread for the I/O operation.

# Getting the Source
```sh
go get -u github.com/ojaai/asyncfs
```

# Restrictions
- The library is incompatible with the race detector
- Using aio on Linux requires 512-bytes alignment

# Example
```go
import(
	"fmt"
	"github.com/ojaai/asyncfs"
	"sync"
)

func main() {
	pool := sync.Pool{
		New: func() interface{} {
			b := make([]uint8, 0, 1024*128)
			return b
		},
	}

	allocator := func(sz int) []uint8 {
		buf, ok := pool.Get().([]uint8)
		if !ok || cap(buf) != sz {
			return make([]uint8, 0, sz)
		}
		return buf
	}

	releaser := func(buf []uint8) {
		for i := range buf {
			buf[i] = 0x0
		}
		buf = buf[:0]
		pool.Put(buf)
	}
	
	// main ctx initialization
	// 1024 - asynchronous operations queue size;
	// 1024*4 - the byte limit that determines whether to switch to a new OS thread for synchronous I/O operation. 
	// If the buffer size for I/O does not exceed this limit, then the Go's scheduler will not switch to a new OS thread;
	// allocator, releaser - memory operations for a buffer that can be used in an I/O operation;
	// allocator && releaser can be nil, if you don't use special allocators
	if err := asyncfs.NewCtx(1024, 1024*4, allocator, releaser); err != nil {
		panic(err.Error())
	}
	// the last argument is the mode of working with the file: synchronous or asynchronous
	f, err := asyncfs.Create("/tmp/test_asyncfs", asyncfs.ModeAsync)
	if err != nil {
		panic(err.Error())
	}

	// aio on Linux require I/O to be 512-byte aligned
	// AllocBuf make and align byte array
	buf := asyncfs.AllocBuf(4096)
	defer asyncfs.ReleaseBuf(buf)

	// fill 'buf' with your payload...
	// async mode: first 'n' will always be 0
	_, err = f.Write(buf)
	if err != nil {
		panic(err.Error())
	}
	// do something ...
	for {
		n, ok, err := f.LastOp()
		if !ok {
			// do something ...
			continue
		}
		if err != nil {
			panic(err.Error)
		}
		fmt.Printf("%d bytes has been written\n", n)
		break
	}
  
	// ok, let's try to read
	f.Seek(0, 0)
	out := asyncfs.AllocBuf(4096)
	defer asyncfs.ReleaseBuf(out)
	_, err = f.Read(out)
	if err != nil {
		panic(err.Error())
	}
	// do something ...
	for {
		n, ok, err := f.LastOp()
		if !ok {
			// do something ...
			continue
		}
		if err != nil {
			panic(err.Error)
		}
		fmt.Printf("%d bytes has been read\n", n)
		break
	}
	// out has been filled
}
```
