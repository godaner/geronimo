package v1

import (
	"fmt"
	"github.com/godaner/geronimo/rule"
	"github.com/godaner/geronimo/win/datastruct"
	"log"
	"sync"
	"sync/atomic"
	"time"
)

// The Receive-Window is as follow :
// sws = 5
// seq range = 0-5
//                                       tailSeq
//          head                          tail                 fixedWinSize
// list  <<--|-------|-------|------|-------|-------|-------|-------|--------|---------|---------|-----<< data flow
//           |                              |                       |
// consumed<=|===========>recved<===========|======>ready recv<=====|=====>not allow recv
//           |                              |                       |
// seq =     0       1       2      3       4       5       0       1        2         3         4
//
// index =   0       1       2      3       4       5       6       7        8         9         10
const (
	checkWinInterval = 250
)

type AckCallBack func(ack, receiveWinSize uint16) (err error)

// RWND
//  Receive window
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
	recvSig      chan bool // start recv data to the list
	readyRecv    *sync.Map
	readyRecvNum int64
	ackWin       bool
	tailSeqLock  sync.RWMutex
}

// rData
type rData struct {
	b       byte
	seq     uint16
	needAck bool
	deled   bool
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
		log.Println("RWND : read , win size is", r.recvWinSize())
	}
	return n, nil
}

// RecvSegment
func (r *RWND) RecvSegment(seqN uint16, bs []byte) {
	r.init()
	log.Println("RWND : recv seq is [", seqN, ",", seqN+uint16(len(bs))-1, "]")
	rdI, _ := r.readyRecv.LoadOrStore(seqN, &rData{})
	rd := rdI.(*rData)
	if rd.deled {
		ackN := seqN + uint16(len(bs))
		if len(bs) == 2 && bs[0] == bs[1] {
			ackN = seqN + 1
		}
		r.ack("4", &ackN)
		return
	}
	for index, b := range bs {
		rdI, _ := r.readyRecv.LoadOrStore(seqN, &rData{})
		rd := rdI.(*rData)
		rd.b = b
		rd.seq = seqN
		rd.needAck = index == (len(bs) - 1)
		rd.deled = false
		r.incTailSeq(1)
		atomic.AddInt64(&r.readyRecvNum, 1)
	}
	r.recvSig <- true
}

// recvWinSize
func (r *RWND) recvWinSize() uint16 {
	i32 := int32(r.fixedWinSize) - int32(r.recved.Len()) - int32(r.readyRecvNum)
	//i32 := int32(r.fixedWinSize) - int32(r.recved.Len())
	if i32 <= 0 {
		return 0
	}
	return uint16(i32)
}

func (r *RWND) init() {
	r.Do(func() {
		r.fixedWinSize = rule.DefWinSize
		r.mss = rule.MSS
		r.maxSeq = rule.MaxSeqN
		r.minSeq = rule.MinSeqN
		r.tailSeq = r.minSeq
		r.recved = &datastruct.ByteBlockChan{Size: rule.U1500} // todo ???? how much ????
		r.recvSig = make(chan bool)
		r.readyRecv = &sync.Map{}
		r.loopRecv()
		r.loopAckWin()
		r.loopPrint()
	})
}

// loopRecv
func (r *RWND) loopRecv() {
	go func() {
		for {
			select {
			case <-r.recvSig:
				func() {
					firstCycle := true // eg. if no firstCycle , cache have seq 9 , 9 ack will be sent twice
					for {
						tailSeq := r.getTailSeq()
						di, _ := r.readyRecv.Load(tailSeq)
						if di == nil {
							if !firstCycle {
								return
							}
							r.ack("1", nil)
							return
						}
						firstCycle = false
						d := di.(*rData)
						// clear seq cache
						r.readyRecv.Delete(tailSeq)
						atomic.AddInt64(&r.readyRecvNum, -1)
						// slide window , next seq
						r.incTailSeq(1)
						// put data to received
						r.recved.Push(d.b)
						// ack it
						if d.needAck {
							r.ack("2", nil)
							return // segment end
						}
					}
				}()
			}
		}
	}()
}

// ack
func (r *RWND) ack(tag string, ackN *uint16) {
	if ackN == nil {
		a := r.getTailSeq()
		ackN = &a
	}
	rws := r.recvWinSize()
	log.Println("RWND : tag is ", tag, ", send ack , ack is", *ackN, " , win size is", rws)
	if rws <= 0 {
		log.Println("RWND : tag is ", tag, ", set ackWin")
		r.ackWin = true
	}
	err := r.AckCallBack(*ackN, rws)
	if err != nil {
		log.Println("RWND : tag is ", tag, ", ack callback err , err is", err.Error())
	}
}

// incTailSeq
func (r *RWND) incTailSeq(step uint16) (tailSeq uint16) {
	r.tailSeqLock.Lock()
	defer r.tailSeqLock.Unlock()
	r.tailSeq = (r.tailSeq+step)%r.maxSeq + r.minSeq
	return r.tailSeq
}
// getTailSeq
func (r *RWND) getTailSeq() (tailSeq uint16) {
	r.tailSeqLock.RLock()
	defer r.tailSeqLock.RUnlock()
	return r.tailSeq
}

// loopPrint
func (r *RWND) loopPrint() {
	go func() {
		for {
			log.Println("RWND : print , recv win size is", r.recvWinSize(), ", readyRecvNum is", r.readyRecvNum, ", ackWin is", r.ackWin)
			//log.Println("RWND : recv win size is", r.recvWinSize())
			time.Sleep(1000 * time.Millisecond)
		}
	}()
}

// loopAckWin
func (r *RWND) loopAckWin() {
	go func() {
		t := time.NewTicker(time.Duration(checkWinInterval) * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				if r.ackWin && r.recvWinSize() > 0 {
					log.Println("RWND : ack win , recv size is", r.recvWinSize())
					r.ackWin = false
					r.ack("3", nil)
				}
			}
		}
	}()
}
