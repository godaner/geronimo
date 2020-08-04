package v1

import (
	"errors"
	"fmt"
	"github.com/godaner/geronimo/logger"
	gologging "github.com/godaner/geronimo/logger/go-logging"
	"github.com/godaner/geronimo/rule"
	"github.com/godaner/geronimo/win/ds"
	"math"
	"sync"
	"sync/atomic"
	"time"
)

// The Send-Window is as follow :
// sws = 5
// seq range = 0-5
//               headSeq                               tailSeq
//          	  head                                  tail                        congWinSize
// list    <<------|-------|-------|------|-------|-------|-------|-------|-------|--------|--------|-----<< data flow
//                 |                                      |                                |
// sent and ack<===|==========>sent but not ack<==========|====>allow send but not send<===|===>not allow send
//                 |                                      |                                |
// seq =           0       1       2      3       4       5       0       1        2       3       4
//
// index =         0       1       2      3       4       5       6       7        8       9       10
const (
	readySendMaxSIze           = 10 // n mss
	quickResendIfAckGEN        = 3
	clearReadySendInterval     = 100                  // ms
	clearReadySendLongInterval = 1000 * 60 * 60       // ms
	closeCheckFlushInterval    = 10                   // ms
	closeCheckFlushNum         = readySendMaxSIze * 2 // should ge bq size
	closeCheckFlushTO          = 5 * 1000             // ms
	closeCheckAckTO            = 5 * 1000             // ms
	closeCheckAckInterval      = 10                   // ms
	closeCheckAckNum           = readySendMaxSIze * 2
	closeTimeout               = 1000 * 5 // ms
)
const (
	rtts_a  = float64(0.125)
	rttd_b  = float64(0.25)
	def_rto = float64(100 * 1e6) // ns
	def_rtt = float64(100 * 1e6) // ns
	min_rto = float64(10 * 1e6)  // ns
	max_rto = float64(500 * 1e6) // ns
)

var (
	ErrSWNDClosed       = errors.New("swnd closed")
	ErrSWNDCloseTimeout = errors.New("swnd close timeout")
)

type SegmentSender func(seq uint32, bs []byte) (err error)

type SWND struct {
	sync.Once
	sync.RWMutex
	SegmentSender      SegmentSender
	sentC              int64
	ackSeqCache        *sync.Map            // map[uint32]bool      // for slide head to right
	readySend          *ds.BQ               // from app , cache data , wait for send
	segResendCancel    *sync.Map            // map[uint32]chan bool // for cancel resend
	segResendQuick     *sync.Map            // map[uint32]chan bool // for quick resend
	quickResendAckNum  *sync.Map            // map[uint32]*uint8    // for quick resend
	cancelResendResult map[uint32]chan bool // for ack progress
	quickResendResult  map[uint32]chan bool // for ack progress
	flushTimer         *time.Timer          // loopFlush not send data
	sendWinSize        int64                // send window size , from "congestion window size" or receive window size
	congWinSize        int64                // congestion window size
	recvWinSize        int64                // not recv window size
	headSeq            uint32               // current head seq , location is head
	tailSeq            uint32               // current tail seq , location is tail
	ssthresh           int64                // ssthresh
	rttd, rtts, rto    float64              // ns
	closeSignal        chan bool
	sendFinish         chan bool
	logger             logger.Logger
	FTag               string
}

// Write
func (s *SWND) Write(bs []byte) (err error) {
	s.init()
	select {
	case <-s.closeSignal:
		s.logger.Warning("SWND : window is closed")
		return ErrSWNDClosed
	default:
	}
	s.readySend.Push(bs...)
	s.Lock()
	defer s.Unlock()
	return s.send(true)
}

