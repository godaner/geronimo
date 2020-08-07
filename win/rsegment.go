package win

import (
	"fmt"
	"sync"
)

// rSegment
type rSegment struct {
	sync.RWMutex
	seq uint16
	bs  []byte
}
// newRSegment
func newRSegment(seqN uint16, bs []byte) (rs *rSegment) {
	return &rSegment{
		seq: seqN,
		bs:  bs,
	}
}
func (r *rSegment) Bs() (bs []byte) {
	r.RLock()
	defer r.RUnlock()
	return r.bs
}
func (r *rSegment) Seq() (seq uint16) {
	r.RLock()
	defer r.RUnlock()
	return r.seq
}
// String
func (r *rSegment) String() string {
	return fmt.Sprintf("{%v:%v}", r.seq, string(r.bs))
}
