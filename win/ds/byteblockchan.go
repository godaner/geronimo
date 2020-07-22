package ds

import (
	"github.com/godaner/geronimo/rule"
	"sync"
	"sync/atomic"
)

const (
	defSize = 2 * rule.DefRecWinSize * rule.MSS
)

type ByteBlockChan struct {
	len  int64
	c    chan byte
	Size uint64
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

// Len
func (b *ByteBlockChan) Len() uint32 {
	b.init()
	return uint32(b.len)
}

// Pop
//  not block
func (b *ByteBlockChan) Pop() (usable bool, byt byte, len uint32) {
	b.init()
	select {
	case byt = <-b.c:
		atomic.AddInt64(&b.len, -1)
		return true, byt, uint32(b.len)
	default:
		return
	}
}

// BlockPop
func (b *ByteBlockChan) BlockPop() (byt byte, len uint32) {
	b.init()
	byt = <-b.c
	atomic.AddInt64(&b.len, -1)
	return byt, uint32(b.len)
}

// BlockPush
func (b *ByteBlockChan) BlockPush(byt byte) (len uint32) {
	b.init()
	b.c <- byt
	atomic.AddInt64(&b.len, 1)
	return uint32(b.len)
}

// BlockPushs
func (b *ByteBlockChan) BlockPushs(bs ...byte) (l uint32) {
	b.init()
	for _, by := range bs {
		b.c <- by
	}
	atomic.AddInt64(&b.len, int64(len(bs)))
	return uint32(b.len)
}
