package win

import (
	"errors"
	"fmt"
	"github.com/godaner/geronimo/logger"
	gologging "github.com/godaner/geronimo/logger/go-logging"
	"math"
	"sync"
	"time"
)

// The send-Window is as follow :
// sws = 5
// seq range = 0-5
//                hSeq                                  tSeq
//          	  head                                  tail                              swnd
// list    <<------|-------|-------|------|-------|-------|-------|-------|-------|--------|--------|-----<< data flow
//                 |                                      |                                |
// sent and ack<===|==========>sent but not ack<==========|====>allow send but not send<===|===>not allow send
//                 |                                      |                                |
// seq =           0       1       2      3       4       5       0       1        2       3       4
//
// index =         0       1       2      3       4       5       6       7        8       9       10

const (
	maxSeqN = uint16(1<<16 - 1) // [0,65535]
	minSeqN = 0
)

const (
	defCongWinSize = 1
	defRecWinSize  = 32
)

const (
	defSsthresh = 8
)
const (
	mss = 1472 - 14
)
const (
	appBufferMSS  = 10
	appBufferSize = appBufferMSS * mss // n mss
)
const (
	rtts_a   = float64(0.125)
	rttd_b   = float64(0.25)
	min_rto  = time.Duration(1) * time.Nanosecond
	max_rto  = time.Duration(500) * time.Millisecond
	def_rto  = time.Duration(100) * time.Millisecond
	def_rtts = def_rto
	def_rttd = def_rto
)
const (
	closeTimeout               = time.Duration(5) * time.Second
	clearReadySendInterval     = time.Duration(100) * time.Millisecond
	clearReadySendLongInterval = time.Duration(1) * time.Minute // ms
	// flush
	closeCheckFlushInterval = closeCheckFlushTO / closeCheckFlushNum
	closeCheckFlushNum      = appBufferMSS * 2 // should ge bq size
	closeCheckFlushTO       = time.Duration(1000) * time.Millisecond
	// ack
	closeCheckAckInterval = closeCheckAckTO / closeCheckAckNum
	closeCheckAckNum      = appBufferMSS * 2
	closeCheckAckTO       = time.Duration(3000) * time.Millisecond
)

var (
	ErrSWNDClosed       = errors.New("swnd closed")
	ErrSWNDCloseTimeout = errors.New("swnd close timeout")
)

func init() {
	if maxSeqN <= minSeqN {
		panic("maxSeqN <= minSeqN")
	}
	if defRecWinSize > maxSeqN {
		panic("defRecWinSize > maxSeqN")
	}
	if defCongWinSize > defRecWinSize {
		panic("defCongWinSize > defRecWinSize")
	}
	if max_rto <= min_rto {
		panic("max_rto <= min_rto")
	}
}

type SWND struct {
	sync.Once
	sync.RWMutex
	writeLock sync.RWMutex
	appBuffer *bq // from app , wait for send
	//sentC           int64       // sent segment count
	//sent            []*segment  // sent segments
	sent            map[uint16]*segment // sent segments
	flushTimer      *time.Timer         // loopFlush not send data
	swnd            int64               // send window size , swnd = min(cwnd,rwnd)
	cwnd            int64               // congestion window size
	rwnd            int64               // receive window size
	hSeq            uint16              // current head seq , location is head
	tSeq            uint16              // current tail seq , location is tail
	ssthresh        int64               // ssthresh
	rttd, rtts, rto time.Duration
	closeSignal     chan bool
	sendFinish      chan bool
	logger          logger.Logger
	SegmentSender   SegmentSender
	FTag            string
}

// Write
func (s *SWND) Write(bs []byte) (err error) {
	s.init()
	s.writeLock.Lock()
	defer s.writeLock.Unlock()
	select {
	case <-s.closeSignal:
		s.logger.Warning("SWND : window is closed")
		return ErrSWNDClosed
	default:
	}
	s.logger.Info("SWND : write appBuffer , len is", len(bs), ", appBuffer len is", s.appBuffer.Len(), ", sent len is", len(s.sent), ", send win size is", s.swnd, ", cong win size is", s.cwnd, ", recv win size is", s.rwnd)
	//r := make(chan struct{})
	//go func() { // test
	//	select {
	//	case <-r:
	//		return
	//	case <-time.After(time.Duration(10) * time.Second):
	//		panic("Write push timeout , tag is : " + s.FTag + " , appBuffer len is : " + fmt.Sprint(s.appBuffer.Len()))
	//	}
	//}()
	s.appBuffer.BlockPush(func() {
		s.Lock()
		defer s.Unlock()
		err = s.send(s.readMSS)
		if err != nil {
			s.logger.Error("SWND : push event call send err , err is", err)
			return
		}
	}, bs...)
	//close(r) // test
	return nil
}

