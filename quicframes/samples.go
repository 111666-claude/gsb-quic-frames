package quicframes

import "fmt"

// Scenario 是一个固定场景。
type Scenario struct {
	Name string
	Run  func() map[string]any
}

// Varint 按最短形式编码一个变长整数。
func Varint(value uint64) []byte {
	switch {
	case value < 1<<6:
		return []byte{byte(value)}
	case value < 1<<14:
		return []byte{0x40 | byte(value>>8), byte(value)}
	default:
		return []byte{0x80 | byte(value>>24), byte(value >> 16), byte(value >> 8), byte(value)}
	}
}

// StreamFrame 拼一个 STREAM 帧。
func StreamFrame(id uint64, offset uint64, withOffset bool, data []byte) []byte {
	ftype := uint64(0x0a)
	if withOffset {
		ftype |= 0x04
	}
	out := append([]byte(nil), Varint(ftype)...)
	out = append(out, Varint(id)...)
	if withOffset {
		out = append(out, Varint(offset)...)
	}
	out = append(out, Varint(uint64(len(data)))...)
	return append(out, data...)
}

// AckFrame 拼一个 ACK 帧，ranges 是（间隔, 长度）对。
func AckFrame(largest, first uint64, ranges [][2]uint64) []byte {
	out := append([]byte(nil), Varint(0x02)...)
	out = append(out, Varint(largest)...)
	out = append(out, Varint(0)...)
	out = append(out, Varint(uint64(len(ranges)))...)
	out = append(out, Varint(first)...)
	for _, r := range ranges {
		out = append(out, Varint(r[0])...)
		out = append(out, Varint(r[1])...)
	}
	return out
}

// Samples 返回全部场景。
func Samples() []Scenario {
	return []Scenario{
		{Name: "varint", Run: func() map[string]any {
			data := append(Varint(0x0a), Varint(0)...)
			data = append(data, Varint(100)...)
			data = append(data, make([]byte, 100)...)
			frames, _ := Parse(data, &Counter{})
			if len(frames) == 0 {
				return map[string]any{"len": 0}
			}
			return map[string]any{"len": len(frames[0].Data)}
		}},
		{Name: "offbit", Run: func() map[string]any {
			data := StreamFrame(0, 8, true, []byte("ab"))
			frames, _ := Parse(data, &Counter{})
			if len(frames) == 0 {
				return map[string]any{"offset": 0}
			}
			return map[string]any{"offset": frames[0].Offset}
		}},
		{Name: "ack", Run: func() map[string]any {
			data := AckFrame(20, 4, [][2]uint64{{1, 4}})
			frames, _ := Parse(data, &Counter{})
			if len(frames) == 0 {
				return map[string]any{"acked": 0}
			}
			return map[string]any{"acked": frames[0].AckTotal}
		}},
		{Name: "offgap", Run: func() map[string]any {
			conn := NewConn(1000)
			data := StreamFrame(0, 4, true, []byte("xy"))
			frames, _ := Parse(data, &Counter{})
			conn.Handle(frames[0], &Counter{})
			return map[string]any{"len": len(conn.Stream(0, &Counter{}))}
		}},
		{Name: "flow", Run: func() map[string]any {
			conn := NewConn(100)
			payload := make([]byte, 60)
			frame := Frame{Type: 0x0a, StreamID: 0, Data: payload}
			if err := conn.Handle(frame, &Counter{}); err != nil {
				return map[string]any{"error": err.Error()}
			}
			if err := conn.Handle(frame, &Counter{}); err != nil {
				return map[string]any{"error": err.Error()}
			}
			return map[string]any{"received": conn.Received()}
		}},
	}
}

// Work 跑规模线场景。
func Work(n int) map[string]any {
	conn := NewConn(1 << 20)
	for i := 0; i < n; i++ {
		conn.Handle(Frame{Type: 0x0a, StreamID: uint64(i), Data: []byte{'x'}}, &Counter{})
	}
	c := &Counter{}
	for i := 0; i < n; i++ {
		conn.Stream(uint64(i), c)
	}
	return map[string]any{"streams": n, "scanned": c.Scanned}
}

var _ = fmt.Sprintf
