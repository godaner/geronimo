package ds

import (
	"bytes"
	"errors"
	"github.com/godaner/geronimo/rule"
	"sync"
	"time"
)

const (
	defSize = 10
)

var (
	ErrStoped = errors.New("stoped")
)

type BQ struct {
	sync.Once
	sync.RWMutex
	bs         []byte
	buf        bytes.Buffer
	readBlock  chan bool
	writeBlock chan bool
	Size       uint32
}

func (b *BQ) init() {
	b.Do(func() {
		b.bs = make([]byte, 0)
		b.readBlock = make(chan bool, 0)
		b.writeBlock = make(chan bool, 0)
		close(b.writeBlock)
		if b.Size <= 0 {
			b.Size = defSize * rule.MSS
		} else {
			b.Size *= rule.MSS
		}
	})
}

// Len
func (b *BQ) Len() uint32 {
	b.init()
	b.RLock()
	defer b.RUnlock()
	return uint32(len(b.bs))
}

// Pop
func (b *BQ) Pop(byt []byte) (nn uint32) {
	b.init()
	return b.pop(byt)
}

// BlockPop
func (b *BQ) BlockPop(byt []byte) (nn uint32) {
	b.init()
	select {
	case <-b.readBlock:
		return b.pop(byt)
	case <-time.After(time.Duration(1000) * time.Millisecond):
		panic("to")
	}
}

// BlockPopWithStop
func (b *BQ) BlockPopWithStop(byt []byte, stop chan bool) (nn uint32, err error) {
	b.init()
	select {
	case <-b.readBlock:
		return b.pop(byt), nil
	case <-stop:
		return 0, ErrStoped
	}

}

//   Push
func (b *BQ) Push(byt ...byte) {
	b.init()
	for {
		bl := uint32(len(byt))
		if bl <= 0 {
			return
		}
		byt = b.push(byt...)
	}
}

// push
func (b *BQ) push(byt ...byte) (rest []byte) {
	select {
	case <-b.writeBlock:
		b.Lock()
		l := uint32(len(b.bs))
		if l >= b.Size {
			b.Unlock()
			return byt
		}
		s := int(b.Size) - len(b.bs)
		bytl := len(byt)
		if bytl < s {
			s = bytl
		}
		b.bs = append(b.bs, byt[:s]...)
		nl := uint32(len(b.bs))
		if nl > 0 {
			select {
			case <-b.readBlock:
			default:
				close(b.readBlock)
			}
		}
		if nl >= b.Size { // block write
			select {
			case <-b.writeBlock:
			default:
				close(b.writeBlock)
			}
			b.writeBlock = make(chan bool)
		}
		b.Unlock()
		return byt[s:]
	}
}

// pop
func (b *BQ) pop(byt []byte) (nn uint32) {
	b.Lock()
	defer b.Unlock()
	l := uint32(len(b.bs))
	if l <= 0 {
		return
	}
	n := uint32(len(byt))
	if n <= 0 || l < n {
		n = l
	}
	copy(byt, b.bs[:n])
	b.bs = b.bs[n:]
	nl := uint32(len(b.bs))
	if nl < b.Size {
		select {
		case <-b.writeBlock:
		default:
			close(b.writeBlock)
		}
	}
	if nl <= 0 { // block read
		select {
		case <-b.readBlock:
		default:
			close(b.readBlock)
		}
		b.readBlock = make(chan bool)
	}
	return n
}