// RecvAckSegment
func (s *SWND) RecvAckSegment(winSize uint16, ack uint32) (err error) {
	s.init()
	//select {
	//case <-s.closeSignal:
	//	return ErrSWNDClosed
	//default:
	//
	//}
	//s.recvWinSize = rule.DefRecWinSize // todo
	//s.comSendWinSize()
	//s.recvWinSize = int64(s.sendAbleNum(winSize, ack)) + s.sentC
	//s.comSendWinSize()
	s.Lock()
	defer func() {
		s.trimAck()
		err = s.send(true)
		s.Unlock()
	}()
	s.logger.Debug("SWND : recv ack is [", ack, "] , readySend len is", s.readySend.Len(), ", sent len is", s.sentC, ", send win size is", s.sendWinSize, ", cong win size is", s.congWinSize, ", recv win size is", s.recvWinSize)
	// recv 0-3 => ack = 4
	m := s.ack(ack)
	if m {
		return
	}
	s.quickResendSegment(ack)
	return nil
}

// comSendWinSize
func (s *SWND) comSendWinSize() {
	s.sendWinSize = int64(math.Min(float64(s.recvWinSize), float64(s.congWinSize)))
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
		s.ssthresh = rule.DefSsthresh
		s.recvWinSize = rule.DefRecWinSize
		s.congWinSize = rule.DefCongWinSize
		s.sendWinSize = s.congWinSize
		s.tailSeq = rule.MinSeqN
		s.headSeq = s.tailSeq
		s.rto = def_rto
		s.rtts = def_rtt
		s.rttd = def_rtt
		s.readySend = &ds.BQ{Size: readySendMaxSIze}
		s.segResendCancel = &sync.Map{}
		s.segResendQuick = &sync.Map{}
		s.cancelResendResult = map[uint32]chan bool{}
		s.quickResendResult = map[uint32]chan bool{}
		s.quickResendAckNum = &sync.Map{}
		s.ackSeqCache = &sync.Map{}
		s.flushTimer = time.NewTimer(time.Duration(clearReadySendInterval) * time.Millisecond)
		s.closeSignal = make(chan bool)
		s.closeSignal = make(chan bool)
		s.sendFinish = make(chan bool)
		//s.loopPrint()
		s.loopFlush()
	})
}
func (s *SWND) trimAck() {
	defer func() {
		s.logger.Debug("SWND : trimAck , sent len is", s.sentC)
	}()
	for {
		_, ok := s.ackSeqCache.Load(s.headSeq)
		if !ok {
			return
		}
		s.ackSeqCache.Delete(s.headSeq)
		s.incSeq(&s.headSeq, 1)
		atomic.AddInt64(&s.sentC, -1)
	}
}

// sendAbleNum
func (s *SWND) sendAbleNum(recvWinSize uint16, ack uint32) (num uint16) {
	inNetPkgNum := s.seqSpace(s.tailSeq, ack, rule.MaxSeqN)
	return uint16(uint32(recvWinSize) - inNetPkgNum)
}

// seqSpace
//  bSeq ----->-------->--- aSeq
/*
	0-7
	x (5) , n (6 7 0 1 2 3 4 5)
		x >=n
		x-n
		x < n
		x+8-n
*/
func (s *SWND) seqSpace(aSeq, bSeq, maxSeq uint32) (ss uint32) {
	if aSeq >= bSeq {
		return aSeq - bSeq
	}
	return aSeq + maxSeq - bSeq
}

// send
func (s *SWND) send(checkMSS bool) (err error) {
	for {
		bs, needFlush := s.readASegment(checkMSS)
		if needFlush {
			s.flushTimer.Reset(time.Duration(clearReadySendInterval) * time.Millisecond) // flush after n ms
			s.logger.Debug("SWND : need flush , readySend len is", s.readySend.Len(), ", sent len is", s.sentC, ", send win size is", s.sendWinSize, ", cong win size is", s.congWinSize, ", recv win size is", s.recvWinSize, ", rto is", int64(s.rto))
		} else {
			s.flushTimer.Reset(time.Duration(clearReadySendLongInterval) * time.Millisecond) // no flush
		}
		if len(bs) <= 0 {
			// todo ???????
			s.logger.Debug("SWND : read segment fail , readySend len is", s.readySend.Len(), ", sent len is", s.sentC, ", send win size is", s.sendWinSize, ", cong win size is", s.congWinSize, ", recv win size is", s.recvWinSize, ", rto is", int64(s.rto))
			return
		}
		// add sent count
		atomic.AddInt64(&s.sentC, 1)
		// set segment timeout
		s.setSegmentResend(s.tailSeq, bs)
		// send
		err = s.sendCallBack("1", s.tailSeq, bs)
		if err != nil {
			return err
		}
		// inc seq
		s.incSeq(&s.tailSeq, 1)

	}
}

