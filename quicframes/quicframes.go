// Package quicframes 解析 QUIC 帧并处理连接。
package quicframes

import "errors"

// Counter 记录比较次数。
type Counter struct {
	Scanned int
}

// Frame 是一个帧。
type Frame struct {
	Type     uint64
	StreamID uint64
	Offset   uint64
	Length   uint64
	Fin      bool
	Data     []byte
	AckTotal uint64
}

func readVarint(data []byte, pos int, c *Counter) (uint64, int, bool) {
	if pos >= len(data) {
		return 0, pos, false
	}
	c.Scanned++
	n := 1 << (data[pos] >> 6)
	if pos+n > len(data) {
		return 0, pos, false
	}
	v := uint64(data[pos] & 0x3f)
	for i := 1; i < n; i++ {
		v = v<<8 | uint64(data[pos+i])
	}
	return v, pos + n, true
}

func isStream(t uint64) bool {
	return t >= 0x08 && t <= 0x0f
}

// Parse 解析一串帧。
func Parse(data []byte, c *Counter) ([]Frame, error) {
	var frames []Frame
	pos := 0
	for pos < len(data) {
		ftype, next, ok := readVarint(data, pos, c)
		if !ok {
			break
		}
		pos = next
		switch {
		case ftype == 0x02 || ftype == 0x03:
			_, p1, ok1 := readVarint(data, pos, c)
			_, p2, ok2 := readVarint(data, p1, c)
			rangeCount, p3, ok3 := readVarint(data, p2, c)
			first, p4, ok4 := readVarint(data, p3, c)
			if !ok1 || !ok2 || !ok3 || !ok4 {
				return frames, nil
			}
			total := first + 1
			pos = p4
			for i := uint64(0); i < rangeCount; i++ {
				_, g1, ok1 := readVarint(data, pos, c)
				length, g2, ok2 := readVarint(data, g1, c)
				if !ok1 || !ok2 {
					return frames, nil
				}
				total += length
				pos = g2
			}
			frames = append(frames, Frame{Type: ftype, AckTotal: total})
		case isStream(ftype):
			frame := Frame{Type: ftype, Fin: ftype&0x01 != 0}
			id, p1, ok := readVarint(data, pos, c)
			if !ok {
				return frames, nil
			}
			frame.StreamID = id
			pos = p1
			if ftype&0x04 != 0 {
				offset, p2, ok := readVarint(data, pos, c)
				if !ok {
					return frames, nil
				}
				frame.Offset = offset
				pos = p2
			}
			if ftype&0x02 != 0 {
				length, p3, ok := readVarint(data, pos, c)
				if !ok {
					return frames, nil
				}
				frame.Length = length
				pos = p3
			} else {
				frame.Length = uint64(len(data) - pos)
			}
			if pos+int(frame.Length) > len(data) {
				frame.Length = uint64(len(data) - pos)
			}
			frame.Data = append([]byte(nil), data[pos:pos+int(frame.Length)]...)
			pos += int(frame.Length)
			frames = append(frames, frame)
		}
	}
	return frames, nil
}

type streamState struct {
	id   uint64
	data []byte
}

// Conn 是一条连接的接收侧。
type Conn struct {
	MaxData  uint64
	received uint64
	streams  map[uint64]*streamState
}

// NewConn 建一个连接接收侧。
func NewConn(maxData uint64) *Conn {
	return &Conn{MaxData: maxData, streams: make(map[uint64]*streamState)}
}

// Received 返回已收的新字节数。
func (c *Conn) Received() uint64 {
	return c.received
}

// Handle 处理一个帧。
func (c *Conn) Handle(frame Frame, counter *Counter) error {
	if !isStream(frame.Type) {
		return nil
	}
	counter.Scanned++
	state, ok := c.streams[frame.StreamID]
	if !ok {
		state = &streamState{id: frame.StreamID}
		c.streams[frame.StreamID] = state
	}
	end := frame.Offset + uint64(len(frame.Data))
	var newBytes uint64
	if end > uint64(len(state.data)) {
		newBytes = end - uint64(len(state.data))
	}
	if c.received+newBytes > c.MaxData {
		return errors.New("flow-control")
	}
	c.received += newBytes
	if end > uint64(len(state.data)) {
		grown := make([]byte, end)
		copy(grown, state.data)
		state.data = grown
	}
	copy(state.data[frame.Offset:], frame.Data)
	return nil
}

// Stream 取某个流的数据。
func (c *Conn) Stream(id uint64, counter *Counter) []byte {
	counter.Scanned++
	if state, ok := c.streams[id]; ok {
		return state.data
	}
	return nil
}
