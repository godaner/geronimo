package v1

import (
	"errors"
	"fmt"
	"github.com/godaner/geronimo/rule"
	"github.com/godaner/geronimo/win/ds"
	"io"
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

var (
	ErrRWNDClosed = errors.New("rwnd closed")
)

type AckSender func(ack uint32, receiveWinSize uint16) (err error)

// RWND
//  Receive window
type RWND struct {
	sync.Mutex
	sync.Once
	AckSender   AckSender
	recved      *ds.ByteBlockChan
	recvWinSize int32     // recv window size
	readyRecv   *sync.Map //map[uint32]*rData // cache recved package
	tailSeq     uint32    // current tail seq , location is tail
	ackWin      bool
	closeSignal chan bool
}

// rData
type rData struct {
	seq     uint32
	isAlive bool
	bs      []byte
}

// String
func (r *rData) String() string {
	return fmt.Sprintf("{%v:%v}", r.seq, string(r.bs))
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
	select {
	case <-r.closeSignal:
		return 0, io.EOF
	default:

	}
	if len(bs) <= 0 {
		return 0, nil
	}
	bs[0], _, err = r.recved.BlockPopWithStop(r.closeSignal)
	if err != nil {
		return 0, io.EOF
	}
	n = 1
	for i := 1; i < len(bs); i++ {
		usable, b, _, err := r.recved.PopWithStop(r.closeSignal)
		if err != nil {
			return 0, io.EOF
		}
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
func (r *RWND) RecvSegment(seqN uint32, bs []byte) (err error) {
	r.init()
	select {
	case <-r.closeSignal:
		log.Println("RWND : window is closeSignal")
		return ErrRWNDClosed
	default:
	}
	r.Lock()
	defer r.Unlock()
	log.Println("RWND : recv seq is [", seqN, "]")
	if !r.inRecvSeqRange(seqN) {
		ackN := seqN
		r.incSeq(&ackN, uint16(len(bs)))
		r.ack("4", &ackN)
		return
	}
	rdI, ok := r.readyRecv.Load(seqN)
	rd, _ := rdI.(*rData)
	if !ok {
		rd = &rData{}
		r.readyRecv.Store(seqN, rd)
	}
	if !ok || !rd.isAlive {
		r.incRecvWinSize(-1)
	}
	rd.isAlive = true
	rd.seq = seqN
	rd.bs = bs
	r.recv()
	return
}

func (r *RWND) init() {
	r.Do(func() {
		r.recvWinSize = int32(rule.DefRecWinSize)
		r.tailSeq = rule.MinSeqN
		r.recved = &ds.ByteBlockChan{Size: 0}
		r.readyRecv = &sync.Map{}
		r.closeSignal = make(chan bool)
		//r.loopAckWin()
		r.loopPrint()
	})
}

// recv
func (r *RWND) recv() {
	firstCycle := true // eg. if no firstCycle , cache have seq 9 , 9 ack will be sent twice
	for {
		di, ok := r.readyRecv.Load(r.tailSeq)
		d, _ := di.(*rData)
		if !ok || d == nil || !d.isAlive {
			if !firstCycle {
				return
			}
			r.ack("1", nil)
			return
		}
		firstCycle = false
		// delete cache data
		d.isAlive = false
		// slide window , next seq
		r.incSeq(&r.tailSeq, 1)
		// put data to received
		r.recved.BlockPushs(d.bs...)
		// reset window size
		r.incRecvWinSize(1)
		// ack it
		r.ack("2", nil)
	}
}

// ack
func (r *RWND) ack(tag string, ackN *uint32) {
	if ackN == nil {
		a := r.tailSeq
		ackN = &a
	}
	rws := r.getRecvWinSize()
	log.Println("RWND : tag is ", tag, ", send ack , ack is [", *ackN, "] , win size is", rws)
	if rws <= 0 {
		log.Println("RWND : tag is ", tag, ", set ackWin")
		r.ackWin = true
	}
	err := r.AckSender(*ackN, rws)
	if err != nil {
		log.Println("RWND : tag is ", tag, ", ack callback err , err is", err.Error())
	}
}

// incSeq
func (r *RWND) incSeq(seq *uint32, step uint16) {
	*seq = (*seq+uint32(step))%rule.MaxSeqN + rule.MinSeqN
}

// incRecvWinSize
func (r *RWND) incRecvWinSize(step int32) (rws uint16) {
	r.recvWinSize += step
	if r.recvWinSize > rule.DefRecWinSize {
		panic("fuck the window size")
	}
	return uint16(r.recvWinSize)
}

// getRecvWinSize
func (r *RWND) getRecvWinSize() (rws uint16) {
	if r.recvWinSize < 0 {
		return uint16(0)
	}
	return uint16(r.recvWinSize)
}

// loopPrint
func (r *RWND) loopPrint() {
	go func() {
		for {
			select {
			case <-r.closeSignal:
				return
			default:
				log.Println("RWND : print , recved len is", r.recved.Len(), ", recv win size is", r.getRecvWinSize(), ", ackWin is", r.ackWin)
				//log.Println("RWND : recv win size is", r.recvWinSize())
				time.Sleep(1000 * time.Millisecond)
			}

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
			case <-r.closeSignal:
				return
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

// inRecvSeqRange
func (r *RWND) inRecvSeqRange(seq uint32) (yes bool) {
	tailSeq := r.tailSeq
	for i := 0; i < rule.DefRecWinSize; i++ {
		if seq == tailSeq {
			return true
		}
		r.incSeq(&tailSeq, 1)
	}
	return false
}

func (r *RWND) Close() (err error) {
	r.init()
	select {
	case <-r.closeSignal:
	default:
		close(r.closeSignal)
	}
	return nil
}
