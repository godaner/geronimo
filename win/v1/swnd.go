package v1

import (
	"fmt"
	"github.com/godaner/geronimo/rule"
	"github.com/godaner/geronimo/win/ds"
	"log"
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
	quickResendIfAckGEN    = 3
	clearReadySendInterval = 100
)
const (
	rtts_a  = float64(0.125)
	rttd_b  = float64(0.25)
	def_rto = float64(1000) // ms
	def_rtt = float64(500)  // ms
	min_rto = float64(1)    // ms
	max_rto = float64(500)  // ms
)

type SegmentSender func(seq uint32, bs []byte) (err error)

type SWND struct {
	sync.Once
	sync.RWMutex
	SegmentSender         SegmentSender
	sentC                 int64
	ackSeqCache           map[uint32]bool      // for slide head to right
	readySend             *ds.ByteBlockChan    // from app , cache data , wait for send
	segResendCancel       map[uint32]chan bool // for cancel resend
	segResendQuick        map[uint32]chan bool // for quick resend
	quickResendAckNum     map[uint32]*uint8    // for quick resend
	quickResendAckNumLock sync.RWMutex         // for quickResendAckNum
	cancelResendResult    map[uint32]chan bool // for ack progress
	quickResendResult     map[uint32]chan bool // for ack progress
	flushTimer            *time.Timer          // loopFlush not send data
	sendWinSize           int64                // send window size , from "congestion window size" or receive window size
	congWinSize           int64                // congestion window size
	recvWinSize           int64                // not recv window size
	headSeq               uint32               // current head seq , location is head
	tailSeq               uint32               // current tail seq , location is tail
	ssthresh              int64                // ssthresh
	rtts                  float64
	rttd                  float64
	rto                   float64
}

// Write
func (s *SWND) Write(bs []byte) {
	s.init()
	s.readySend.BlockPushs(bs...)
	s.send(true)
}

// RecvAckSegment
func (s *SWND) RecvAckSegment(winSize uint16, ack uint32) {
	s.init()
	s.Lock()
	s.recvWinSize = rule.DefRecWinSize // todo
	s.comSendWinSize()
	//s.recvWinSize = int64(s.sendAbleNum(winSize, ack)) + s.sentC
	//s.recvWinSize = int64(winSize) // todo
	defer func() {
		s.Unlock()
		s.trimAck()
		s.send(true)
	}()
	log.Println("SWND : recv ack is [", ack, "] , readySend len is", s.readySend.Len(), ", sent len is", s.sentC, ", send win size is", s.sendWinSize, ", cong win size is", s.congWinSize, ", recv win size is", s.recvWinSize)
	// recv 0-3 => ack = 4
	m := s.ack(ack)
	if m {
		return
	}
	s.quickResendSegment(ack)

}

// comSendWinSize
func (s *SWND) comSendWinSize() {
	s.sendWinSize = int64(math.Min(float64(s.recvWinSize), float64(s.congWinSize)))
}

