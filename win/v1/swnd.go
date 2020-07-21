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
	quickResendIfAckGEN = 3
)
const (
	rtts_a  = float64(0.125)
	rttd_b  = float64(0.25)
	def_rto = float64(1000) // ms
	def_rtt = float64(500)  // ms
	min_rto = float64(10)   // ms
	max_rto = float64(3000) // ms
)

type SegmentSender func(firstSeq uint16, bs []byte) (err error)

type SWND struct {
	// status
	sent        *datastruct.ByteBlockChan
	sendWinSize int64  // send window size , from "congestion window size" or receive window size
	congWinSize int64  // congestion window size
	recvWinSize int64  // recv window size , from ack
	headSeq     uint16 // current head seq , location is head
	tailSeq     uint16 // current tail seq , location is tail
	ssthresh    int64  // ssthresh
	// outer
	SegmentSender SegmentSender
	// helper
	sync.Once
	readySend            *datastruct.ByteBlockChan
	segResendCancel      *sync.Map // for cancel resend
	segResendImmediately *sync.Map // for resend
	ackNum               *sync.Map // for quick resend
	ackSeqCache          *sync.Map // for slide head to right
	cancelResendResult   *sync.Map // for ack progress
	tailSeqLock          sync.RWMutex
	headSeqLock          sync.RWMutex
	winLock              sync.RWMutex
	rtts                 float64
	rttd                 float64
	rto                  float64
	rtoLock              sync.RWMutex
}

// Write
func (s *SWND) Write(bs []byte) {
	s.init()
	for _, b := range bs {
		s.readySend.BlockPush(b)
	}
	s.send()
}

// RecvAckSegment
func (s *SWND) RecvAckSegment(winSize uint16, ackNs ...uint16) {
	s.init()
	s.winLock.Lock()
	s.recvWinSize = int64(winSize)

	defer func() {
		s.winLock.Unlock()
		s.trimAck()
		s.send()
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
		s.segResendCancel = &sync.Map{}
		s.segResendImmediately = &sync.Map{}
		s.cancelResendResult = &sync.Map{}
		s.ackNum = &sync.Map{}
		s.ackSeqCache = &sync.Map{}
		s.loopPrint()
	})
}
func (s *SWND) trimAck() {
	s.winLock.Lock()
	defer s.winLock.Unlock()
	for {
		headSeq := s.getHeadSeq()
		d, ok := s.ackSeqCache.Load(headSeq)
		if !ok && d == nil {
			return
		}
		s.sent.BlockPop()
		s.ackSeqCache.Delete(headSeq)
		s.incHeadSeq(1)
	}
}
func (s *SWND) send() {
	s.winLock.Lock()
	defer s.winLock.Unlock()
	for {
		ws := s.sendWinSize - int64(s.sent.Len())
		if ws <= 0 {
			return
		}
		bs := s.readSegment()
		if len(bs) <= 0 {
			return
		}
		// push to sent collection
		firstSeq := s.getTailSeq()
		for _, b := range bs {
			s.sent.BlockPush(b)
			s.incTailSeq(1)
		}
		// set segment timeout
		lastSeq:=s.getTailSeq()
		s.decSeq(&lastSeq,1)
		s.setSegmentResend(firstSeq, lastSeq, bs)
		// send
		s.sendCallBack("1", firstSeq, bs)

	}
}

