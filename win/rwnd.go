package win

import (
	"errors"
	"fmt"
	"github.com/godaner/logger"
	loggerfac "github.com/godaner/logger/factory"
	"io"
	"sync"
	"time"
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
	errRClosed = errors.New("rwnd closed")
)

type AckSender func(seqN, ack, receiveWinSize uint16) (err error)

// RWND
//  Receive window
type RWND struct {
	sync.Mutex
	sync.Once
	OverBose    bool
	rwnd        int64
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
func (r *RWND) Read(bs []byte, rdl time.Time) (n int, err error) {
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
	to := make(<-chan time.Time)
	if !rdl.IsZero() {
		to = time.After(time.Now().Sub(rdl))
	}
	var nn uint32
	nn, err = r.appBuffer.BlockPopWithSignal(bs, r.closeSignal, to)
	if err != nil {
		r.logger.Error("RWND : read data from appBuffer err , err is", err)
		if err.Error() == errStoped.Error() {
			return 0, io.EOF
		}
		return 0, err
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
		return errRClosed
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
		r.logger = loggerfac.GetLogger(r.String())
		r.tailSeq = minSeqN
		r.appBuffer = &bq{Size: appBufferSize}
		r.recved = make(map[uint16]*segment)
		r.closeSignal = make(chan struct{})
		switch r.OverBose {
		case true:
			r.rwnd = obDefRecWinSize
		case false:
			r.rwnd = defRecWinSize
		}
		r.loopPrint()
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
	// put segment data to application buffer
	// maybe closed , no consumer will block
	err := r.appBuffer.BlockPushWithSignal(nil, r.closeSignal, make(<-chan time.Time), seg.Bs()...)
	if err != nil {
		r.logger.Error("RWND : recved to appBuffer err , err is", err.Error())
		return true
	}
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
	for i := int64(0); i < r.rwnd; i++ {
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

// loopPrint
func (r *RWND) loopPrint() {
	go func() {
		for {
			select {
			case <-r.closeSignal:
				return
			case <-time.After(5 * time.Second):
				r.logger.Info("RWND : loop print , appBuffer len is", r.appBuffer.Len(), ", recvd len is", len(r.recved), ", recv win size is", r.rwnd)
			}
		}
	}()
}
