package win

import (
	"sync"
	"time"
)

const (
	wb     = 10
	wd     = time.Duration(1) * time.Second
	def_bw = 0
)

// btlBw
type btlBw struct {
	sync.Once
	n uint8
	//lastDr       uint32
	deliveryRate *deliveryRate
	v            float64
	CS           chan struct{} // close signal
	initV        bool
	//ud           uint8         //  0 == , 1 up , 2 down
	drs chan float64
}

func (b *btlBw) init() {
	b.Do(func() {
		b.initV = true
		b.v = def_bw
		b.drs = make(chan float64)
		b.deliveryRate = &deliveryRate{
			DRS: b.drs,
		}
		go func() {
			select {
			case dr := <-b.drs:
				if b.initV {
					b.v = dr
					b.initV = false
					return
				}
				if dr > b.v {
					b.v = dr
				}
			case <-b.CS:
				return
			}
		}()
	})
}
func (b *btlBw) Com() {
	b.init()
	select {
	case <-b.CS:
		return
	default:
	}
	if b.n >= wb {
		b.deliveryRate.stop()
		b.initV = true
		return
	}
	if b.deliveryRate.stoped() {
		b.deliveryRate = &deliveryRate{
			DRS: b.drs,
		}
	}
	b.deliveryRate.com()

}
func (b *btlBw) Get() (dr float64) {
	b.init()
	return b.v
}

// deliveryRate
type deliveryRate struct {
	sync.Once
	t   *time.Timer
	d   uint32
	cs  chan struct{} // close signal
	DRS chan float64
	st  time.Time
}

func (d *deliveryRate) init() {
	d.Do(func() {
		d.t = time.NewTimer(wd)
		d.d = 0
		d.cs = make(chan struct{})
		d.st = time.Now()
		go func() {
			defer d.t.Stop()
			select {
			case <-d.t.C:
				close(d.cs)
				select {
				case d.DRS <- float64(d.d) / float64(time.Now().Sub(d.st)/1e6):
				default:
				}
				return
			}
		}()
	})
}
func (d *deliveryRate) com() {
	d.init()
	select {
	case <-d.cs:
		return
	default:
	}
	d.d++
}
func (d *deliveryRate) stop() {
	d.init()
	d.t.Stop()
}
func (d *deliveryRate) stoped() bool {
	d.init()
	select {
	case <-d.cs:
		return true
	default:
	}
	return false
}