// RecvAck
func (s *SWND) RecvAck(seq, ack, winSize uint16) (err error) {
	s.init()
	s.Lock()
	defer s.Unlock()
	//s.logger.Info("SWND : recv seq is", seq, ", ack is [", ack, "]")
	s.logger.Info("SWND : recv ack , seq is", seq, ", ack is [", ack, "] , appBuffer len is", s.appBuffer.Len(), ", sent len is", len(s.sent), ", send win size is", s.swnd, ", cong win size is", s.cwnd, ", recv win size is", s.rwnd)
	// ack segment
	seg := s.sent[seq]
	if seg != nil {
		err = seg.AckIi()
		if err != nil {
			s.logger.Error("SWND : seg ack err , err is", err)
			return err
		}
	}
	// trim ack segment
	s.trimAckSeg()
	err = s.send(s.readMSS)
	if err != nil {
		s.logger.Error("SWND : recv ack send err , err is", err)
		return err
	}
	// tre quick resend segment
	seg = s.sent[ack]
	if seg != nil {
		err = seg.TryQResend()
		if err != nil {
			s.logger.Error("SWND : seg tryQResend err , err is", err)
			return err
		}
	}
	return nil
}

// segmentEvent
func (s *SWND) segmentEvent(e event, ec *eContext) (err error) {
	switch e {
	case EventFinish:
		if s.ssthresh <= s.cwnd {
			s.cwnd += 1
		} else {
			s.cwnd *= 2 // slow start
		}
		if s.cwnd > defRecWinSize {
			s.cwnd = defRecWinSize
		}
		s.comSendWinSize()
		s.comRTO(float64(ec.rttm))
		s.logger.Info("SWND : segment finish rttm is", ec.rttm, ", rto is", s.rto)
		return nil
	case EventQResend:
		s.ssthresh = s.cwnd / 2
		s.cwnd = s.ssthresh
		s.comSendWinSize()
		return nil
	case EventResend:
		s.ssthresh = s.cwnd / 2
		s.cwnd = 1
		s.comSendWinSize()
		return nil
	}
	panic("error segment event")
	return nil
}

// comSendWinSize
func (s *SWND) comSendWinSize() {
	s.swnd = int64(math.Min(float64(s.rwnd), float64(s.cwnd)))
}
func (s *SWND) String() string {
	return fmt.Sprintf("SWND:%v->%v", &s, s.FTag)
}
func (s *SWND) init() {
	s.Do(func() {
		if s.FTag == "" {
			s.FTag = "nil"
		}
		s.logger = gologging.GetLogger(s.String())
		s.rto = def_rto
		s.rtts = def_rtts
		s.rttd = def_rttd
		s.ssthresh = defSsthresh
		s.rwnd = defRecWinSize
		s.cwnd = defCongWinSize
		s.swnd = s.cwnd
		s.tSeq = minSeqN
		s.hSeq = s.tSeq
		s.appBuffer = &bq{Size: appBufferSize}
		s.sent = make(map[uint16]*segment)
		s.flushTimer = time.NewTimer(clearReadySendInterval)
		s.closeSignal = make(chan bool)
		s.sendFinish = make(chan bool)
		s.loopFlush()
	})
}
func (s *SWND) trimAckSeg() {
	defer func() {
		s.logger.Info("SWND : trimAckSeg , sent len is", len(s.sent))
	}()
	for {
		seg := s.sent[s.hSeq]
		if seg == nil || !seg.IsAck() {
			return
		}
		delete(s.sent, s.hSeq)
		seg = nil
		s.hSeq++
	}
}

// segmentReader
type segmentReader func() (seg *segment)

// readMSS
//  implement segmentReader
func (s *SWND) readMSS() (seg *segment) {
	if s.appBuffer.Len() <= 0 {
		return nil
	}
	if s.swnd-int64(len(s.sent)) <= 0 { // no enough win size to send todo
		return nil
	}
	if s.appBuffer.Len() < mss { // no enough data to fill a mss
		s.flushTimer.Reset(clearReadySendInterval) // flush after n ms
		return nil
	}
	bs := make([]byte, mss, mss)
	n := s.appBuffer.BlockPop(bs) // todo ?? blockpop ??
	if s.appBuffer.Len() > 0 { // rest data wait to flush
		s.flushTimer.Reset(clearReadySendInterval) // flush after n ms
	} else {
		s.flushTimer.Reset(clearReadySendLongInterval) // no flush
	}
	bs = bs[:n]
	if len(bs) <= 0 {
		return nil
	}
	return newSSegment(s.logger, s.tSeq, bs, s.rto, s.segmentEvent, s.SegmentSender)
}