func (s *SWND) init() {
	s.Do(func() {
		s.ssthresh = rule.DefSsthresh
		s.congWinSize = rule.DefCongWinSize
		s.sendWinSize = s.congWinSize
		s.tailSeq = rule.MinSeqN
		s.headSeq = s.tailSeq
		s.rto = def_rto
		s.rtts = def_rtt
		s.rttd = def_rtt
		//s.sent = &ds.ByteBlockChan{Size: 0}
		s.readySend = &ds.ByteBlockChan{Size: 0}
		s.segResendCancel = map[uint32]chan bool{}
		s.segResendQuick = map[uint32]chan bool{}
		s.cancelResendResult = map[uint32]chan bool{}
		s.quickResendResult = map[uint32]chan bool{}
		s.quickResendAckNum = map[uint32]*uint8{}
		s.ackSeqCache = map[uint32]bool{}
		s.flushTimer = time.NewTimer(checkWinInterval)
		s.loopPrint()
		s.loopFlush()
	})
}
func (s *SWND) trimAck() {
	s.Lock()
	defer func() {
		log.Println("SWND : trimAck , sent len is", s.sentC)
		s.Unlock()
	}()
	for {
		_, ok := s.ackSeqCache[s.headSeq]
		if !ok {
			return
		}
		delete(s.ackSeqCache, s.headSeq)
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
func (s *SWND) send(checkMSS bool) {
	s.Lock()
	defer s.Unlock()
	for {
		bs := s.readASegment(checkMSS)
		if len(bs) <= 0 {
			return
		}
		s.flushTimer.Reset(clearReadySendInterval) // reset clear ready send timeout
		// add sent count
		atomic.AddInt64(&s.sentC, 1)
		// set segment timeout
		s.setSegmentResend(s.tailSeq, bs)
		// send
		s.sendCallBack("1", s.tailSeq, bs)
		// inc seq
		s.incSeq(&s.tailSeq, 1)

	}
}

// setSegmentResend
func (s *SWND) setSegmentResend(seq uint32, bs []byte) {
	t := time.NewTimer(time.Duration(int64(s.getRTO())) * time.Millisecond)
	reSendCancel := make(chan bool)
	s.segResendCancel[seq] = reSendCancel
	reSendQuick := make(chan bool)
	s.segResendQuick[seq] = reSendQuick
	select {
	case <-reSendCancel:
		panic("fuck ! who repeat this ?")
	default:
	}

	rttms := time.Now().UnixNano() / 1e6 // ms
	log.Println("SWND : set resend progress success , seq is", seq)
	go func() {
		defer func() {
			rttme := time.Now().UnixNano()/1e6 - rttms
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
			log.Println("SWND : rttm is", rttme, ", readySend len is", s.readySend.Len(), ", sent len is", s.sentC, ", send win size is", s.sendWinSize, ", cong win size is", s.congWinSize, ", recv win size is", s.recvWinSize, ", rto is", s.getRTO())
			t.Stop()
			s.resetQuickResendAckNum(seq)
			delete(s.segResendCancel, seq)
			delete(s.segResendQuick, seq)

			s.ackSeqCache[seq] = true
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
				s.sendCallBack("2", seq, bs)
				s.resetQuickResendAckNum(seq)
				t.Reset(time.Duration(int64(s.getRTO())) * time.Millisecond)
				continue
			case <-reSendQuick:
				s.ssthresh = s.congWinSize / 2
				s.congWinSize = s.ssthresh
				s.comSendWinSize()
				s.sendCallBack("3", seq, bs)
				s.resetQuickResendAckNum(seq)
				t.Reset(time.Duration(int64(s.getRTO())) * time.Millisecond)
				// quick resend result
				s.quickResendResult[seq] <- true
				continue
			case <-reSendCancel:
				return
			}
		}

	}()
}

// readASegment
func (s *SWND) readASegment(checkMSS bool) (bs []byte) {
	if s.sendWinSize-s.sentC <= 0 { // no enough win size to send todo
		return
	}
	if checkMSS && s.readySend.Len() < rule.MSS { // not enough data to fill a mss
		return
	}
	for i := 0; i < rule.MSS; i++ {
		usable, b, _ := s.readySend.Pop()
		if !usable {
			break
		}
		bs = append(bs, b)
	}
	return bs
}

func (s *SWND) sendCallBack(tag string, seq uint32, bs []byte) {
	log.Println("SWND : send , tag is", tag, ", seq is [", seq, "] , readySend len is", s.readySend.Len(), ", sent len is", s.sentC, ", send win is", s.sendWinSize, ", cong win size is", s.congWinSize, ", recv win size is", s.recvWinSize, ", rto is", s.getRTO())
	err := s.SegmentSender(seq, bs)
	if err != nil {
		log.Println("SWND : send , tag is", tag, ", err , err is", err.Error())
	}
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

// getRTO
func (s *SWND) getRTO() (rto float64) {
	return s.rto
}

// quickResendSegment
func (s *SWND) quickResendSegment(ack uint32) (match bool) {
	zero := uint8(0)
	num, ok := s.quickResendAckNum[ack]
	if !ok {
		log.Println("SWND : first trigger quick resend , ack is", ack)
		num = &zero
		s.quickResendAckNum[ack] = num
	}
	*num++
	if *num < quickResendIfAckGEN {
		log.Println("SWND : com quick resend ack num , ack is", ack, ", num is", *num)
		return false
	}
	log.Println("SWND : com quick resend ack num , ack is", ack, ", num is", *num)
	ci, ok := s.segResendQuick[ack]
	if !ok { // not data wait to send
		*num = 0
		log.Println("SWND : no resend to be find , maybe recv ack before , ack is", ack)
		return false
	}

	r := make(chan bool)
	s.quickResendResult[ack] = r

	select {
	case ci <- true:
		log.Println("SWND : find quick resend , ack is", ack)
		select {
		case <-r:
			log.Println("SWND : quick resend ack is", ack)
		case <-time.After(1000 * time.Millisecond):
			log.Println("SWND : quick resend timeout , ack is", ack)
			panic("quick resend timeout , ack is " + fmt.Sprint(ack))

		}
	case <-time.After(100 * time.Millisecond):
		log.Println("SWND : no resend imm be found , ack is", ack)
	}
	return true
}

// resetQuickResendAckNum
func (s *SWND) resetQuickResendAckNum(ack uint32) {
	s.quickResendAckNumLock.Lock()
	defer s.quickResendAckNumLock.Unlock()
	num, ok := s.quickResendAckNum[ack]
	if !ok {
		return
	}
	*num = 0
}

// ack
func (s *SWND) ack(ack uint32) (match bool) {
	originAck := ack
	s.decSeq(&ack, 1)
	ci, ok := s.segResendCancel[ack]
	if !ok {
		log.Println("SWND : no be ack find , decack is", ack)
		return false
	}
	// cancel result
	r := make(chan bool)
	s.cancelResendResult[ack] = r

	select {
	case ci <- true:
		log.Println("SWND : find resend cancel , origin ack is", originAck)
		select {
		case <-r:
			log.Println("SWND : cancel resend finish , origin ack is", originAck, ", rto is", s.getRTO())
			return true
		case <-time.After(1000 * time.Millisecond):
			log.Println("SWND : cancel resend timeout , origin ack is", originAck)
			panic("cancel resend timeout , origin ack is " + fmt.Sprint(originAck))
		}
		//case <-time.After(100 * time.Millisecond):
		//	log.Println("SWND : no resend cancel be found , origin ack is", originAck)
		//	return
	}
	return true
}

// loopPrint
func (s *SWND) loopPrint() {
	go func() {
		for {
			log.Println("SWND : print , sent len is ", s.sentC, ", readySend len is", s.readySend.Len(), ", sent len is", s.sentC, ", send win size is", s.sendWinSize, ", recv win size is", s.recvWinSize, ", cong win size is", s.congWinSize, ", rto is", s.getRTO())
			time.Sleep(1000 * time.Millisecond)
		}
	}()

}

// loopFlush
func (s *SWND) loopFlush() {
	go func() {
		for {
			select {
			case <-s.flushTimer.C:
				s.send(false)
			}
		}
	}()
}
