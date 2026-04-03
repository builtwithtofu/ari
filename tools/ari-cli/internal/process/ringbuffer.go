package process

import (
	"strings"
	"sync"
)

type RingBuffer struct {
	mu       sync.RWMutex
	buf      []byte
	capacity int
	start    int
	length   int
}

func NewRingBuffer(capacity int) *RingBuffer {
	if capacity <= 0 {
		panic("ring buffer capacity must be greater than zero")
	}

	return &RingBuffer{
		buf:      make([]byte, capacity),
		capacity: capacity,
	}
}

func (r *RingBuffer) Write(p []byte) (int, error) {
	if r == nil {
		panic("ring buffer is nil")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for _, b := range p {
		if r.length < r.capacity {
			index := (r.start + r.length) % r.capacity
			r.buf[index] = b
			r.length++
			continue
		}

		r.buf[r.start] = b
		r.start = (r.start + 1) % r.capacity
	}

	return len(p), nil
}

func (r *RingBuffer) Snapshot() []byte {
	if r == nil {
		panic("ring buffer is nil")
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.length == 0 {
		return []byte{}
	}

	out := make([]byte, r.length)
	if r.start+r.length <= r.capacity {
		copy(out, r.buf[r.start:r.start+r.length])
		return out
	}

	firstLen := r.capacity - r.start
	copy(out, r.buf[r.start:])
	copy(out[firstLen:], r.buf[:r.length-firstLen])

	return out
}

func (r *RingBuffer) Lines() []string {
	if r == nil {
		panic("ring buffer is nil")
	}

	text := string(r.Snapshot())
	if text == "" {
		return []string{}
	}

	trimmed := strings.TrimSuffix(text, "\n")
	if trimmed == "" {
		return []string{}
	}

	return strings.Split(trimmed, "\n")
}
