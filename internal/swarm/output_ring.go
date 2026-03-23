package swarm

// outputRing is a fixed-capacity ring buffer that stores token output for a
// SwarmAgent. When the buffer is full the oldest bytes are silently overwritten,
// bounding per-agent memory usage to outputRingCap bytes regardless of how long
// the agent runs or how much it streams.
//
// All operations must be called with the owning SwarmAgent.mu held.
const outputRingCap = 512 * 1024 // 512 KB per agent

type outputRing struct {
	data []byte
	head int // next write position (circular)
	size int // bytes currently stored (≤ len(data))
}

func newOutputRing() *outputRing {
	return newOutputRingWithSize(outputRingCap)
}

// newOutputRingWithSize creates an outputRing with a custom capacity in bytes.
// n <= 0 falls back to outputRingCap (512 KB).
func newOutputRingWithSize(n int) *outputRing {
	if n <= 0 {
		n = outputRingCap
	}
	return &outputRing{data: make([]byte, n)}
}

// write appends s to the ring, overwriting oldest bytes when full.
func (r *outputRing) write(s string) {
	cap := len(r.data)
	for i := 0; i < len(s); i++ {
		r.data[r.head] = s[i]
		r.head = (r.head + 1) % cap
		if r.size < cap {
			r.size++
		}
	}
}

// string returns the current contents in insertion order.
func (r *outputRing) string() string {
	cap := len(r.data)
	if r.size == 0 {
		return ""
	}
	if r.size < cap {
		return string(r.data[:r.size])
	}
	// Buffer is full: content starts at head (oldest byte) and wraps.
	out := make([]byte, cap)
	n := copy(out, r.data[r.head:])
	copy(out[n:], r.data[:r.head])
	return string(out)
}

// reset clears the buffer, equivalent to truncating the output on retry.
func (r *outputRing) reset() {
	r.head = 0
	r.size = 0
}
