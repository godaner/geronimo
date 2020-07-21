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
	checkWinInterval = 10
)

type AckSender func(ack uint32, receiveWinSize uint16) (err error)

// RWND
//  Receive window
type RWND struct {
	// status
	recved       *datastruct.ByteBlockChan
	recvWinSize  int32  // recv window size
	tailSeq      uint32 // current tail seq , location is tail
	// outer
	AckSender AckSender
	// helper
	sync.Once
	readyRecv       *sync.Map
	ackWin          bool
	tailSeqLock     sync.RWMutex
	recvWinSizeLock sync.RWMutex
	winLock sync.RWMutex
}

// rData
type rData struct {
	b       byte
	seq     uint32
	needAck bool
	isAlive bool
}

// String
func (r *rData) String() string {
	return fmt.Sprintf("{%v:%v}", r.seq, string(r.b))
}

// ReadFull
//func (r *RWND) ReadFull(bs []byte) (n int, err error) {
//	r.init()
//	for i := 0; i < len(bs); i++ {
//		bs[i], _ = r.recved.BlockPop()
//	}
//	return len(bs), nil
//}

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
		//log.Println("RWND : read , win size is", r.getRecvWinSize(), ", char is", string(b))
	}
	return n, nil
}

// RecvSegment
func (r *RWND) RecvSegment(seqN uint32, bs []byte) {
	r.init()
	r.winLock.Lock()
	//s := time.Now().Unix()
	defer func() {
		r.winLock.Unlock()
		//log.Println("RWND : recv seq time is", time.Now().Unix()-s)
	}()
	log.Println("RWND : recv seq is [", seqN, ",", seqN+uint32(len(bs))-1, "]")
	//s1 := time.Now().Unix()
	if !r.inRecvSeqRange(seqN) {
		ackN:=seqN
		r.incSeq(&ackN,uint16(len(bs)))
		r.ack("4", &ackN)
		return
	}
	//log.Println("RWND : recv seq s1 time is", time.Now().Unix()-s1)
	//s2 := time.Now().Unix()
	for index, b := range bs {
		rdI, _ := r.readyRecv.LoadOrStore(seqN, &rData{})
		rd := rdI.(*rData)
		if !rd.isAlive { // not repeat
			r.incRecvWinSize(-1)
		}
		// fill daata
		rd.b = b
		rd.seq = seqN
		rd.needAck = index == (len(bs) - 1)
		rd.isAlive = true
		r.incSeq(&seqN, 1)
	}
	//log.Println("RWND : recv seq s2 time is", time.Now().Unix()-s2)
	//s3 := time.Now().Unix()
	r.recv()
	//log.Println("RWND : recv seq s3 time is", time.Now().Unix()-s3)
}

func (r *RWND) init() {
	r.Do(func() {
		r.recvWinSize = int32(rule.DefRecWinSize)
		r.tailSeq = rule.MinSeqN
		r.recved = &datastruct.ByteBlockChan{Size: 0}
		r.readyRecv = &sync.Map{}
		r.loopAckWin()
		r.loopPrint()
	})
}

// recv
func (r *RWND) recv() {
	firstCycle := true // eg. if no firstCycle , cache have seq 9 , 9 ack will be sent twice
	for {
		tailSeq := r.getTailSeq()
		di, _ := r.readyRecv.Load(tailSeq)
		d, _ := di.(*rData)
		if di == nil || !d.isAlive {
			if !firstCycle {
				return
			}
			r.ack("1", nil)
			return
		}
		firstCycle = false
		// clear seq cache
		d.isAlive = false
		// slide window , next seq
		r.incTailSeq(1)
		// put data to received
		r.recved.BlockPush(d.b)
		// reset window size
		r.incRecvWinSize(1)
		// ack it
		if d.needAck {
			r.ack("2", nil)
			return // segment end
		}
	}
}

// ack
func (r *RWND) ack(tag string, ackN *uint32) {
	if ackN == nil {
		a := r.getTailSeq()
		ackN = &a
	}
	rws := r.getRecvWinSize()
	log.Println("RWND : tag is ", tag, ", send ack , ack is", *ackN, " , win size is", rws)
	if rws <= 0 {
		log.Println("RWND : tag is ", tag, ", set ackWin")
		r.ackWin = true
	}
	err := r.AckSender(*ackN, rws)
	if err != nil {
		log.Println("RWND : tag is ", tag, ", ack callback err , err is", err.Error())
	}
}

// inRecvSeqRange
func (r *RWND) inRecvSeqRange(seq uint32) (yes bool) {
	tailSeq := r.getTailSeq()
	for i := 0; i < int(r.getRecvWinSize()); i++ {
		if tailSeq == seq {
			return true
		}
	}
	return false
}

// incSeq
func (r *RWND) incSeq(seq *uint32, step uint16) {
	*seq = (*seq+uint32(step))%rule.MaxSeqN + rule.MinSeqN
}

// incRecvWinSize
func (r *RWND) incRecvWinSize(step int32) (rws uint16) {
	r.recvWinSizeLock.Lock()
	defer r.recvWinSizeLock.Unlock()
	r.recvWinSize += step
	if r.recvWinSize > rule.DefRecWinSize {
		panic("fuck the window size")
	}
	//r.changeRecvSeqRange(step)
	return uint16(r.recvWinSize)
}

// getRecvWinSize
func (r *RWND) getRecvWinSize() (rws uint16) {
	r.recvWinSizeLock.RLock()
	defer r.recvWinSizeLock.RUnlock()
	if r.recvWinSize < 0 {
		return uint16(0)
	}
	return uint16(r.recvWinSize)
}

// incTailSeq
func (r *RWND) incTailSeq(step uint16) (tailSeq uint32) {
	r.tailSeqLock.Lock()
	defer r.tailSeqLock.Unlock()
	r.incSeq(&r.tailSeq, step)
	return r.tailSeq
}

// getTailSeq
func (r *RWND) getTailSeq() (tailSeq uint32) {
	r.tailSeqLock.RLock()
	defer r.tailSeqLock.RUnlock()
	return r.tailSeq
}
// loopPrint
func (r *RWND) loopPrint() {
	go func() {
		for {
			log.Println("RWND : print , recved len is", r.recved.Len(), ", recv win size is", r.getRecvWinSize(), ", ackWin is", r.ackWin)
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
				rws := r.getRecvWinSize()
				if r.ackWin && rws > 0 {
					log.Println("RWND : ack win , recv size is", rws)
					r.ackWin = false
					r.ack("3", nil)
				}
			}
		}
	}()
}