// setSegmentResend
func (s *SWND) setSegmentResend(seq uint32, bs []byte) {
	t := time.NewTimer(time.Duration(int64(s.rto)) * time.Nanosecond)
	reSendCancel := make(chan bool)
	s.segResendCancel.Store(seq, reSendCancel)
	reSendQuick := make(chan bool)
	s.segResendQuick.Store(seq, reSendQuick)
	select {
	case <-reSendCancel:
		panic("fuck ! who repeat this ?")
	default:
	}

	rttms := time.Now().UnixNano() // ms
	s.logger.Debug("SWND : set resend progress success , seq is", seq)
	go func() {
		defer func() {
			rttme := time.Now().UnixNano() - rttms
			//fmt.Println(rttme)
			if s.ssthresh <= s.congWinSize {
				s.congWinSize += 1
			} else {
				s.congWinSize *= 2 // slow start
			}
			if s.congWinSize > rule.DefRecWinSize {
				s.congWinSize = rule.DefRecWinSize
			}
			s.comSendWinSize()
			s.comRTO(float64(rttme))
			s.logger.Debug("SWND : rttm is", rttme, ", readySend len is", s.readySend.Len(), ", sent len is", s.sentC, ", send win size is", s.sendWinSize, ", cong win size is", s.congWinSize, ", recv win size is", s.recvWinSize, ", rto is", int64(s.rto))
			t.Stop()
			s.resetQuickResendAckNum(seq)
			s.segResendCancel.Delete(seq)
			s.segResendQuick.Delete(seq)

			s.ackSeqCache.Store(seq, true)
			// cancel result
			s.cancelResendResult[seq] <- true
		}()
		for {
			select {
			case <-t.C:
				s.ssthresh = s.congWinSize / 2
				s.congWinSize = 1
				s.comSendWinSize()
				s.incRTO()
				err := s.sendCallBack("2", seq, bs)
				if err != nil {
					s.logger.Error("SWND : 1 resend get send err", err)
					return
				}
				s.resetQuickResendAckNum(seq)
				t.Reset(time.Duration(int64(s.rto)) * time.Nanosecond)
				continue
			case <-reSendQuick:
				s.ssthresh = s.congWinSize / 2
				s.congWinSize = s.ssthresh
				s.comSendWinSize()
				err := s.sendCallBack("3", seq, bs)
				if err != nil {
					s.logger.Error("SWND : 2 resend get send err", err)
					return
				}
				s.resetQuickResendAckNum(seq)
				t.Reset(time.Duration(int64(s.rto)) * time.Nanosecond)
				// quick resend result
				s.quickResendResult[seq] <- true
				continue
			case <-reSendCancel:
				return
			case <-s.sendFinish:
				s.logger.Error("SWND : send finish , cancel resend")
				return

			}
		}

	}()
}

// readASegment
func (s *SWND) readASegment(checkMSS bool) (bs []byte, needFlush bool) {
	if s.sendWinSize-s.sentC <= 0 { // no enough win size to send todo
		return nil, false
	}
	if checkMSS && s.readySend.Len() < rule.MSS { // no enough data to fill a mss
		if s.readySend.Len() > 0 {
			//s.logger.Warning("SWND : readASegment , need flush , readySend len is", s.readySend.Len())
			return nil, true
		}
		return nil, false
	}
	bs = make([]byte, rule.MSS, rule.MSS)
	n := s.readySend.Pop(bs) // todo ?? blockpop ??
	return bs[:n], false
}

