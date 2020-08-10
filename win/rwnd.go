package win

import (
	"errors"
	"fmt"
	"github.com/godaner/geronimo/logger"
	gologging "github.com/godaner/geronimo/logger/go-logging"
	"io"
	"sync"
)

// The Receive-Window is as follow :
// sws = 5
// seq range = 0-5
//                                        tSeq
//          head                          tail                     rwnd
// list  <<--|-------|-------|------|-------|-------|-------|-------|--------|---------|---------|-----<< data flow
//           |                              |                       |
// consumed<=|===========>appBuffer<===========|======>ready recv<=====|=====>not allow recv
//           |                              |                       |
// seq =     0       1       2      3       4       5       0       1        2         3         4
//
// index =   0       1       2      3       4       5       6       7        8         9         10

var (
	ErrRWNDClosed = errors.New("rwnd closed")
)

type AckSender func(seqN, ack, receiveWinSize uint16) (err error)

// RWND
//  Receive window
type RWND struct {
	sync.Mutex
	sync.Once
	readLock    sync.RWMutex
	appBuffer   *bq                 // to app
	recved      map[uint16]*segment // received segment
	tailSeq     uint16              // current tail seq , location is tail
	closeSignal chan struct{}
	logger      logger.Logger
	AckSender   AckSender
	FTag        string
}

// Read
func (r *RWND) Read(bs []byte) (n int, err error) {
	r.init()
	r.readLock.Lock()
	defer r.readLock.Unlock()
	select {
	case <-r.closeSignal:
		return 0, io.EOF
	default:

	}
	if len(bs) <= 0 {
		return 0, nil
	}
	var nn uint32
	nn, err = r.appBuffer.BlockPopWithStop(bs, r.closeSignal)
	if err != nil {
		return 0, io.EOF
	}
	return int(nn), nil
}

// Recv
func (r *RWND) Recv(seqN uint16, bs []byte) (err error) {
	r.init()
	r.Lock()
	defer r.Unlock()
	// closed ?
	select {
	case <-r.closeSignal:
		r.logger.Warning("RWND : window is closeSignal")
		return ErrRWNDClosed
	default:
	}
	// illegal seq ?
	r.logger.Info("RWND : recv seq is [", seqN, "] , segment len is", len(bs))
	if !r.legalSeqN(seqN) {
		return
	}
	// receive the segment
	r.recv(newRSegment(seqN, bs))
	return
}
func (r *RWND) String() string {
	return fmt.Sprintf("RWND:%v->%v", &r, r.FTag)
}
func (r *RWND) init() {
	r.Do(func() {
		if r.FTag == "" {
			r.FTag = "nil"
		}
		r.logger = gologging.GetLogger(r.String())
		r.tailSeq = minSeqN
		r.appBuffer = &bq{Size: appBufferSize}
		r.recved = make(map[uint16]*segment)
		r.closeSignal = make(chan struct{})
	})
}

// recv
func (r *RWND) recv(rs *segment) {
	defer func() {
		r.ack(rs.seq, nil)
	}()
	// put it to receved
	r.recved[rs.seq] = rs
	// push received segment data to app
	for {
		if r.recved2AppBuffer() {
			return
		}
	}
}

// recved2AppBuffer
func (r *RWND) recved2AppBuffer() (breakk bool) {
	seg := r.recved[r.tailSeq]
	if seg == nil {
		return true
	}
	//rr := make(chan struct{})
	//go func() { // test
	//	select {
	//	case <-rr:
	//		return
	//	case <-time.After(time.Duration(10) * time.Second):
	//		panic("recved2AppBuffer push timeout , tag is : " + r.FTag + " , appBuffer len is : " + fmt.Sprint(r.appBuffer.Len()))
	//	}
	//}()
	// put segment data to application buffer
	r.appBuffer.BlockPush(nil, seg.bs...)
	//close(rr)
	// delete segment
	delete(r.recved, r.tailSeq)
	seg = nil
	// slide window , next seq
	r.tailSeq++
	return false
}

// ack
func (r *RWND) ack(seqN uint16, ackN *uint16) {
	if ackN == nil {
		a := r.tailSeq
		ackN = &a
	}
	r.logger.Info("RWND : send ack , seq is", seqN, ", ack is [", *ackN, "]")
	go func() {
		err := r.AckSender(seqN, *ackN, 0)
		if err != nil {
			r.logger.Error("RWND : ack callback err , err is", err.Error())
		}
	}()
}

// legalSeqN
func (r *RWND) legalSeqN(seqN uint16) (yes bool) {
	tailSeq := r.tailSeq
	for i := 0; i < defRecWinSize; i++ {
		if seqN == tailSeq { // legal
			return true
		}
		tailSeq++
	}
	// illegal
	ackN := seqN + 1
	r.ack(seqN, &ackN)
	r.logger.Warning("RWND : illegal seqN , recv seq is [", seqN, "]", ", ack is", ackN)
	return false
}

func (r *RWND) Close() (err error) {
	r.init()
	select {
	case <-r.closeSignal:
		return nil
	default:
		close(r.closeSignal)
	}
	return nil
}
