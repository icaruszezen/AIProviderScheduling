package helps

import (
	"bufio"
	"sync"
)

const (
	// SSEScannerBufSize is the pooled initial buffer for upstream SSE scanners.
	SSEScannerBufSize = 64 * 1024
	// SSEScannerMaxTokenSize is the maximum SSE line a scanner may grow to.
	SSEScannerMaxTokenSize = 52_428_800
)

type sseScannerBuf [SSEScannerBufSize]byte

var sseScannerBufPool = sync.Pool{
	New: func() any {
		return new(sseScannerBuf)
	},
}

func getSSEScannerBuf() *sseScannerBuf {
	v := sseScannerBufPool.Get()
	buf, ok := v.(*sseScannerBuf)
	if !ok || buf == nil {
		return new(sseScannerBuf)
	}
	return buf
}

func putSSEScannerBuf(buf *sseScannerBuf) {
	if buf == nil {
		return
	}
	sseScannerBufPool.Put(buf)
}

// BorrowSSEScannerBuffer installs a pooled 64KiB initial buffer on scanner.
// maxTokenSize remains the maximum line size; the pool only avoids allocating
// the initial buffer on every stream.
//
// scanner.Bytes and scanner.Text alias that buffer. Those slices are invalid
// after the next Scan and after release. Callers must copy any token they
// retain, including payloads sent to another goroutine.
//
// release must run after scanning finishes, including when a background
// goroutine continues the same scanner. release is safe to call more than once.
//
// When maxTokenSize is smaller than the pooled buffer, the pool is not used.
// bufio.Scanner treats cap(buf) as a floor on the token limit, so a 64KiB
// buffer would accept lines longer than a smaller max.
func BorrowSSEScannerBuffer(scanner *bufio.Scanner, maxTokenSize int) (release func()) {
	if scanner == nil {
		return func() {}
	}
	if maxTokenSize <= 0 {
		maxTokenSize = SSEScannerMaxTokenSize
	}
	if maxTokenSize < SSEScannerBufSize {
		scanner.Buffer(nil, maxTokenSize)
		return func() {}
	}
	buf := getSSEScannerBuf()
	scanner.Buffer(buf[:0], maxTokenSize)
	var once sync.Once
	return func() {
		once.Do(func() { putSSEScannerBuf(buf) })
	}
}
