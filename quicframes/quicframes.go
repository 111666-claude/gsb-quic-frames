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
	return uint64(data[pos] & 0x3f), pos + 1, true
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
			_, p1, _ := readVarint(data, pos, c)
			_, p2, _ := readVarint(data, p1, c)
			_, p3, _ := readVarint(data, p2, c)
			first, p4, _ := readVarint(data, p3, c)
			frames = append(frames, Frame{Type: ftype, AckTotal: first + 1})
			pos = p4
		case isStream(ftype):
			frame := Frame{Type: ftype, Fin: ftype&0x01 != 0}
			id, p1, _ := readVarint(data, pos, c)
			frame.StreamID = id
			frame.Length, pos = readLength(data, p1, c)
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

func readLength(data []byte, pos int, c *Counter) (uint64, int) {
	value, next, ok := readVarint(data, pos, c)
	if !ok {
		return 0, pos
	}
	return value, next
}

type streamState struct {
	id   uint64
	data []byte
}

// Conn 是一条连接的接收侧。
type Conn struct {
	MaxData  uint64
	received uint64
	streams  []streamState
}

// NewConn 建一个连接接收侧。
func NewConn(maxData uint64) *Conn {
	return &Conn{MaxData: maxData}
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
	c.received += uint64(len(frame.Data))
	if c.received > c.MaxData {
		return errors.New("flow-control")
	}
	for i := range c.streams {
		counter.Scanned++
		if c.streams[i].id == frame.StreamID {
			c.streams[i].data = append(c.streams[i].data, frame.Data...)
			return nil
		}
	}
	c.streams = append(c.streams, streamState{id: frame.StreamID, data: append([]byte(nil), frame.Data...)})
	return nil
}

// Stream 取某个流的数据。
func (c *Conn) Stream(id uint64, counter *Counter) []byte {
	for i := range c.streams {
		counter.Scanned++
		if c.streams[i].id == id {
			return c.streams[i].data
		}
	}
	return nil
}
