package geronimo

import (
	"errors"
	"github.com/godaner/logger"
	"time"
)

const (
	//quickResendInterval = time.Duration(300) * time.Millisecond
	maxTickerResendC = 15
)
const (
	incrto = 0.5
)
const (
	waitAckTo = time.Duration(10) * time.Second
)

var (
	errResendTo = errors.New("resend timeout")
)

const (
	EventTickerResend Event = 1 << iota
	EventQResend
	EventEnd
)
const (
	EndByAck endBy = 1 << iota
	EndByResendTo
	EndByUnknown
)

type Event uint8
type endBy uint8
type EContext struct {
	Seg *Segment
}
type eventSender func(e Event, ec *EContext) (err error)

type Sender func(seq uint16, bs []byte) (err error)

// Segment
type Segment struct {
	//sync.RWMutex
	rto           time.Duration
	bs            []byte        // payload
	seq           uint16        // seq
	trsc          uint32        // ticker resend count
	qrt           *time.Timer   // quick resend timer
	qrs           chan struct{} // for quick resend
	qrsr          chan struct{} // for quick resend result
	acks          chan struct{} // for ack Segment
	acksr         chan struct{} // for ack Segment result
	rst, rstt     *time.Timer   // resend timer ; resend timeout timer
	logger        logger.Logger
	es            eventSender
	ss            Sender        // ss
	acked         chan struct{} // is ack ?
	s             *SWND
	rtts, rtte    time.Time
	rtt           time.Duration
	endBy         endBy
	delivered     int64
	deliveredTime time.Time
}

// NewSSegment
func NewSSegment(s *SWND, bs []byte) (seg *Segment) {
	logger := s.Logger()
	return &Segment{
		s:             s,
		rto:           s.RTO(),
		bs:            bs,
		seq:           s.TSeq(),
		trsc:          0,
		qrs:           make(chan struct{}),
		qrsr:          make(chan struct{}),
		acks:          make(chan struct{}),
		acksr:         make(chan struct{}),
		acked:         make(chan struct{}),
		qrt:           time.NewTimer(time.Duration(1) * time.Nanosecond),
		rst:           nil,
		rstt:          nil,
		delivered:     s.BBR().Delivered(),// newest ack num
		deliveredTime: s.BBR().DeliveredTime(), // newest ack time
		//deliveredTime: time.Now(),
		logger: logger,
		es: func(e Event, ec *EContext) (err error) {
			return s.SegmentEvent(e, ec)
		},
		ss: func(seq uint16, bs []byte) (err error) {
			logger.Info("Segment : send , seq is [", seq, "] , rto is", s.RTO())
			return s.SegmentSender(seq, bs)
		},
	}
}

// NewRSegment
func NewRSegment(seqN uint16, bs []byte) (rs *Segment) {
	return &Segment{
		seq: seqN,
		bs:  bs,
	}
}
func (s *Segment) Delivered() (d int64) {
	return s.delivered
}
func (s *Segment) RTT() (rtt time.Duration) {
	//s.RLock()
	//defer s.RUnlock()
	return s.rtt
}
func (s *Segment) Bs() (bs []byte) {
	//s.RLock()
	//defer s.RUnlock()
	return s.bs
}
func (s *Segment) RTTS() (rtts time.Time) {
	//s.RLock()
	//defer s.RUnlock()
	return s.rtts
}
func (s *Segment) DeliveredTime() (deliveredTime time.Time) {
	return s.deliveredTime
}

func (s *Segment) RTTE() (rtte time.Time) {
	//s.RLock()
	//defer s.RUnlock()
	return s.rtte
}
func (s *Segment) Seq() (seq uint16) {
	//s.RLock()
	//defer s.RUnlock()
	return s.seq
}

func (s *Segment) IsAck() (y bool) {
	//s.RLock()
	//defer s.RUnlock()
	return s.isAck()
}
func (s *Segment) isAck() (y bool) {
	select {
	case _, ok := <-s.acked:
		return !ok
	default:
	}
	return false
}
func (s *Segment) Send() (err error) {
	//s.Lock()
	//defer s.Unlock()
	s.setResend()
	return s.ss(s.seq, s.bs)
}

