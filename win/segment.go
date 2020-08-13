package win

import (
	"errors"
	"github.com/godaner/geronimo/logger"
	"sync"
	"time"
)

const (
	quickResendIfAckGEN = 3
	maxResendC          = 10
)

var (
	ErrSegmentCantQRS             = errors.New("segment can't quick resend")
	ErrSegmentCantFin             = errors.New("segment can't ack")
	ErrSegmentResendBeyondMaxTime = errors.New("segment resend beyond max time")
)

const (
	EventResend event = 1 << iota
	EventQResend
	EventFinish
)

type event uint8
type eContext struct {
	rttm time.Duration
}
type eventSender func(e event, ec *eContext) (err error)

type SegmentSender func(seq uint16, bs []byte) (err error)

// segment
type segment struct {
	sync.RWMutex
	rto      time.Duration // time.Duration
	overBose bool
	bs       []byte        // payload
	seq      uint16        // seq
	rsc      uint32        // resend count
	ackc     uint16        // ack count , for quick resend
	acked    bool          // is ack ?
	qrs      chan struct{} // for quick resend
	qrsr     chan struct{} // for quick resend result
	fs       chan struct{} // for ack segment
	fsr      chan struct{} // for ack segment result
	t        *time.Timer
	logger   logger.Logger
	es       eventSender
	ss       SegmentSender // ss
}

// newSSegment
func newSSegment(logger logger.Logger, overBose bool, seq uint16, bs []byte, rto time.Duration, es eventSender, sender SegmentSender) (s *segment) {
	return &segment{
		rto:    rto,
		bs:     bs,
		seq:    seq,
		rsc:    0,
		ackc:   0,
		qrs:    make(chan struct{}),
		qrsr:   make(chan struct{}),
		fs:     make(chan struct{}),
		fsr:    make(chan struct{}),
		t:      nil,
		logger: logger,
		es:     es,
		ss: func(seq uint16, bs []byte) (err error) {
			s.logger.Info("segment : send , seq is [", seq, "] , rto is", s.rto)
			return sender(seq, bs)
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
	return s.acked
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
	if s.acked {
		return
	}
	select {
	case <-s.fs: // ack
		return ErrSegmentCantFin
	case s.fs <- struct{}{}:
		select {
		case <-s.fsr:
			return
		case <-time.After(time.Duration(10000) * time.Millisecond):
			panic("wait ack segment result timeout")
			//return errors.New("wait ack segment result timeout")
		}
	case <-time.After(time.Duration(10000) * time.Millisecond):
		panic("send ack segment signal timeout")
		//return errors.New("send ack segment signal timeout")
	}
}

// TryQResend
func (s *segment) TryQResend() (err error) {
	s.Lock()
	defer s.Unlock()
	if s.acked {
		return
	}
	if s.rsc >= quickResendIfAckGEN {
		s.rsc = 0
		select {
		case <-s.qrs:
			return ErrSegmentCantQRS
		case s.qrs <- struct{}{}:
			select {
			case <-s.qrsr:
				return
			case <-time.After(time.Duration(10000) * time.Millisecond):
				panic("wait quick resend segment result timeout")
			}
		case <-time.After(time.Duration(10000) * time.Millisecond):
			panic("Send quick resend segment signal timeout")
		}
	} else {
		s.rsc++
	}
	return nil
}

// setResend
func (s *segment) setResend() {
	s.t = time.NewTimer(s.rto)
	rttms := time.Now()
	select {
	case <-s.fs:
		panic("fuck ! who repeat this ?")
	default:
	}
	go func() {
		defer func() {
			s.t.Stop()
			close(s.qrs)
			close(s.fs)
			s.acked = true
			s.es(EventFinish, &eContext{
				rttm: time.Now().Sub(rttms),
			})
			s.fsr <- struct{}{}
		}()
		for {
			select {
			case <-s.t.C:
				s.logger.Info("segment : ticker resend , seq is [", s.seq, "] , rto is", s.rto)
				//s.Lock() todo
				err := s.resend()
				if err != nil {
					//s.Unlock()
					return
				}
				s.es(EventResend, &eContext{})
				//s.Unlock()
				continue
			case <-s.qrs:
				s.logger.Info("segment : quick resend , seq is [", s.seq, "] , rto is", s.rto)
				err := s.resend()
				if err != nil {
					return
				}
				s.es(EventQResend, &eContext{})
				s.qrsr <- struct{}{}
				continue
			case <-s.fs:
				return
			}
		}

	}()
}
func (s *segment) resend() (err error) {
	if maxResendC < s.rsc {
		return ErrSegmentResendBeyondMaxTime
	}
	s.rsc++
	s.incRTO()
	err = s.ss(s.seq, s.bs)
	if err != nil {
		return err
	}
	s.t.Reset(s.rto)
	return nil
}

// incRTO
func (s *segment) incRTO() {
	s.rto = 2 * s.rto
	switch s.overBose {
	case true:
		if s.rto < ob_min_rto {
			s.rto = ob_min_rto
		}
		if s.rto > ob_max_rto {
			s.rto = ob_max_rto
		}
	case false:
		if s.rto < min_rto {
			s.rto = min_rto
		}
		if s.rto > max_rto {
			s.rto = max_rto
		}
	}
}
