package v1

import (
	"fmt"
	"github.com/godaner/geronimo/rule"
	"github.com/godaner/geronimo/win/datastruct"
	"log"
	"sync"
)

// The Receive-Window is as follow :
// sws = 5
// seq range = 0-5
//                                       tailSeq
//          head                          tail                 fixedWinSize
// list  <<--|-------|-------|------|-------|-------|-------|-------|--------|---------|---------|-----<< data flow
//           |                              |                       |
// consumed<=|==========>received<==========|======>ready recv<=====|=====>not allow recv
//           |                              |                       |
// seq =     0       1       2      3       4       5       0       1        2         3         4
//
// index =   0       1       2      3       4       5       6       7        8         9         10

type AckCallBack func(ack, receiveWinSize uint16) (err error)

// RWND
//  recv window
type RWND struct {
	// status
	recved       *datastruct.ByteBlockChan
	fixedWinSize uint16 // fixed window size . this is not "receive window size"
	mss          uint16 // mss
	maxSeq       uint16 // max seq
	minSeq       uint16 // min seq
	tailSeq      uint16 // current tail seq , location is tail
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
		bs[i], _ = r.recved.BlockPop()
	}
	return len(bs), nil
}

// Read
func (r *RWND) Read(bs []byte) (n int, err error) {
	r.init()
	if len(bs) <= 0 {
		return 0, nil
	}
	bs[0], _ = r.recved.BlockPop()
	n = 1
	for i := 1; i < len(bs); i++ {
		usable, b, _ := r.recved.Pop()
		if !usable {
			break
		}
		bs[i] = b
		n++
	}
	return n, nil
}

// Recv
func (r *RWND) Recv(seqN uint16, bs []byte) {
	r.init()
	for index, b := range bs {
		r.readyRecv.Store(seqN, &rData{
			b:       b,
			seq:     seqN,
			needAck: index == (len(bs) - 1),
		})
		r.incSeq(&seqN, 1)
	}
	r.recvSig <- true

}

// receiveWinSize
func (r *RWND) receiveWinSize() uint16 {
	return r.fixedWinSize - uint16(r.recved.Len())
}

func (r *RWND) init() {
	r.Do(func() {
		r.fixedWinSize = rule.DefWinSize
		r.mss = rule.MSS
		r.maxSeq = rule.MaxSeqN
		r.minSeq = rule.MinSeqN
		r.tailSeq = r.minSeq
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
						di, _ := r.readyRecv.Load(r.tailSeq)
						if di == nil {
							if !firstCycle {
								return
							}
							err := r.AckCallBack(r.tailSeq, r.receiveWinSize())
							if err != nil {
								log.Println("di is nil , ack callback err , err is", err.Error())
							}
							return
						}
						firstCycle = false
						d := di.(*rData)
						// clear seq cache
						r.readyRecv.Delete(r.tailSeq)
						// slide window , next seq
						r.incSeq(&r.tailSeq, 1)
						if d.needAck { // ack before put data
							err := r.AckCallBack(r.tailSeq, r.receiveWinSize())
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
