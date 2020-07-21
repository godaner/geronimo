package v1

import (
	"fmt"
	"github.com/godaner/geronimo/rule"
	"github.com/godaner/geronimo/win/datastruct"
	"log"
	"math"
	"sync"
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
	max_rto = float64(3000) // ms
)

type SegmentSender func(firstSeq uint32, bs []byte) (err error)

type SWND struct {
	sync.Once
	sync.RWMutex
	// outer
	SegmentSender SegmentSender
	// status
	sent        *datastruct.ByteBlockChan
	sendWinSize int64  // send window size , from "congestion window size" or receive window size
	congWinSize int64  // congestion window size
	recvWinSize int64  // recv window size , from ack
	headSeq     uint32 // current head seq , location is head
	tailSeq     uint32 // current tail seq , location is tail
	ssthresh    int64  // ssthresh
	// helper
	readySend           *datastruct.ByteBlockChan
	segResendCancel     map[uint32]chan bool // for cancel resend
	segResendQuick      map[uint32]chan bool // for quick resend
	quickResendAckNum   map[uint32]*uint8    // for quick resend
	cancelResendResult  map[uint32]chan bool // for ack progress
	ackSeqCache         map[uint32]bool      // for slide head to right
	rtts                float64
	rttd                float64
	rto                 float64
	clearReadySendTimer *time.Timer
}

// Write
func (s *SWND) Write(bs []byte) {
	s.init()
	for _, b := range bs {
		s.readySend.BlockPush(b)
	}
	s.send(true)
}

// RecvAckSegment
func (s *SWND) RecvAckSegment(winSize uint16, ackNs ...uint32) {
	s.init()
	s.Lock()
	s.recvWinSize = int64(winSize)

	defer func() {
		s.Unlock()
		s.trimAck()
		s.send(true)
	}()
	log.Println("SWND : recv ack is", ackNs, ", recv win size is", s.recvWinSize)
	// recv 0-3 => ack = 4
	for _, ack := range ackNs {
		m := s.quickResendSegment(ack)
		if m {
			continue
		}
		s.ack(ack)
	}

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
		s.sent = &datastruct.ByteBlockChan{Size: 0}
		s.readySend = &datastruct.ByteBlockChan{Size: 0}
		s.segResendCancel = map[uint32]chan bool{}
		s.segResendQuick = map[uint32]chan bool{}
		s.cancelResendResult = map[uint32]chan bool{}
		s.quickResendAckNum = map[uint32]*uint8{}
		s.ackSeqCache = map[uint32]bool{}
		s.clearReadySendTimer = time.NewTimer(checkWinInterval)
		s.loopPrint()
		s.clearReadySend()
	})
}
func (s *SWND) trimAck() {
	s.Lock()
	defer s.Unlock()
	for {
		headSeq := s.headSeq
		_, ok := s.ackSeqCache[headSeq]
		if !ok {
			return
		}
		s.sent.BlockPop()
		delete(s.ackSeqCache, headSeq)
		s.incSeq(&s.headSeq, 1)
	}
}
func (s *SWND) send(checkMSS bool) {
	s.Lock()
	defer s.Unlock()
	for {
		ws := s.sendWinSize - int64(s.sent.Len()) // no win size to send
		if ws <= 0 {
			return
		}
		if checkMSS && s.readySend.Len() < rule.MSS { // not enough to send
			return
		}
		bs := s.readSegment(checkMSS)
		if len(bs) <= 0 {
			return
		}
		s.clearReadySendTimer.Reset(clearReadySendInterval) // reset clear ready send timeout
		// push to sent collection
		firstSeq := s.tailSeq
		for _, b := range bs {
			s.sent.BlockPush(b)
			s.incSeq(&s.tailSeq, 1)
		}
		// set segment timeout
		lastSeq := s.tailSeq
		s.decSeq(&lastSeq, 1)
		s.setSegmentResend(firstSeq, lastSeq, bs)
		// send
		s.sendCallBack("1", firstSeq, bs)

	}
}

