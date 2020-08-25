package win

import (
	"errors"
	"github.com/godaner/logger"
	"sync"
	"time"
)

const (
	//quickResendInterval = time.Duration(300) * time.Millisecond
	maxTickerResendC    = 3
)
const (
	incrto = 0.5
)
const (
	waitAckTo = time.Duration(10) * time.Second
)

var (
	errResendTo = errors.New("segment resend timeout")
)

const (
	EventTickerResend event = 1 << iota
	EventQResend
	EventEnd
)
const (
	EndByAck endBy = 1 << iota
	EndByResendTo
	EndByUnknown
)

type event uint8
type endBy uint8
type eContext struct {
	rttm  time.Duration
	endBy endBy
}
type eventSender func(e event, ec *eContext) (err error)

type SegmentSender func(seq uint16, bs []byte) (err error)

// segment
type segment struct {
	sync.RWMutex
	rto       time.Duration
	bs        []byte        // payload
	seq       uint16        // seq
	trsc      uint32        // ticker resend count
	qrt       *time.Timer   // quick resend timer
	qrs       chan struct{} // for quick resend
	qrsr      chan struct{} // for quick resend result
	acks      chan struct{} // for ack segment
	acksr     chan struct{} // for ack segment result
	rst, rstt *time.Timer   // resend timer ; resend timeout timer
	logger    logger.Logger
	es        eventSender
	ss        SegmentSender // ss
	acked     chan struct{} // is ack ?
	s         *SWND
}

// newSSegment
func newSSegment(s *SWND, bs []byte) (seg *segment) {
	return &segment{
		s:      s,
		rto:    s.rto,
		bs:     bs,
		seq:    s.tSeq,
		trsc:   0,
		qrs:    make(chan struct{}),
		qrsr:   make(chan struct{}),
		acks:   make(chan struct{}),
		acksr:  make(chan struct{}),
		acked:  make(chan struct{}),
		qrt:    time.NewTimer(time.Duration(1) * time.Nanosecond),
		rst:    nil,
		rstt:   nil,
		logger: s.logger,
		es: func(e event, ec *eContext) (err error) {
			return s.segmentEvent(e, ec)
		},
		ss: func(seq uint16, bs []byte) (err error) {
			s.logger.Info("segment : send , seq is [", seq, "] , rto is", s.rto)
			return s.SegmentSender(seq, bs)
		},
	}
}

// newRSegment
func newRSegment(seqN uint16, bs []byte) (rs *segment) {
	return &segment{
		seq: seqN,
		bs:  bs,
	}
}
func (s *segment) Bs() (bs []byte) {
	s.RLock()
	defer s.RUnlock()
	return s.bs
}
func (s *segment) Seq() (seq uint16) {
	s.RLock()
	defer s.RUnlock()
	return s.seq
}

func (s *segment) IsAck() (y bool) {
	s.RLock()
	defer s.RUnlock()
	return s.isAck()
}
func (s *segment) isAck() (y bool) {
	select {
	case _, ok := <-s.acked:
		return !ok
	default:
	}
	return false
}
func (s *segment) Send() (err error) {
	s.Lock()
	defer s.Unlock()
	s.setResend()
	return s.ss(s.seq, s.bs)
}

// AckIi
func (s *segment) AckIi() (err error) {
	s.Lock()
	defer s.Unlock()
	if s.isAck() {
		return
	}
	select {
	case <-s.acked:
		return
	case s.acks <- struct{}{}:
		select {
		case <-s.acksr:
			return
		case <-time.After(time.Duration(10000) * time.Millisecond):
			panic("wait ack segment result timeout")
		}
		//case <-time.After(time.Duration(10000) * time.Millisecond):
		//	panic("send ack segment signal timeout")
		//case <-time.After(time.Duration(10) * time.Millisecond):
		//	s.logger.Warning("segment#AckIi : maybe segment is acked")
		//	return errAcked
	}
}

