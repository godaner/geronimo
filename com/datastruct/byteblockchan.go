package datastruct

import "sync"

const (
	defSize = 65535
)

type ByteBlockChan struct {
	len  uint32
	c    chan byte
	Size uint32
	sync.Once
}

func (b *ByteBlockChan) init() {
	b.Do(func() {
		if b.Size <= 0 {
			b.Size = defSize
		}
		b.c = make(chan byte, b.Size)
	})
}
func (b *ByteBlockChan) Len() uint32 {
	b.init()
	return b.len
}
func (b *ByteBlockChan) Pop() (byt byte, len uint32) {
	b.init()
	byt = <-b.c
	b.len--
	return byt, b.len
}

func (b *ByteBlockChan) Push(byt byte) (len uint32) {
	b.init()
	b.c <- byt
	b.len++
	return b.len
}
