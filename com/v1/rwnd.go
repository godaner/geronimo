package v1

import (
	"fmt"
	"github.com/godaner/geronimo/com/datastruct"
	"github.com/godaner/geronimo/rule"
	"log"
	"sync"
)

// The Receive-Window is as follow :
// sws = 5
// seq range = 0-5
//                                        cSeq
//          head                          tail                     rws
// list  <<--|-------|-------|------|-------|-------|-------|-------|--------|---------|---------|-----<< data flow
//           |                              |                       |
// consumed<=|==========>received<==========|=====>ready recv<===|=====>not allow recv
//           |                              |                       |
// seq =     0       1       2      3       4       5       0       1        2         3         4
//
// index =   0       1       2      3       4       5       6       7        8         9         10

type AckCallBack func(ack, receiveWinSize uint16) (err error)

// RWND
//  recv window
type RWND struct {
	// status
	recved           *datastruct.ByteBlockChan
	fixedRecvWinSize uint16 // fixed recv window size
	mss              uint16 // mss
	maxSeq           uint16 // max seq
	minSeq           uint16 // min seq
	cSeq             uint16 // current seq , location is tail
	// outer
	AckCallBack AckCallBack
	// helper
	sync.Once
	recvSig   chan bool // start recv data to the list
	readyRecv *sync.Map
}

// rData
type rData struct {
	b       byte
	seq     uint16
	needAck bool
}

// String
func (r *rData) String() string {
	return fmt.Sprintf("{%v:%v}", r.seq, string(r.b))
}

// ReadFull
func (r *RWND) ReadFull(bs []byte) (n int, err error) {
	r.init()
	for i := 0; i < len(bs); i++ {
		bs[i], _ = r.recved.Pop()
	}
	return len(bs), nil
}

// Write
func (r *RWND) Write(seqN uint16, bs []byte) {
	r.init()
	for index, b := range bs {
		r.readyRecv.Store(seqN, &rData{
			b:       b,
			seq:     seqN,
			needAck: index == (len(bs) - 1),
		})
		seqN++
	}
	r.recvSig <- true

}

// receiveWinSize
func (r *RWND) receiveWinSize() uint16 {
	return r.fixedRecvWinSize - uint16(r.recved.Len())
}

func (r *RWND) init() {
	r.Do(func() {
		r.fixedRecvWinSize = rule.DefWinSize
		r.mss = rule.MSS
		r.maxSeq = rule.MaxSeqN
		r.minSeq = rule.MinSeqN
		r.cSeq = r.minSeq
		r.recved = &datastruct.ByteBlockChan{Size: rule.MaxWinSize}
		r.recvSig = make(chan bool)
		r.readyRecv = &sync.Map{}
		r.recv()
	})
}

// recv
func (r *RWND) recv() {
	go func() {
		for {
			select {
			case <-r.recvSig:
				func() {
					firstCycle := true // eg. if no firstCycle , cache have seq 9 , 9 ack will be sent twice
					for {
						di, _ := r.readyRecv.Load(r.cSeq)
						if di == nil {
							if !firstCycle {
								return
							}
							err := r.AckCallBack(r.cSeq, r.receiveWinSize())
							if err != nil {
								log.Println("di is nil , ack callback err , err is", err.Error())
							}
							return
						}
						firstCycle = false
						d := di.(*rData)
						// clear seq cache
						r.readyRecv.Delete(r.cSeq)
						// slide window , next seq
						r.incSeq(&r.cSeq, 1)
						if d.needAck { // ack before put data
							err := r.AckCallBack(r.cSeq, r.receiveWinSize())
							if err != nil {
								log.Println("need ack , ack callback err , err is", err.Error())
							}
						}
						// put data to received , maybe block , so put it at the end
						r.recved.Push(d.b)
					}
				}()
			}
		}
	}()
}

// incSeq
func (r *RWND) incSeq(seq *uint16, step uint16) {
	*seq = (*seq+step)%r.maxSeq + r.minSeq
}