func (s *SWND) sendCallBack(tag string, seq uint32, bs []byte) (err error) {
	go func() {
		s.logger.Debug("SWND : send , tag is", tag, ", seq is [", seq, "] , readySend len is", s.readySend.Len(), ", sent len is", s.sentC, ", send win is", s.sendWinSize, ", cong win size is", s.congWinSize, ", recv win size is", s.recvWinSize, ", rto is", int64(s.rto))
		err = s.SegmentSender(seq, bs)
		if err != nil {
			s.logger.Error("SWND : send , tag is", tag, ", err , err is", err.Error())
		}
		//return err
	}()
	return
}

// incSeq
func (s *SWND) incSeq(seq *uint32, step uint16) {
	*seq = (*seq+uint32(step))%rule.MaxSeqN + rule.MinSeqN
}

// decSeq
func (s *SWND) decSeq(seq *uint32, step uint16) {
	//fmt.Println("seq ", *seq, "max ", rule.MaxSeqN, "min ", rule.MinSeqN)

	*seq = (*seq + rule.MaxSeqN - uint32(step)) % rule.MaxSeqN
	//fmt.Println("seq ", *seq, "max ", rule.MaxSeqN, "min ", rule.MinSeqN)
	// (0+8-1)%8 = 7
	// (5+8-1)%8 = 4
}

// comRTO
func (s *SWND) comRTO(rttm float64) {
	s.rtts = (1-rtts_a)*s.rtts + rtts_a*rttm
	s.rttd = (1-rttd_b)*s.rttd + rttd_b*math.Abs(rttm-s.rtts)
	s.rto = s.rtts + 4*s.rttd
	if s.rto < min_rto {
		s.rto = min_rto
	}
	if s.rto > max_rto {
		s.rto = max_rto
	}
}

// incRTO
func (s *SWND) incRTO() {
	s.rto = 2 * s.rto
	if s.rto < min_rto {
		s.rto = min_rto
	}
	if s.rto > max_rto {
		s.rto = max_rto
	}
}

// quickResendSegment
func (s *SWND) quickResendSegment(ack uint32) (match bool) {
	zero := uint8(0)
	//num, ok := s.quickResendAckNum[ack]
	numI, ok := s.quickResendAckNum.Load(ack)
	num, _ := numI.(*uint8)
	if !ok {
		s.logger.Debug("SWND : first trigger quick resend , ack is", ack)
		num = &zero
		//s.quickResendAckNum[ack] = num
		s.quickResendAckNum.Store(ack, num)
	}
	*num++
	if *num < quickResendIfAckGEN {
		s.logger.Debug("SWND : com quick resend ack num , ack is", ack, ", num is", *num)
		return false
	}
	s.logger.Debug("SWND : com quick resend ack num , ack is", ack, ", num is", *num)
	cii, ok := s.segResendQuick.Load(ack)
	//ci, ok := s.segResendQuick[ack]
	if !ok { // not data wait to send
		*num = 0
		s.logger.Debug("SWND : no resend to be find , maybe recv ack before , ack is", ack)
		return false
	}
	ci := cii.(chan bool)
	r := make(chan bool)
	s.quickResendResult[ack] = r

	select {
	case ci <- true:
		s.logger.Debug("SWND : find quick resend , ack is", ack)
		select {
		case <-r:
			s.logger.Debug("SWND : quick resend ack is", ack)
		case <-time.After(1000 * time.Millisecond):
			s.logger.Error("SWND : quick resend timeout , ack is", ack)
			panic("quick resend timeout , ack is " + fmt.Sprint(ack))

		}
	case <-time.After(100 * time.Millisecond):
		s.logger.Error("SWND : no resend imm be found , ack is", ack)
	}
	return true
}

// resetQuickResendAckNum
func (s *SWND) resetQuickResendAckNum(ack uint32) {
	numI, ok := s.quickResendAckNum.Load(ack)
	if !ok {
		return
	}
	num := numI.(*uint8)
	*num = 0
}