// TryQResend
func (s *segment) TryQResend() (err error) {
	s.Lock()
	defer s.Unlock()
	if s.isAck() {
		return
	}
	select {
	case <-s.qrt.C:
		s.triggerQResend()
		//s.qrt.Reset(quickResendInterval)
		s.qrt.Reset(s.rto/3) // 300 -> 100
		return nil
	default:
		return nil
	}
}
func (s *segment) triggerQResend() {
	select {
	case <-s.acked:
		return
	case s.qrs <- struct{}{}:
		select {
		case <-s.qrsr:
			return
		case <-time.After(time.Duration(10000) * time.Millisecond):
			panic("wait quick resend segment result timeout")
		}
		//case <-time.After(time.Duration(10000) * time.Millisecond):
		//	panic("send quick resend segment signal timeout")
		//case <-time.After(time.Duration(10) * time.Millisecond):
		//	s.logger.Warning("segment#TryQResend : maybe segment is finish")
		//	return errCantQRS
	}
}

// setResend
func (s *segment) setResend() {
	rttms := time.Now()
	s.rstt = time.NewTimer(waitAckTo)
	s.rst = time.NewTimer(s.rto)
	go func() {
		endBy := EndByUnknown
		defer func() {
			s.endResend(rttms, endBy)
		}()
		for {
			select {
			case <-s.rstt.C:
				endBy = EndByResendTo
				return
			case <-s.rst.C:
				err := s.tickerResend()
				if err != nil {
					if err.Error() == errResendTo.Error() { // can't resend by ticker
						s.rst.Reset(waitAckTo * 2)
						//endBy = EndByResendTo
						continue
					}
					return
				}
				continue
			case <-s.qrs:
				err := s.quickResend()
				if err != nil {
					return
				}
				continue
			case <-s.acks:
				endBy = EndByAck
				return
			}
		}

	}()
}

// incRTO
func (s *segment) incRTO() {
	s.rto = s.rto + time.Duration(float64(s.s.rto)*incrto)
	if s.rto < min_rto {
		s.rto = min_rto
	}
	if s.rto > max_rto {
		s.rto = max_rto
	}
}

// tickerResend
func (s *segment) tickerResend() (err error) {
	s.logger.Info("segment#tickerResend : ticker resend , seq is [", s.seq, "] , rto is", s.rto)
	if maxTickerResendC < s.trsc {
		return errResendTo
	}
	s.trsc++
	s.incRTO()
	s.rst.Reset(s.rto)
	err = s.ss(s.seq, s.bs)
	if err != nil {
		s.logger.Error("segment#tickerResend : ticker resend err , seq is [", s.seq, "] , rto is", s.rto, ", err is", err.Error())
		return err
	}
	s.es(EventTickerResend, &eContext{})
	return nil
}

// quickResend
func (s *segment) quickResend() (err error) {
	defer func() {
		//s.es(EventQResend, &eContext{})
		select {
		case s.qrsr <- struct{}{}:
			return
		case <-time.After(time.Duration(10000) * time.Millisecond):
			panic("send segment quick result result signal timeout")
		}
	}()
	s.logger.Info("segment#quickResend : quick resend , seq is [", s.seq, "] , rto is", s.rto)
	s.rst.Reset(s.rto)
	err = s.ss(s.seq, s.bs)
	if err != nil {
		s.logger.Error("segment#quickResend : quick resend err , seq is [", s.seq, "] , rto is", s.rto, ", err is", err.Error())
		return err
	}
	s.es(EventQResend, &eContext{})
	return nil
}

// endResend
func (s *segment) endResend(rttms time.Time, endBy endBy) {
	s.rst.Stop()
	s.rstt.Stop()
	s.qrt.Stop()
	close(s.acked)
	s.es(EventEnd, &eContext{
		rttm: time.Now().Sub(rttms),
	})
	if endBy == EndByAck {
		select {
		case s.acksr <- struct{}{}:
			return
		case <-time.After(time.Duration(10000) * time.Millisecond):
			panic("send segment ack result signal timeout")
		}
	}
}
