package v1

import (
	"fmt"
	"github.com/godaner/geronimo/rule"
	"sync"
)

// The Receive-Window is as follow :
// sws = 5
// seq range = 0-5
//          p1                              p2
//     last consumed                 last received             last ready receive
//         [lc,)                           [lr)                   [lrr)=lr + (sws-(lr-lc))
// list  <<--|-------|-------|------|-------|-------|-------|-------|--------|---------|---------|-----<< data flow
//           |                              |                       |
// consumed<=|==========>received<==========|=====>ready receive<===|=====>not allow receive
//           |                              |                       |
// seq =     0       1       2      3       4       5       0       1        2         3         4
//
// index =   0       1       2      3       4       5       6       7        8         9         10

// RWND
//  receive window
type RWND struct {
	// status
	ds     *ArrayList
	rws    uint16 // read window size
	mss    uint16 // mss
	lc    int32  // last consumed
	lr    int32  // last received
	maxSeq uint16 // max seq
	minSeq uint16 // min seq
	// helper
	sync.Once
	winLock               sync.RWMutex // lock the win
	data2Up               chan []byte
	collectReceivedSignal chan bool
}

// rData
type rData struct {
	b   byte
	seq uint16
}

func (r *rData) String() string {
	return fmt.Sprintf("{%v:%v}", r.seq, string(r.b))
}

// Read
func (r *RWND) Read() (bs []byte, n uint32) {
	return // todo
}
func (r *RWND) Write(seqN uint16, bs []byte) {
	rDatas := make([]interface{}, 0)
	for _, b := range bs {
		rDatas = append(rDatas, &rData{
			b:   b,
			seq: seqN,
		})
		seqN++
	}
	r.ds.Append(rDatas...)
}
func (r *RWND) ReceiveWinSize() uint16 {
	return r.rws - uint16(r.frr)
}

func (r *RWND) init() {
	r.Do(func() {
		r.rws = rule.DefWinSize
		r.mss = rule.MSS
		r.maxSeq = rule.MaxSeqN
		r.minSeq = rule.MinSeqN
		r.data2Up = make(chan []byte)
		r.collectReceivedSignal = make(chan bool)
		r.frr = 1
		r.ds = &ArrayList{}
		r.ds.Append(windowHead) // for lwtc , lraa
		//r.ds = make([]*data, 0)
		r.collectReceivedData()
	})
}

// collectAckData
func (r *RWND) collectReceivedData() {
	go func() {
		select {
		case <-r.collectReceivedSignal:
			func() {

			}()
		}
	}()
}