// setSegmentResend
func (s *SWND) setSegmentResend(firstSeq, lastSeq uint32, bs []byte) {
	t := time.NewTimer(time.Duration(int64(s.getRTO())) * time.Millisecond)
	reSendCancel := make(chan bool)
	s.segResendCancel[lastSeq] = reSendCancel
	reSendQuick := make(chan bool)
	s.segResendQuick[lastSeq] = reSendQuick
	select {
	case <-reSendCancel:
		panic("fuck ! who repeat this ?")
	default:
	}

	rttms := time.Now().UnixNano() / 1e6 // ms
	log.Println("SWND : set resend progress success , seq is [", firstSeq, ",", lastSeq, "]")
	go func() {
		defer func() {
			rttme := time.Now().UnixNano()/1e6 - rttms
			if s.ssthresh <= s.congWinSize {
				s.congWinSize += rule.MSS
			} else {
				s.congWinSize *= 2 // slow start
			}
			if s.congWinSize > rule.DefRecWinSize {
				s.congWinSize = rule.DefRecWinSize
			}
			s.comSendWinSize()
			s.comRTO(float64(rttme))
			log.Println("SWND : rttm is", rttme, ", send win size is", s.sendWinSize, ", cong win size is", s.congWinSize, ", recv win size is", s.recvWinSize, ", rto is", s.getRTO())
			t.Stop()
			s.resetQuickResendAckNum(firstSeq)
			delete(s.segResendCancel, lastSeq)
			delete(s.segResendQuick, lastSeq)
			seq := firstSeq
			end := lastSeq
			s.incSeq(&end, 1)
			for {
				s.ackSeqCache[seq] = true
				s.incSeq(&seq, 1)
				if seq == end {
					break
				}
			}
			// cancel result
			s.cancelResendResult[lastSeq] <- true
		}()
		for {
			select {
			case <-t.C:
				s.ssthresh = s.congWinSize / 2
				s.congWinSize = 1 * rule.MSS
				s.comSendWinSize()
				s.incRTO()
				s.sendCallBack("2", firstSeq, bs)
				t.Reset(time.Duration(int64(s.getRTO())) * time.Millisecond)
				continue
			case <-reSendQuick:
				s.ssthresh = s.congWinSize / 2
				s.congWinSize = s.ssthresh
				s.comSendWinSize()
				s.sendCallBack("3", firstSeq, bs)
				t.Reset(time.Duration(int64(s.getRTO())) * time.Millisecond)
				continue
			case <-reSendCancel:
				return
			}
		}

	}()
}

// readSegment
func (s *SWND) readSegment(checkMSS bool) (bs []byte) {
	if checkMSS && s.readySend.Len() < rule.MSS {
		return
	}
	ws := int(s.sendWinSize - int64(s.sent.Len()))
	for i := 0; i < rule.MSS && i < ws; i++ {
		usable, b, _ := s.readySend.Pop()
		if !usable {
			break
		}
		bs = append(bs, b)

	}
	return bs
}

func (s *SWND) sendCallBack(tag string, firstSeq uint32, bs []byte) {
	log.Println("SWND : send , tag is", tag, ", seq is [", firstSeq, ",", firstSeq+uint32(len(bs))-1, "]", ", readySend len is", s.readySend.Len(), ", sent len is", s.sent.Len(), ", send win is", s.sendWinSize, ", cong win size is", s.congWinSize, ", recv win size is", s.recvWinSize, ", rto is", s.getRTO())
	err := s.SegmentSender(firstSeq, bs)
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
		num = &zero
	}
	*num++
	if *num < quickResendIfAckGEN {
		return false
	}
	ci, ok := s.segResendQuick[ack]
	if !ok { // not data wait to send
		*num = 0
		log.Println("SWND : no resend to be find , maybe recv ack before , ack is", ack)
		return false
	}
	select {
	case ci <- true:
	case <-time.After(100 * time.Millisecond):
		log.Println("SWND : no resend imm be found , ack is", ack)
	}
	log.Println("SWND : quick resend seq is", ack)
	return true
}

// resetQuickResendAckNum
func (s *SWND) resetQuickResendAckNum(ack uint32) {
	num, ok := s.quickResendAckNum[ack]
	if !ok {
		return
	}
	*num = 0
}

// ack
func (s *SWND) ack(ack uint32) {
	originAck := ack
	s.decSeq(&ack, 1)
	ci, ok := s.segResendCancel[ack]
	if !ok {
		log.Println("SWND : no be ack find , decack is", ack)
		return
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
			return
		case <-time.After(1000 * time.Millisecond):
			log.Println("SWND : cancel resend timeout , origin ack is", originAck)
			panic("cancel resend timeout , origin ack is " + fmt.Sprint(originAck))
		}
		//case <-time.After(100 * time.Millisecond):
		//	log.Println("SWND : no resend cancel be found , origin ack is", originAck)
		//	return
	}
}

// loopPrint
func (s *SWND) loopPrint() {
	go func() {
		for {
			log.Println("SWND : print , sent len is ", s.sent.Len(), ", readySend len is", s.readySend.Len(), ", send win size is", s.sendWinSize, ", recv win size is", s.recvWinSize, ", cong win size is", s.congWinSize, ", rto is", s.getRTO())
			time.Sleep(1000 * time.Millisecond)
		}
	}()

}

func (s *SWND) clearReadySend() {
	go func() {
		for {
			select {
			case <-s.clearReadySendTimer.C:
				s.send(false)
			}
		}
	}()
}
