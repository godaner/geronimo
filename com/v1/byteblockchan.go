package v1

import "sync"

const (
	defSize = 65535
)

type byteBlockChan struct {
	len  uint32
	c    chan byte
	Size uint32
	sync.Once
}

func (b *byteBlockChan) init() {
	b.Do(func() {
		if b.Size <= 0 {
			b.Size = defSize
		}
		b.c = make(chan byte, b.Size)
	})
}
func (b *byteBlockChan) Len() uint32 {
	b.init()
	return b.len
}
func (b *byteBlockChan) Pop() (byt byte, len uint32) {
	b.init()
	byt = <-b.c
	b.len--
	return byt, b.len
}

func (b *byteBlockChan) Push(byt byte) (len uint32) {
	b.init()
	b.c <- byt
	b.len++
	return b.len
}
