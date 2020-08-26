package win

import (
	"sync"
	"time"
)

const (
	wr = time.Duration(10) * time.Second
)

// rtprop
type rtprop struct {
	sync.Once
	n     uint8
	v     time.Duration // min rtt
	t     *time.Timer
	CS    chan struct{} // close signal
	initV bool
}

func (r *rtprop) init() {
	r.Do(func() {
		r.t = time.NewTimer(wr)
		r.v = 0
		r.initV = true
	})
}
func (r *rtprop) Get() (v time.Duration) {
	r.init()
	return r.v
}
func (r *rtprop) Com(newrtt time.Duration) {
	r.init()
	select {
	case <-r.CS:
		r.t.Stop()
		return
	case <-r.t.C:
		r.t.Reset(wr)
		r.initV = true
	default:
	}
	if r.initV {
		r.v = newrtt
		r.initV = false
		return
	}
	r.v = time.Duration(_mini64(int64(r.v), int64(newrtt)))
}