// ack
func (s *SWND) ack(ack uint32) (match bool) {
	originAck := ack
	s.decSeq(&ack, 1)
	//ci, ok := s.segResendCancel[ack]
	cii, ok := s.segResendCancel.Load(ack)
	if !ok {
		s.logger.Warning("SWND : repeat ack , origin ack is", originAck, " , decack is", ack)
		return false
	}
	ci := cii.(chan bool)
	// cancel result
	r := make(chan bool)
	s.cancelResendResult[ack] = r

	select {
	case ci <- true:
		s.logger.Debug("SWND : find resend cancel , origin ack is", originAck)
		select {
		case <-r:
			s.logger.Debug("SWND : cancel resend finish , origin ack is", originAck, ", rto is", int64(s.rto))
			return true
		case <-time.After(1000 * time.Millisecond):
			s.logger.Error("SWND : cancel resend timeout , origin ack is", originAck)
			panic("cancel resend timeout , origin ack is " + fmt.Sprint(originAck))
		}
		//case <-time.After(100 * time.Millisecond):
		//	s.logger.Debug("SWND : no resend cancel be found , origin ack is", originAck)
		//	return
	}
	return true
}

// loopPrint
func (s *SWND) loopPrint() {
	go func() {
		for {
			select {
			case <-s.closeSignal:
				return
			default:
				s.logger.Notice("SWND : print , sent len is ", s.sentC, ", readySend len is", s.readySend.Len(), ", sent len is", s.sentC, ", send win size is", s.sendWinSize, ", recv win size is", s.recvWinSize, ", cong win size is", s.congWinSize, ", rto is", int64(s.rto))
				time.Sleep(1000 * time.Millisecond)
			}

		}
	}()

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
				s.flushLast()
				s.recvAckLast()
				return
			case <-s.flushTimer.C:
				s.Lock()
				s.logger.Debug("SWND : flushing , readySend len is", s.readySend.Len(), ", sent len is", s.sentC, ", send win size is", s.sendWinSize, ", recv win size is", s.recvWinSize, ", cong win size is", s.congWinSize, ", rto is", int64(s.rto))
				err := s.send(false)
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
		case <-time.After(time.Duration(closeTimeout) * time.Millisecond):
			return ErrSWNDCloseTimeout
		}

	}

	return nil
}

// flushLast
func (s *SWND) flushLast() {
	ft := time.NewTimer(time.Duration(closeCheckFlushTO) * time.Millisecond)
	defer ft.Stop()
	// send all
	sn := 0
	for {
		select {
		case <-ft.C:
			s.logger.Error("SWND : flushLast flush timeout , flush num is", sn, ", readySend len is", s.readySend.Len())
			panic("flush timeout")
			return
		case <-time.After(time.Duration(closeCheckFlushInterval) * time.Millisecond):
			if sn > closeCheckFlushNum {
				s.logger.Error("SWND : flushLast stopping flush , flush num beynd , flush num is", sn, ", readySend len is", s.readySend.Len())
				return
			}
			s.Lock()
			s.logger.Debug("SWND : flushLast stopping flush , ready send is", s.readySend.Len())
			if s.readySend.Len() <= 0 {
				s.logger.Debug("SWND : flushLast stopping flush finish , ready send is", s.readySend.Len())
				s.Unlock()
				return
			}
			err := s.send(false) // clear data
			if err != nil {
				s.logger.Error("SWND : flushLast stopping flush err , ready send is", s.readySend.Len(), ", err is", err)
				s.Unlock()
				return
			}
			s.Unlock()
			sn++

		}
	}
}

// recvAckLast
func (s *SWND) recvAckLast() {
	at := time.NewTimer(time.Duration(closeCheckAckTO) * time.Millisecond)
	defer at.Stop()
	// recv all ack
	an := 0
	for {
		select {
		case <-at.C:
			s.logger.Error("SWND : recvAckLast flush timeout , ack num is", an, ", sentC is", s.sentC)
			panic("flush timeout")
			return
		case <-time.After(time.Duration(closeCheckAckInterval) * time.Millisecond):
			if an > closeCheckAckNum {
				s.logger.Error("SWND : recvAckLast stopping flush , ack num beynd , ack num is", an)
				return
			}
			s.logger.Debug("SWND : recvAckLast stopping flush , sentC is", s.sentC)
			if s.sentC <= 0 {
				s.logger.Debug("SWND : recvAckLast stopping flush finish , sentC is", s.sentC)
				return
			}
			an++
		}

	}
}
