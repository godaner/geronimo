package win

import (
	"errors"
	"fmt"
	"github.com/godaner/geronimo/bbr"
	"github.com/godaner/logger"
	loggerfac "github.com/godaner/logger/factory"
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
	quickResendIfSkipGEN = 2
)

const (
	mss = 1472 - 14 - 16 // - protocol len , - vi len
)
const (
	appBufferMSS  = 10
	appBufferSize = appBufferMSS * mss // n mss
)
const (
	rtts_a  = float64(0.125)
	rttd_b  = float64(0.25)
	min_rto = time.Duration(100) * time.Millisecond
	max_rto = time.Duration(1000) * time.Millisecond
	def_rto = time.Duration(500) * time.Millisecond
)
const (
	// flush
	closeCheckFlushTO = time.Duration(100) * time.Millisecond
	// ack
	closeCheckAckTO = time.Duration(2) * time.Second
)
const (
	clearReadySendInterval = time.Duration(10) * time.Millisecond
)
const (
	defRecWinSize = 32
)

var (
	errSClosed      = errors.New("swnd closed")
	errCloseTimeout = errors.New("swnd close timeout")
)

func init() {

	if defRecWinSize > maxSeqN {
		panic("defRecWinSize > maxSeqN")
	}
	if maxSeqN <= minSeqN {
		panic("maxSeqN <= minSeqN")
	}
	if max_rto <= min_rto {
		panic("max_rto <= min_rto")
	}
	if max_rto <= min_rto {
		panic("max_rto <= min_rto")
	}

}
func _maxi64(a, b int64) (c int64) {
	if a > b {
		return a

	}
	return b
}
func _mini64(a, b int64) (c int64) {
	if a > b {
		return b
	}
	return a
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
	rttd, rtts, rto         time.Duration
	sendFinish, closeSignal chan struct{}
	logger                  logger.Logger
	SegmentSender           SegmentSender
	FTag                    string
	bbr                     *bbr.BBR
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
	if seg == nil {
		return nil
	}
	err = seg.AckIi()
	if err != nil {
		s.logger.Error("SWND : seg ack err , err is", err)
		return err
	}
	// trim ack segment
	s.trimAckSeg()
	err = s.send(s.readMSS)
	if err != nil {
		s.logger.Error("SWND : recv ack send err , err is", err)
		return err
	}
	// try quick resend segment
	err = s.quickResend(seq)
	if err != nil {
		s.logger.Error("SWND : quick resend err , err is", err)
		return err
	}
	return nil
}

// quickResend
func (s *SWND) quickResend(seq uint16) (err error) {
	seqs := s.getTryResendSeqs(seq)
	for _, seq := range seqs {
		seg := s.sent[seq]
		if seg != nil {
			err = seg.TryQResend()
			if err != nil {
				s.logger.Error("SWND : seg tryQResend err , err is", err)
				return err
			}
		}
	}
	return nil
}

// inflight
func (s *SWND) inflight() (i int64) {
	return int64(len(s.sent))
}

// setCWND
func (s *SWND) setCWND(n int64) (c int64) {
	s.cwnd = n
	s.comSendWinSize()
	return s.cwnd
}

// segmentEvent
func (s *SWND) segmentEvent(e event, ec *eContext) (err error) {
	switch e {
	case EventEnd:
		seg := ec.seg
		rtt := seg.RTT()
		s.comRTO(rtt)
		s.bbr.Update(int64(len(s.sent)), seg.RTT(), seg.RTTS(), seg.RTTE())
		s.setCWND(s.bbr.GetCWND())
		return nil
	case EventQResend:
		return nil
	case EventTickerResend:
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
		s.logger = loggerfac.GetLogger(s.String())
		s.tSeq = minSeqN
		s.hSeq = s.tSeq
		s.appBuffer = &bq{Size: appBufferSize}
		s.sent = make(map[uint16]*segment)
		s.flushTimer = time.NewTimer(clearReadySendInterval)
		s.closeSignal = make(chan struct{})
		s.sendFinish = make(chan struct{})
		s.bbr = &bbr.BBR{
			Logger: s.logger,
		}
		s.rwnd = defRecWinSize
		s.cwnd = s.bbr.GetCWND()
		s.swnd = s.cwnd
		s.rto = def_rto
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
	//s.logger.Critical("swnd is",s.swnd)
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
	return NewSSegment(s, bs)
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
	return NewSSegment(s, bs)
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
		case <-time.After(closeCheckAckTO + closeCheckFlushTO):
			return errCloseTimeout
		}

	}

	return nil
}

// comRTO
func (s *SWND) comRTO(rtt time.Duration) {
	rttf := float64(rtt)
	s.rtts = time.Duration((1-rtts_a)*float64(s.rtts) + rtts_a*rttf)
	s.rttd = time.Duration((1-rttd_b)*float64(s.rttd) + rttd_b*math.Abs(rttf-float64(s.rtts)))
	s.rto = s.rtts + 4*s.rttd
	if s.rto < min_rto {
		s.rto = min_rto
	}
	if s.rto > max_rto {
		s.rto = max_rto
	}
	//s.rto=s.rtprop
	//s.logger.Critical("comRTO : rtt", rtt, "rto", s.rto)
}

// flushLastSegment
func (s *SWND) flushLastSegment() {
	ft := time.NewTimer(closeCheckFlushTO)
	defer ft.Stop()
	// send all
	for {
		select {
		case <-ft.C:
			s.logger.Error("SWND : flushLastSegment flush timeout , appBuffer len is", s.appBuffer.Len())
			return
			//case <-time.After(5 * time.Millisecond):
		default:
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
		}
	}
}

// waitLastAck
func (s *SWND) waitLastAck() {
	at := time.NewTimer(closeCheckAckTO)
	defer at.Stop()
	// recv all ack
	for {
		select {
		case <-at.C:
			s.logger.Error("SWND : waitLastAck flush timeout , sentC is", len(s.sent))
			return
		case <-time.After(10 * time.Millisecond):
			s.logger.Info("SWND : waitLastAck stopping flush , sentC is", len(s.sent))
			if len(s.sent) <= 0 {
				s.logger.Info("SWND : waitLastAck stopping flush finish , sentC is", len(s.sent))
				return
			}
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

// getTryResendSeqs
func (s *SWND) getTryResendSeqs(seq uint16) (seqs []uint16) {
	seqIndex := -1
	// sent segment seqs
	index := 0
	for sentSeq := s.hSeq; sentSeq != s.tSeq; sentSeq++ {
		seqs = append(seqs, sentSeq)
		if seq == sentSeq {
			seqIndex = index
			break
		}
		index++
	}
	if seqIndex == -1 {
		return nil // illegal ack
	}
	if seqIndex < quickResendIfSkipGEN {
		// 0 1 2
		return nil // no segment need quick resend
	}
	//s.logger.Infof("getTryResendSeqs : seq is : %v seqIndex is : %v , seqs is %v , end seqs is : %v ", seq, seqIndex, seqs, seqs[:seqIndex-quickResendIfSkipGEN+1])
	return seqs[:seqIndex-quickResendIfSkipGEN+1]
	//if _conui16(ack, seqs) { // illegal ack
	//	return nil
	//}
	//return seqs
}
