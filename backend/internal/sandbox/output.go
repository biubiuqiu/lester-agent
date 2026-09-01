package sandbox

const commandCaptureLimit = 256 << 10

type boundedCapture struct {
	limit     int
	headLimit int
	tailLimit int
	head      []byte
	tail      []byte
	total     int64
}

func newBoundedCapture(limit int) *boundedCapture {
	tail := limit / 4
	return &boundedCapture{limit: limit, headLimit: limit - tail, tailLimit: tail}
}

func (b *boundedCapture) Write(data []byte) (int, error) {
	written := len(data)
	b.total += int64(written)
	if remaining := b.headLimit - len(b.head); remaining > 0 {
		count := min(remaining, len(data))
		b.head = append(b.head, data[:count]...)
		data = data[count:]
	}
	if len(data) > 0 {
		b.tail = append(b.tail, data...)
		if len(b.tail) > b.tailLimit {
			b.tail = append([]byte(nil), b.tail[len(b.tail)-b.tailLimit:]...)
		}
	}
	return written, nil
}

func (b *boundedCapture) String() string {
	return string(append(append([]byte(nil), b.head...), b.tail...))
}
func (b *boundedCapture) Truncated() bool { return b.total > int64(len(b.head)+len(b.tail)) }
func (b *boundedCapture) OmittedBytes() int64 {
	return max(0, b.total-int64(len(b.head)+len(b.tail)))
}
