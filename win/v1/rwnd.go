package v1

import (
	"fmt"
	"github.com/godaner/geronimo/rule"
	"github.com/godaner/geronimo/win/datastruct"
	"log"
	"sync"
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
	checkWinInterval = 100
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
	readyRecvNum uint32
	ackWin       bool
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
		log.Println("RWND : read , win size is", r.recvWinSize())
	}
	return n, nil
}

// Recv
func (r *RWND) Recv(seqN uint16, bs []byte) {
	r.init()
	log.Println("RWND : recv seq is [", seqN, ",", seqN+uint16(len(bs))-1, "]")
	for index, b := range bs {
		r.readyRecv.Store(seqN, &rData{
			b:       b,
			seq:     seqN,
			needAck: index == (len(bs) - 1),
		})
		r.incSeq(&seqN, 1)
		r.readyRecvNum++
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
		r.recved = &datastruct.ByteBlockChan{Size: rule.MaxWinSize}
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
						di, _ := r.readyRecv.Load(r.tailSeq)
						if di == nil {
							if !firstCycle {
								return
							}
							r.ack()
							return
						}
						firstCycle = false
						d := di.(*rData)
						// clear seq cache
						r.readyRecv.Delete(r.tailSeq)
						r.readyRecvNum--
						// slide window , next seq
						r.incSeq(&r.tailSeq, 1)
						if d.needAck { // ack before put data
							r.ack()
						}
						// put data to received , maybe block , so put it at the end
						r.recved.Push(d.b)
					}
				}()
			}
		}
	}()
}

// ack
func (r *RWND) ack() {
	rws := r.recvWinSize()
	if rws <= 0 {
		log.Println("RWND : set ackWin")
		r.ackWin = true
	}
	err := r.AckCallBack(r.tailSeq, rws)
	if err != nil {
		log.Println("RWND : ack callback err , err is", err.Error())
	}
}

// incSeq
func (r *RWND) incSeq(seq *uint16, step uint16) {
	*seq = (*seq+step)%r.maxSeq + r.minSeq
}

// loopPrint
func (r *RWND) loopPrint() {
	go func() {
		for {
			log.Println("RWND : recv win size is", r.recvWinSize(), ", ackWin is", r.ackWin)
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
					r.ack()
				}
			}
		}
	}()
}
