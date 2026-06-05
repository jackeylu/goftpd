package main

import (
	"bytes"
	"sync"
	"time"
)

const logBufSize = 200

// LogEntry is one captured log line.
type LogEntry struct {
	Time string `json:"time"`
	Msg  string `json:"msg"`
}

// LogBuffer is a ring buffer that captures log output.
// It implements io.Writer so it can be used with log.SetOutput().
type LogBuffer struct {
	mu      sync.Mutex
	buf     []LogEntry
	cap     int
	pos     int // next write position
	count   int // total entries written (capped at cap)
	partial bytes.Buffer
}

// NewLogBuffer creates a ring buffer with the given capacity.
func NewLogBuffer(capacity int) *LogBuffer {
	return &LogBuffer{
		buf: make([]LogEntry, capacity),
		cap: capacity,
	}
}

// Write implements io.Writer. It accumulates bytes until a newline,
// then stores the complete line as a LogEntry.
func (lb *LogBuffer) Write(p []byte) (int, error) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	n := len(p)
	for {
		idx := bytes.IndexByte(p, '\n')
		if idx < 0 {
			lb.partial.Write(p)
			break
		}
		lb.partial.Write(p[:idx])
		line := lb.partial.String()
		lb.partial.Reset()
		p = p[idx+1:]

		if len(line) == 0 {
			continue
		}

		lb.buf[lb.pos] = LogEntry{
			Time: time.Now().Format("15:04:05"),
			Msg:  line,
		}
		lb.pos = (lb.pos + 1) % lb.cap
		if lb.count < lb.cap {
			lb.count++
		}
	}
	return n, nil
}

// Recent returns the last n log entries (or fewer if not enough).
func (lb *LogBuffer) Recent(n int) []LogEntry {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	if n > lb.count {
		n = lb.count
	}
	if n == 0 {
		return nil
	}

	result := make([]LogEntry, n)
	// start is the oldest entry to return
	start := (lb.pos - n + lb.cap) % lb.cap
	for i := 0; i < n; i++ {
		result[i] = lb.buf[(start+i)%lb.cap]
	}
	return result
}
