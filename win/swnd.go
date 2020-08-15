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
	defCongWinSize   = 1
	defRecWinSize    = 32
	maxCongWinSize   = defRecWinSize
	obDefCongWinSize = 64
	obDefRecWinSize  = 256
	obMaxCongWinSize = obDefRecWinSize
)

const (
	defSsthresh = 8
	minSsthresh = 2
)
const (
	mss = 1472 - 14 - 16 // - protocol len , - vi len
)
const (
	appBufferMSS  = 10
	appBufferSize = appBufferMSS * mss // n mss
)
const (
	rtts_a     = float64(0.125)
	rttd_b     = float64(0.25)
	min_rto    = time.Duration(1) * time.Nanosecond
	max_rto    = time.Duration(500) * time.Millisecond
	def_rto    = time.Duration(100) * time.Millisecond
	ob_min_rto = time.Duration(1) * time.Nanosecond
	ob_max_rto = time.Duration(500) * time.Millisecond
	ob_def_rto = time.Duration(100) * time.Millisecond
)
const (
	closeTimeout           = time.Duration(5) * time.Second
	clearReadySendInterval = time.Duration(10) * time.Millisecond
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
	errSClosed      = errors.New("swnd closed")
	errCloseTimeout = errors.New("swnd close timeout")
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
	if obDefCongWinSize > obDefRecWinSize {
		panic("obDefCongWinSize > obDefRecWinSize")
	}
	if max_rto <= min_rto {
		panic("max_rto <= min_rto")
	}
	if ob_max_rto <= ob_min_rto {
		panic("ob_max_rto <= ob_min_rto")
	}
	if defCongWinSize != 1 {
		panic("defCongWinSize !=1")
	}
	if minSsthresh > defSsthresh {
		panic("minSsthresh > defSsthresh")
	}
}
func _maxi64(a, b int64) (c int64) {
	if a > b {
		return a
	}
	return b
}

type SWND struct {
	sync.Once
	sync.RWMutex
	writeLock               sync.RWMutex
	appBuffer               *bq                 // from app , wait for send
	sent                    map[uint16]*segment // sent segments
	flushTimer              *time.Timer         // loopFlush not send data
	swnd                    int64               // send window size , swnd = min(cwnd,rwnd)
	cwnd                    int64               // congestion window size
	rwnd                    int64               // receive window size
	hSeq                    uint16              // current head seq , location is head
	tSeq                    uint16              // current tail seq , location is tail
	ssthresh                int64               // ssthresh
	rttd, rtts, rto         time.Duration
	sendFinish, closeSignal chan struct{}
	logger                  logger.Logger
	OverBose                bool
	SegmentSender           SegmentSender
	FTag                    string
}

// Write
func (s *SWND) Write(bs []byte, wdl time.Time) (err error) {
	s.init()
	s.writeLock.Lock()
	defer s.writeLock.Unlock()
	select {
	case <-s.closeSignal:
		s.logger.Warning("SWND : window is closed")
		return errSClosed
	default:
	}
	s.logger.Info("SWND : write appBuffer , len is", len(bs), ", appBuffer len is", s.appBuffer.Len(), ", sent len is", len(s.sent), ", send win size is", s.swnd, ", cong win size is", s.cwnd, ", recv win size is", s.rwnd)
	to := make(<-chan time.Time)
	if !wdl.IsZero() {
		to = time.After(time.Now().Sub(wdl))
	}
	err = s.appBuffer.BlockPushWithSignal(func() {
		s.Lock()
		defer s.Unlock()
		err = s.send(s.readMSS)
		if err != nil {
			s.logger.Error("SWND : push event call send err , err is", err)
			return
		}
	}, s.closeSignal, to, bs...)
	if err != nil {
		s.logger.Error("SWND : push data to appBuffer err , err is", err)
		return err
	}
	return nil
}

// RecvAck
func (s *SWND) RecvAck(seq, ack, winSize uint16) (err error) {
	s.init()
	s.Lock()
	defer s.Unlock()
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
	case EventEnd:
		switch s.OverBose {
		case true:
			s.cwnd *= 2
			if s.cwnd > obMaxCongWinSize {
				s.cwnd = obMaxCongWinSize
			}
		case false:
			if s.ssthresh <= s.cwnd {
				s.cwnd += 1 // avoid cong
			} else {
				s.cwnd *= 2 // slow start
			}
			if s.cwnd > maxCongWinSize {
				s.cwnd = maxCongWinSize
			}
		}
		s.comRTO(float64(ec.rttm))
		s.comSendWinSize()
		s.logger.Info("SWND : segment ack rttm is", ec.rttm, ", rto is", s.rto)
		return nil
	case EventQResend:
		switch s.OverBose {
		case true:
		case false:
			s.ssthresh = _maxi64(s.cwnd/2, minSsthresh)
			s.cwnd = s.ssthresh
			s.comSendWinSize()
		}
		return nil
	case EventResend:
		switch s.OverBose {
		case true:
			s.cwnd--
			if s.cwnd <= 0 {
				s.cwnd = 1
			}
		case false:
			s.ssthresh = _maxi64(s.cwnd/2, minSsthresh)
			s.cwnd = 1
		}
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
		s.ssthresh = defSsthresh
		s.tSeq = minSeqN
		s.hSeq = s.tSeq
		s.appBuffer = &bq{Size: appBufferSize}
		s.sent = make(map[uint16]*segment)
		s.flushTimer = time.NewTimer(clearReadySendInterval)
		s.closeSignal = make(chan struct{})
		s.sendFinish = make(chan struct{})
		switch s.OverBose {
		case true:
			s.rto = ob_def_rto
			s.rtts = ob_def_rto
			s.rttd = ob_def_rto
			s.rwnd = obDefRecWinSize
			s.cwnd = obDefCongWinSize
		case false:
			s.rto = def_rto
			s.rtts = def_rto
			s.rttd = def_rto
			s.rwnd = defRecWinSize
			s.cwnd = defCongWinSize
		}
		s.swnd = s.cwnd
		s.loopFlush()
		s.loopPrint()
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
	s.flushTimer.Stop() // no flush
	bs := make([]byte, mss, mss)
	n := s.appBuffer.BlockPop(bs) // todo ?? blockpop ??
	bs = bs[:n]
	if len(bs) <= 0 {
		return nil
	}
	return newSSegment(s.logger, s.OverBose, s.tSeq, bs, s.rto, s.segmentEvent, s.SegmentSender)
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
	return newSSegment(s.logger, s.OverBose, s.tSeq, bs, s.rto, s.segmentEvent, s.SegmentSender)
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
			return errCloseTimeout
		}

	}

	return nil
}

// comRTO
func (s *SWND) comRTO(rttm float64) {
	switch s.OverBose {
	case true:
		s.rto = ob_def_rto
		//if s.rto < ob_min_rto {
		//	s.rto = ob_min_rto
		//}
		//if s.rto > ob_max_rto {
		//	s.rto = ob_max_rto
		//}
	case false:
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

// loopPrint
func (s *SWND) loopPrint() {
	go func() {
		for {
			select {
			case <-s.closeSignal:
				return
			case <-time.After(5 * time.Second):
				s.logger.Info("SWND : loop print , appBuffer len is", s.appBuffer.Len(), ", sent len is", len(s.sent), ", send win size is", s.swnd, ", recv win size is", s.rwnd, ", cong win size is", s.cwnd, ", rto is", int64(s.rto))
			}
		}
	}()
}
