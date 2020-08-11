package win

import (
	"bytes"
	"errors"
	"sync"
	"time"
)

const (
	defSize = 10 * mss
)

var (
	ErrStoped = errors.New("stoped")
)

type bq struct {
	sync.Once
	sync.RWMutex
	bs         []byte
	buf        bytes.Buffer
	readBlock  chan bool
	writeBlock chan bool
	Size       uint32
}

func (b *bq) init() {
	b.Do(func() {
		b.bs = make([]byte, 0)
		b.readBlock = make(chan bool, 0)
		b.writeBlock = make(chan bool, 0)
		close(b.writeBlock)
		if b.Size <= 0 {
			b.Size = defSize
		}
	})
}

// Len
func (b *bq) Len() uint32 {
	b.init()
	b.RLock()
	defer b.RUnlock()
	return uint32(len(b.bs))
}

// Pop
func (b *bq) Pop(byt []byte) (nn uint32) {
	b.init()
	return b.pop(byt)
}

// BlockPop
func (b *bq) BlockPop(byt []byte) (nn uint32) {
	b.init()
	select {
	case <-b.readBlock:
		return b.pop(byt)
	case <-time.After(time.Duration(1000) * time.Millisecond):
		panic("to")
	}
}

// BlockPopWithStop
func (b *bq) BlockPopWithStop(byt []byte, stop chan struct{}) (nn uint32, err error) {
	b.init()
	select {
	case <-b.readBlock:
		return b.pop(byt), nil
	case <-stop:
		return 0, ErrStoped
	}

}

type pushEvent func()

// BlockPush
func (b *bq) BlockPush(e pushEvent, byt ...byte) {
	b.init()
	for {
		bl := uint32(len(byt))
		if bl <= 0 {
			return
		}
		byt = b.push(byt...)
		if e != nil {
			e()
		}
	}
}

// push
func (b *bq) push(byt ...byte) (rest []byte) {
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
func (b *bq) pop(byt []byte) (nn uint32) {
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
