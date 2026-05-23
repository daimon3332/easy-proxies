package monitor

import (
	"io"
	"sync"
)

// LogBuffer is a thread-safe ring buffer that captures recent log output.
// It implements io.Writer so it can be plugged into log.SetOutput via io.MultiWriter.
type LogBuffer struct {
	mu   sync.Mutex
	buf  []byte
	size int
}

// NewLogBuffer creates a ring buffer that keeps the last `size` bytes of log output.
func NewLogBuffer(size int) *LogBuffer {
	return &LogBuffer{
		buf:  make([]byte, 0, size),
		size: size,
	}
}

// Write implements io.Writer. Appends data and trims the front if over capacity.
func (lb *LogBuffer) Write(p []byte) (n int, err error) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	lb.buf = append(lb.buf, p...)
	if len(lb.buf) > lb.size {
		lb.buf = lb.buf[len(lb.buf)-lb.size:]
	}
	return len(p), nil
}

// Content returns a copy of the current buffer contents.
func (lb *LogBuffer) Content() string {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	return string(lb.buf)
}

// Clear removes all buffered log content.
func (lb *LogBuffer) Clear() {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	lb.buf = lb.buf[:0]
}

// SharedLogBuffer is the global log buffer accessible by the server.
var SharedLogBuffer *LogBuffer

func init() {
	SharedLogBuffer = NewLogBuffer(64 * 1024) // 64KB ring buffer
}

// LogWriter returns an io.Writer that can be used with io.MultiWriter to capture logs.
func LogWriter() io.Writer {
	return SharedLogBuffer
}
