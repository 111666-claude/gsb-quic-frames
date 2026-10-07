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

// readVarint 按首字节高两位 00/01/10/11 读 1/2/4/8 字节的变长整数。
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
			// 最大确认号、延迟、区间个数、第一个区间长度。
			largest, p1, ok := readVarint(data, pos, c)
			if !ok {
				return frames, nil
			}
			if _, p, ok := readVarint(data, p1, c); ok {
				p1 = p
			} else {
				return frames, nil
			}
			ranges, p2, ok := readVarint(data, p1, c)
			if !ok {
				return frames, nil
			}
			first, p3, ok := readVarint(data, p2, c)
			if !ok {
				return frames, nil
			}
			// 第一个区间覆盖 [largest-first, largest]。
			total := first + 1
			smallest := largest - first
			pos = p3
			// 随后每段是"间隔、区间长度"，区间长度即覆盖的报文数。
			for i := uint64(0); i < ranges; i++ {
				gap, p4, ok := readVarint(data, pos, c)
				if !ok {
					return frames, nil
				}
				length, p5, ok := readVarint(data, p4, c)
				if !ok {
					return frames, nil
				}
				total += length
				if smallest >= gap+1+length {
					smallest -= gap + 1 + length
				} else {
					smallest = 0
				}
				pos = p5
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

// segment 是流上一段已收到的连续区间 [start, end)。
type segment struct {
	start uint64
	end   uint64
}

type streamState struct {
	id   uint64
	data []byte
	segs []segment
}

// missing 返回 [start, end) 中尚未收到的字节数。
func (s *streamState) missing(start, end uint64) uint64 {
	total := end - start
	for _, seg := range s.segs {
		if seg.end <= start {
			continue
		}
		if seg.start >= end {
			break
		}
		lo := seg.start
		if lo < start {
			lo = start
		}
		hi := seg.end
		if hi > end {
			hi = end
		}
		total -= hi - lo
	}
	return total
}

// write 按偏移写入数据并合并区间；重叠重传不改变结果。
func (s *streamState) write(offset uint64, data []byte) {
	if len(data) == 0 {
		return
	}
	end := offset + uint64(len(data))
	if end > uint64(len(s.data)) {
		grown := make([]byte, end)
		copy(grown, s.data)
		s.data = grown
	}
	copy(s.data[offset:], data)
	s.segs = mergeSegment(s.segs, offset, end)
}

// mergeSegment 把 [start, end) 并入有序不重叠的区间列表。
func mergeSegment(segs []segment, start, end uint64) []segment {
	out := make([]segment, 0, len(segs)+1)
	i := 0
	for i < len(segs) && segs[i].end < start {
		out = append(out, segs[i])
		i++
	}
	for i < len(segs) && segs[i].start <= end {
		if segs[i].start < start {
			start = segs[i].start
		}
		if segs[i].end > end {
			end = segs[i].end
		}
		i++
	}
	out = append(out, segment{start: start, end: end})
	return append(out, segs[i:]...)
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
	s, ok := c.streams[frame.StreamID]
	if !ok {
		s = &streamState{id: frame.StreamID}
		c.streams[frame.StreamID] = s
	}
	start := frame.Offset
	end := start + uint64(len(frame.Data))
	newBytes := s.missing(start, end)
	if c.received+newBytes > c.MaxData {
		return errors.New("flow-control")
	}
	c.received += newBytes
	s.write(start, frame.Data)
	return nil
}

// Stream 取某个流的数据。
func (c *Conn) Stream(id uint64, counter *Counter) []byte {
	counter.Scanned++
	if s, ok := c.streams[id]; ok {
		return s.data
	}
	return nil
}