// setSegmentResend
func (s *SWND) setSegmentResend(firstSeq, lastSeq uint16, bs []byte) {
	t := time.NewTimer(time.Duration(int64(s.getRTO())) * time.Millisecond)
	reSendCancelI, _ := s.segResendCancel.LoadOrStore(lastSeq, make(chan bool))
	reSendImmediatelyI, _ := s.segResendImmediately.LoadOrStore(lastSeq, make(chan bool))
	reSendCancel := reSendCancelI.(chan bool)
	reSendImmediately := reSendImmediatelyI.(chan bool)
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
				s.congWinSize += rule.MSS // slow start
			} else {
				s.congWinSize *= 2
			}
			if s.congWinSize > rule.DefRecWinSize {
				s.congWinSize = rule.DefRecWinSize
			}
			s.comSendWinSize()
			s.comRTO(float64(rttme))
			log.Println("SWND : rttm is", rttme, ", send win size is", s.sendWinSize, ", cong win size is", s.congWinSize, ", recv win size is", s.recvWinSize, ", rto is", s.getRTO())
			t.Stop()
			s.resetAckNum(firstSeq)
			s.segResendCancel.Delete(lastSeq)
			s.segResendImmediately.Delete(lastSeq)
			seq := firstSeq
			end := lastSeq
			s.incSeq(&end, 1)
			for {
				s.ackSeqCache.Store(seq, true)
				s.incSeq(&seq, 1)
				if seq == end {
					break
				}
			}
			// cancel result
			rI, _ := s.cancelResendResult.LoadOrStore(lastSeq, make(chan bool))
			r := rI.(chan bool)
			r <- true
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
			case <-reSendImmediately:
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
func (s *SWND) readSegment() (bs []byte) {
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

func (s *SWND) sendCallBack(tag string, firstSeq uint16, bs []byte) {
	log.Println("SWND : send , tag is", tag, ", seq is [", firstSeq, ",", firstSeq+uint16(len(bs))-1, "]", ", readySend len is", s.readySend.Len(), ", sent len is", s.sent.Len(), ", send win is", s.sendWinSize, ", cong win size is", s.congWinSize, ", recv win size is", s.recvWinSize, ", rto is", s.getRTO())
	err := s.SegmentSender(firstSeq, bs)
	if err != nil {
		log.Println("SWND : send , tag is", tag, ", err , err is", err.Error())
	}
}

// incSeq
func (s *SWND) incSeq(seq *uint16, step uint16) {
	*seq = (*seq+step)%rule.MaxSeqN + rule.MinSeqN
}

// incTailSeq
func (s *SWND) incTailSeq(step uint16) (tailSeq uint16) {
	s.tailSeqLock.Lock()
	defer s.tailSeqLock.Unlock()
	s.incSeq(&s.tailSeq, step)
	return s.tailSeq
}

// getTailSeq
func (s *SWND) getTailSeq() (tailSeq uint16) {
	s.tailSeqLock.RLock()
	defer s.tailSeqLock.RUnlock()
	return s.tailSeq
}

// incHeadSeq
func (s *SWND) incHeadSeq(step uint16) (headSeq uint16) {
	s.headSeqLock.Lock()
	defer s.headSeqLock.Unlock()
	s.incSeq(&s.headSeq, step)
	return s.headSeq
}

// getHeadSeq
func (s *SWND) getHeadSeq() (headSeq uint16) {
	s.headSeqLock.RLock()
	defer s.headSeqLock.RUnlock()
	return s.headSeq
}

// decSeq
func (s *SWND) decSeq(seq *uint16, step uint16) {
	//fmt.Println("seq ", *seq, "max ", rule.MaxSeqN, "min ", rule.MinSeqN)
	seq64 := uint64(*seq)
	maxSeq64 := uint64(rule.MaxSeqN)
	step64:=uint64(step)
	*seq = uint16((seq64 + maxSeq64 - step64) % maxSeq64)
	//fmt.Println("seq ", *seq, "max ", rule.MaxSeqN, "min ", rule.MinSeqN)
	// (0+8-1)%8 = 7
	// (5+8-1)%8 = 4
}

// comRTO
func (s *SWND) comRTO(rttm float64) {
	s.rtoLock.Lock()
	defer s.rtoLock.Unlock()
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
	s.rtoLock.Lock()
	defer s.rtoLock.Unlock()
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
	s.rtoLock.RLock()
	defer s.rtoLock.RUnlock()
	return s.rto
}

// quickResendSegment
func (s *SWND) quickResendSegment(ack uint16) (match bool) {

	zero := uint8(0)
	numI, _ := s.ackNum.LoadOrStore(ack, &zero)
	num := numI.(*uint8)
	*num++
	if *num < quickResendIfAckGEN {
		return false
	}
	ci, _ := s.segResendImmediately.Load(ack)
	if ci == nil { // not data wait to send
		*num = 0
		log.Println("SWND : no resend to be find , ack is", ack)
		return false
	}
	c := ci.(chan bool)
	select {
	case c <- true:
	case <-time.After(100 * time.Millisecond):
		log.Println("SWND : no resend imm be found , ack is", ack)
	}
	*num = 0
	log.Println("SWND : quick resend seq is", ack)
	return true
}

// resetAckNum
func (s *SWND) resetAckNum(ack uint16) {
	zero := uint8(0)
	numI, _ := s.ackNum.LoadOrStore(ack, &zero)
	num := numI.(*uint8)
	*num = 0
}

// ack
func (s *SWND) ack(ack uint16) {
	originAck := ack
	s.decSeq(&ack, 1)
	ci, _ := s.segResendCancel.Load(ack)
	if ci == nil {
		log.Println("SWND : no be ack find , decack is", ack)
		return
	}
	// cancel result
	rI, _ := s.cancelResendResult.LoadOrStore(ack, make(chan bool))
	r := rI.(chan bool)

	c := ci.(chan bool)
	select {
	case c <- true:
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
