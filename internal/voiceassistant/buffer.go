package voiceassistant

import "sync"

// RingBuffer is a thread-safe circular buffer for accumulating raw PCM audio.
// When the buffer exceeds MaxBytes, the oldest data is discarded.
type RingBuffer struct {
	mu      sync.Mutex
	data    []byte
	maxSize int
}

// NewRingBuffer creates a ring buffer with the given capacity in bytes.
// A reasonable default is 30 seconds of 96kHz stereo s16le = 30 * 96000 * 2 * 2 = 11,520,000 bytes.
func NewRingBuffer(maxBytes int) *RingBuffer {
	return &RingBuffer{
		data:    make([]byte, 0, maxBytes/2), // start at half capacity
		maxSize: maxBytes,
	}
}

// Write appends raw PCM bytes to the buffer.
// If the buffer exceeds maxSize, oldest bytes are dropped.
func (b *RingBuffer) Write(pcm []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.data = append(b.data, pcm...)

	// Trim oldest if over capacity
	if len(b.data) > b.maxSize {
		excess := len(b.data) - b.maxSize
		b.data = b.data[excess:]
	}
}

// GetAndCopy returns a copy of all buffered data and clears the buffer.
func (b *RingBuffer) GetAndCopy() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()

	if len(b.data) == 0 {
		return nil
	}

	out := make([]byte, len(b.data))
	copy(out, b.data)
	b.data = b.data[:0]
	return out
}

// Clear empties the buffer without returning data.
func (b *RingBuffer) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data = b.data[:0]
}

// Len returns the current number of bytes in the buffer.
func (b *RingBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.data)
}