// AckIi
func (s *Segment) AckIi() (err error) {
	//s.Lock()
	//defer s.Unlock()
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
			panic("wait ack Segment result timeout")
			//s.logger.Warning("Segment : wait ack Segment result timeout , maybe resend be stop by other reason")
			return nil
		}
		//case <-time.After(time.Duration(10000) * time.Millisecond):
		//	panic("send ack Segment signal timeout")
		//case <-time.After(time.Duration(10) * time.Millisecond):
		//	s.logger.Warning("Segment#AckIi : maybe Segment is acked")
		//	return errAcked
	}
}

// TryQResend
func (s *Segment) TryQResend() (err error) {
	//s.Lock()
	//defer s.Unlock()
	if s.isAck() {
		return
	}
	select {
	case <-s.qrt.C:
		s.triggerQResend()
		//s.qrt.Reset(quickResendInterval)
		s.qrt.Reset(s.s.RTO() / 4)
		return nil
	default:
		return nil
	}
}
func (s *Segment) triggerQResend() {
	select {
	case <-s.acked:
		return
	case s.qrs <- struct{}{}:
		select {
		case <-s.qrsr:
			return
		case <-time.After(time.Duration(10000) * time.Millisecond):
			panic("wait quick resend Segment result timeout")
		}
		//case <-time.After(time.Duration(10000) * time.Millisecond):
		//	panic("send quick resend Segment signal timeout")
		//case <-time.After(time.Duration(10) * time.Millisecond):
		//	s.logger.Warning("Segment#TryQResend : maybe Segment is finish")
		//	return errCantQRS
	}
}

// setResend
func (s *Segment) setResend() {
	s.rtts = time.Now()
	s.rstt = time.NewTimer(waitAckTo)
	s.rst = time.NewTimer(s.rto)
	go func() {
		s.endBy = EndByUnknown
		defer func() {
			s.endResend()
		}()
		for {
			select {
			case <-s.rstt.C:
				s.endBy = EndByResendTo
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
				s.endBy = EndByAck
				return
			}
		}

	}()
}

// incRTO
func (s *Segment) incRTO() {
	s.rto = time.Duration(float64(s.rto) + float64(s.s.RTO())*incrto)
	if s.rto < Min_rto {
		s.rto = Min_rto
	}
	if s.rto > Max_rto {
		s.rto = Max_rto
	}
}

// tickerResend
func (s *Segment) tickerResend() (err error) {
	s.logger.Info("Segment#tickerResend : ticker resend , seq is [", s.seq, "] , rto is", s.rto)
	if maxTickerResendC < s.trsc {
		return errResendTo
	}
	s.trsc++
	s.incRTO()
	s.rst.Reset(s.rto)
	err = s.ss(s.seq, s.bs)
	if err != nil {
		s.logger.Error("Segment#tickerResend : ticker resend err , seq is [", s.seq, "] , rto is", s.rto, ", err is", err.Error())
		return err
	}
	s.es(EventTickerResend, &EContext{})
	return nil
}

// quickResend
func (s *Segment) quickResend() (err error) {
	defer func() {
		//s.es(EventQResend, &EContext{})
		select {
		case s.qrsr <- struct{}{}:
			return
		case <-time.After(time.Duration(10000) * time.Millisecond):
			panic("send Segment quick result result signal timeout")
		}
	}()
	s.logger.Info("Segment#quickResend : quick resend , seq is [", s.seq, "] , rto is", s.rto)
	s.rst.Reset(s.rto)
	err = s.ss(s.seq, s.bs)
	if err != nil {
		s.logger.Error("Segment#quickResend : quick resend err , seq is [", s.seq, "] , rto is", s.rto, ", err is", err.Error())
		return err
	}
	s.es(EventQResend, &EContext{})
	return nil
}

// endResend
func (s *Segment) endResend() {
	s.rtte = time.Now()
	s.rtt = s.rtte.Sub(s.rtts)
	s.rst.Stop()
	s.rstt.Stop()
	s.qrt.Stop()
	close(s.acked)
	s.es(EventEnd, &EContext{
		Seg: s,
	})
	if s.endBy == EndByAck {
		select {
		case s.acksr <- struct{}{}:
			return
		case <-time.After(time.Duration(10000) * time.Millisecond):
			panic("send Segment ack result signal timeout")
		}
	}
}
