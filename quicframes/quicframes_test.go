package quicframes

import "testing"

func TestParseStreamFrame(t *testing.T) {
	frames, err := Parse(StreamFrame(0, 0, false, []byte("hi")), &Counter{})
	if err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	if len(frames) != 1 {
		t.Fatalf("应有一帧：%d", len(frames))
	}
}

func TestFinBit(t *testing.T) {
	data := append(Varint(0x0b), Varint(0)...)
	data = append(data, Varint(1)...)
	data = append(data, 'x')
	frames, _ := Parse(data, &Counter{})
	if len(frames) == 0 || !frames[0].Fin {
		t.Fatalf("FIN 位应置位：%+v", frames)
	}
}

func TestHandleStores(t *testing.T) {
	conn := NewConn(1000)
	conn.Handle(Frame{Type: 0x0a, StreamID: 0, Data: []byte("ab")}, &Counter{})
	if string(conn.Stream(0, &Counter{})) != "ab" {
		t.Fatalf("流数据不对：%q", conn.Stream(0, &Counter{}))
	}
}

func TestReceivedZero(t *testing.T) {
	if NewConn(100).Received() != 0 {
		t.Fatal("初始已收应是 0")
	}
}

func TestStreamMissing(t *testing.T) {
	conn := NewConn(100)
	if conn.Stream(9, &Counter{}) != nil {
		t.Fatal("缺失的流应返回 nil")
	}
}

func TestCounterCounts(t *testing.T) {
	c := &Counter{}
	Parse(Varint(0x01), c)
	if c.Scanned == 0 {
		t.Fatal("应统计比较次数")
	}
}