// readAny
//  implement segmentReader
func (s *SWND) readAny() (seg *segment) {
	if s.appBuffer.Len() <= 0 {
		return nil
	}
	bs := make([]byte, mss, mss)
	n := s.appBuffer.BlockPop(bs) // todo ?? blockpop ??
	bs = bs[:n]
	if len(bs) <= 0 {
		return nil
	}
	return newSSegment(s.logger, s.tSeq, bs, s.rto, s.segmentEvent, s.SegmentSender)
}

// send
func (s *SWND) send(sr segmentReader) (err error) {
	for {
		seg := sr()
		if seg == nil {
			return
		}
		// put it to queue
		s.sent[s.tSeq] = seg
		// inc seq
		s.tSeq++
		// send
		s.logger.Info("SWND : segment send , seq is [", seg.Seq(), "] , segment len is", len(seg.Bs()), ", appBuffer len is", s.appBuffer.Len(), ", sent len is", len(s.sent), ", send win size is", s.swnd, ", cong win size is", s.cwnd, ", recv win size is", s.rwnd, ", rto is", int64(s.rto))
		err := seg.Send()
		if err != nil {
			return err
		}

	}
}

// loopFlush
func (s *SWND) loopFlush() {
	go func() {
		defer func() {
			s.flushTimer.Stop()
			s.logger.Warning("SWND : loopFlush stop")
			close(s.sendFinish)
		}()
		for {
			select {
			case <-s.closeSignal:
				s.flushLastSegment()
				s.waitLastAck()
				return
			case <-s.flushTimer.C:
				s.Lock()
				s.logger.Debug("SWND : flushing , appBuffer len is", s.appBuffer.Len(), ", sent len is", len(s.sent), ", send win size is", s.swnd, ", recv win size is", s.rwnd, ", cong win size is", s.cwnd, ", rto is", int64(s.rto))
				err := s.send(s.readAny)
				if err != nil {
					s.logger.Debug("SWND : flushing err , err is", err)
					s.Unlock()
					continue
				}
				s.Unlock()
			}
		}
	}()
}

func (s *SWND) Close() (err error) {
	s.init()
	select {
	case <-s.closeSignal:
	default:
		close(s.closeSignal)
		select {
		case <-s.sendFinish:
		case <-time.After(closeTimeout):
			return ErrSWNDCloseTimeout
		}

	}

	return nil
}

// comRTO
func (s *SWND) comRTO(rttm float64) {
	s.rtts = time.Duration((1-rtts_a)*float64(s.rtts) + rtts_a*rttm)
	s.rttd = time.Duration((1-rttd_b)*float64(s.rttd) + rttd_b*math.Abs(rttm-float64(s.rtts)))
	s.rto = s.rtts + 4*s.rttd
	if s.rto < min_rto {
		s.rto = min_rto
	}
	if s.rto > max_rto {
		s.rto = max_rto
	}
}

// flushLastSegment
func (s *SWND) flushLastSegment() {
	ft := time.NewTimer(closeCheckFlushTO)
	defer ft.Stop()
	// send all
	sn := 0
	for {
		select {
		case <-ft.C:
			s.logger.Error("SWND : flushLastSegment flush timeout , flush num is", sn, ", appBuffer len is", s.appBuffer.Len())
			//panic("flush timeout")
			return
		case <-time.After(closeCheckFlushInterval):
			if sn > closeCheckFlushNum {
				s.logger.Error("SWND : flushLastSegment stopping flush , flush num beynd , flush num is", sn, ", appBuffer len is", s.appBuffer.Len())
				return
			}
			s.Lock()
			s.logger.Info("SWND : flushLastSegment stopping flush , appBuffer is", s.appBuffer.Len())
			if s.appBuffer.Len() <= 0 {
				s.logger.Info("SWND : flushLastSegment stopping flush finish , appBuffer is", s.appBuffer.Len())
				s.Unlock()
				return
			}
			err := s.send(s.readAny) // clear data
			if err != nil {
				s.logger.Error("SWND : flushLastSegment stopping flush err , appBuffer is", s.appBuffer.Len(), ", err is", err)
				s.Unlock()
				return
			}
			s.Unlock()
			sn++

		}
	}
}

// waitLastAck
func (s *SWND) waitLastAck() {
	at := time.NewTimer(closeCheckAckTO)
	defer at.Stop()
	// recv all ack
	an := 0
	for {
		select {
		case <-at.C:
			s.logger.Error("SWND : waitLastAck flush timeout , ack num is", an, ", sentC is", len(s.sent))
			//panic("flush timeout")
			return
		case <-time.After(closeCheckAckInterval):
			if an > closeCheckAckNum {
				s.logger.Error("SWND : waitLastAck stopping flush , ack num beynd , ack num is", an)
				return
			}
			s.logger.Info("SWND : waitLastAck stopping flush , sentC is", len(s.sent))
			if len(s.sent) <= 0 {
				s.logger.Info("SWND : waitLastAck stopping flush finish , sentC is", len(s.sent))
				return
			}
			an++
		}

	}
}