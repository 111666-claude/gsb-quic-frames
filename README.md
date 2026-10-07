# quic-frames

解析 QUIC 帧并处理连接：变长整数四种长度、STREAM 的可选偏移与长度、ACK 确认区间、连接级流量控制与按流的乱序重组。只用标准库。

```
go test ./...
go vet ./...
go run ./cmd/quicframes --sample=varint
go run ./cmd/quicframes --sample=offbit
go run ./cmd/quicframes --sample=ack
go run ./cmd/quicframes --sample=offgap
go run ./cmd/quicframes --sample=flow
go run ./cmd/quicframes --sample=work
```

## 口径

- **变长整数**：首字节高两位 `00/01/10/11` 对应 1/2/4/8 字节，值由低 6 位与后续字节拼成。
- **STREAM**：类型低四位为 `1000`，`0x04` 位表示带偏移、`0x02` 位表示带长度、`0x01` 位表示 FIN；不带长度时数据延伸到帧末。
- **ACK**：确认号最大的那个、延迟、区间个数、第一个区间长度，随后每段是"间隔、区间长度"；确认总数是所有区间覆盖的不同报文数。
- **缓冲**：帧可以乱序到达，流数据按偏移写入；重叠重传不重复计入已收字节。
- **流量控制**：连接累计已收的新字节不超过上限，超出报 `flow-control`。
- **查找**：按流号取流数据走索引。

## 不变量

- 变长整数的长度与首字节高两位一致。
- 流的字节数等于写入区间覆盖的最大范围；重叠写入不改变结果。
- 已收字节只统计新字节，重传不计。
- `scanned` 不随流数乘查询次数放大：八百条流查八百次的 `scanned` 不超过 6000。

## 输出契约

`Frame` 含类型、流号、偏移、长度、FIN、数据与 ACK 确认总数；
`Conn` 提供 `Handle`、`Received` 与 `Stream`。场景打印一行 JSON。
`--sample=work` 打印 `{"streams": N, "scanned": N}`。
