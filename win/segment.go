package win

import (
	"errors"
	"github.com/godaner/logger"
	"sync"
	"time"
)

const (
	quickResendIfAckGEN   = 3
	obQuickResendIfAckGEN = 3
	maxResendC            = 10
	obMaxResendC          = 8
)
const (
	obincrto = 2
	incrto   = 2
)
const (
	firstSendC   = 1
	obFirstSendC = 3
)

var (
	errResendTo = errors.New("segment resend timeout")
)

const (
	EventResend event = 1 << iota
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
	rto      time.Duration // time.Duration
	overBose bool
	bs       []byte        // payload
	seq      uint16        // seq
	rsc      uint32        // resend count
	ackc     uint8         // ack count , for quick resend
	qrs      chan struct{} // for quick resend
	qrsr     chan struct{} // for quick resend result
	acks     chan struct{} // for ack segment
	acksr    chan struct{} // for ack segment result
	t        *time.Timer
	logger   logger.Logger
	es       eventSender
	ss       SegmentSender // ss
	acked    chan struct{} // is ack ?
}

// newSSegment
func newSSegment(logger logger.Logger, overBose bool, seq uint16, bs []byte, rto time.Duration, es eventSender, sender SegmentSender) (s *segment) {
	return &segment{
		overBose: overBose,
		rto:      rto,
		bs:       bs,
		seq:      seq,
		rsc:      0,
		ackc:     0,
		qrs:      make(chan struct{}),
		qrsr:     make(chan struct{}),
		acks:     make(chan struct{}),
		acksr:    make(chan struct{}),
		acked:    make(chan struct{}),
		t:        nil,
		logger:   logger,
		es: func(e event, ec *eContext) (err error) {
			if es != nil {
				return es(e, ec)
			}
			return nil
		},
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
	fs := firstSendC
	switch s.overBose {
	case true:
		fs = obFirstSendC
	}
	// send n time
	for i := 0; i < fs; i++ {
		err = s.ss(s.seq, s.bs)
		if err != nil {
			return err
		}
	}
	return nil
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
	switch s.overBose {
	case true:
		if s.ackc >= obQuickResendIfAckGEN {
			s.ackc = 0
			s.triggerQResend()
		} else {
			s.ackc++
		}

	case false:
		if s.ackc >= quickResendIfAckGEN {
			s.ackc = 0
			s.triggerQResend()
		} else {
			s.ackc++
		}
	}
	return nil
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
	s.t = time.NewTimer(s.rto)
	rttms := time.Now()
	go func() {
		endBy := EndByUnknown
		defer func() {
			s.endResend(rttms, endBy)
		}()
		for {
			select {
			//case <-s.acked:
			//	return
			case <-s.t.C:
				err := s.tickerResend()
				if err != nil {
					if err == errResendTo {
						endBy = EndByResendTo
					}
					return
				}
				continue
			case <-s.qrs:
				err := s.quickResend()
				if err != nil {
					if err == errResendTo {
						endBy = EndByResendTo
					}
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
func (s *segment) resend() (err error) {
	switch s.overBose {
	case true:
		if obMaxResendC < s.rsc {
			return errResendTo
		}
	case false:
		if maxResendC < s.rsc {
			return errResendTo
		}
	}
	s.rsc++
	s.incRTO()
	s.t.Reset(s.rto)
	err = s.ss(s.seq, s.bs)
	if err != nil {
		return err
	}
	return nil
}

// incRTO
func (s *segment) incRTO() {
	switch s.overBose {
	case true:
		s.rto = time.Duration(obincrto * float64(s.rto))
		if s.rto < ob_min_rto {
			s.rto = ob_min_rto
		}
		if s.rto > ob_max_rto {
			s.rto = ob_max_rto
		}
	case false:
		s.rto = time.Duration(incrto * float64(s.rto))
		if s.rto < min_rto {
			s.rto = min_rto
		}
		if s.rto > max_rto {
			s.rto = max_rto
		}
	}
}

// tickerResend
func (s *segment) tickerResend() (err error) {
	//defer func() {
	//	s.es(EventResend, &eContext{})
	//}()
	s.logger.Info("segment#tickerResend : ticker resend , seq is [", s.seq, "] , rto is", s.rto)
	err = s.resend()
	if err != nil {
		s.logger.Error("segment#tickerResend : ticker resend err , seq is [", s.seq, "] , rto is", s.rto, ", err is", err.Error())
		return err
	}
	s.es(EventResend, &eContext{})
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
	err = s.resend()
	if err != nil {
		s.logger.Error("segment#quickResend : quick resend err , seq is [", s.seq, "] , rto is", s.rto, ", err is", err.Error())
		return err
	}
	s.es(EventQResend, &eContext{})
	return nil
}

// endResend
func (s *segment) endResend(rttms time.Time, endBy endBy) {
	s.t.Stop()
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
