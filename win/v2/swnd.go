package v2

import (
	"fmt"
	"github.com/godaner/geronimo/rule"
	"github.com/godaner/geronimo/win/datastruct"
	"log"
	"sync"
	"time"
)

// The Send-Window is as follow :
// sws = 5
// seq range = 0-5
//               headSeq                               tailSeq
//          	  head                                  tail                        currSendWinSize
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

type SendCallBack func(firstSeq uint16, bs []byte) (err error)

type SWND struct {
	// status
	sent            *datastruct.ByteBlockChan
	currSendWinSize uint16 // current send window size , from "receive window size" or "congestion window size"
	currRecvWinSize uint16 // current recv window size , from "recv"
	mss             uint16 // mss
	maxSeq          uint16 // max seq
	minSeq          uint16 // min seq
	headSeq         uint16 // current head seq , location is head
	tailSeq         uint16 // current tail seq , location is tail
	// outer
	SendCallBack SendCallBack
	// helper
	sync.Once
	//sendSig              chan bool // start send data to the list
	//trimAckSig           chan bool // trim ack data
	readySend            *datastruct.ByteBlockChan
	segResendCancel      *sync.Map // for cancel resend
	segResendImmediately *sync.Map // for resend
	ackNum               *sync.Map // for quick resend
	ackSeqCache          *sync.Map // for slide head to right
	cancelResendResult   *sync.Map // for ack progress
	tailSeqLock          sync.RWMutex
	headSeqLock          sync.RWMutex
	sync.RWMutex
}

// Write
func (s *SWND) Write(bs []byte) {
	s.init()
	for _, b := range bs {
		s.readySend.Push(b)
	}
	s.send()
}

// Ack
func (s *SWND) Ack(ackNs ...uint16) {
	s.init()
	s.Lock()
	defer func() {
		s.Unlock()
		s.trimAck()
		s.send()
	}()
	log.Println("SWND : recv ack is", ackNs, "recv win size is", s.currSendWinSize)
	// recv 0-3 => ack = 4
	for _, ack := range ackNs {
		m := s.quickResend(ack)
		if m {
			continue
		}
		s.ack(ack)
	}

}

// SetSendWinSize
func (s *SWND) SetSendWinSize(size uint16) {
	s.init()
	s.currRecvWinSize = size
	s.currSendWinSize = s.currRecvWinSize
}

func (s *SWND) init() {
	s.Do(func() {
		s.currSendWinSize = rule.DefWinSize
		s.currRecvWinSize = s.currSendWinSize
		s.mss = rule.MSS
		s.maxSeq = rule.MaxSeqN
		s.minSeq = rule.MinSeqN
		s.tailSeq = s.minSeq
		s.headSeq = s.tailSeq
		s.sent = &datastruct.ByteBlockChan{Size: rule.U1500}
		s.readySend = &datastruct.ByteBlockChan{Size: rule.U1500}
		s.segResendCancel = &sync.Map{}
		s.segResendImmediately = &sync.Map{}
		s.cancelResendResult = &sync.Map{}
		s.ackNum = &sync.Map{}
		s.ackSeqCache = &sync.Map{}
		s.loopPrint()
	})
}
func (s *SWND) trimAck() {
	s.Lock()
	defer s.Unlock()
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
	s.Lock()
	defer s.Unlock()
	for {
		if !s.sendAble() {
			return
		}
		bs := s.readSeg()
		if len(bs) <= 0 {
			return
		}
		// push to sent collection
		firstSeq := s.getTailSeq()
		seqs := make([]uint16, 0)
		for _, b := range bs {
			s.sent.Push(b)
			seqs = append(seqs, s.getTailSeq())
			s.incTailSeq(1)
		}
		// set segment timeout
		s.setSegResend(seqs, bs)
		// send
		s.sendCallBack("1", firstSeq, bs)

	}
}

// setSegResend
func (s *SWND) setSegResend(seqs []uint16, bs []byte) {
	lastSeq := seqs[len(seqs)-1]
	firstSeq := seqs[0]
	t := time.NewTicker(time.Duration(s.rto()) * time.Millisecond)
	reSendCancelI, _ := s.segResendCancel.LoadOrStore(lastSeq, make(chan bool))
	reSendImmediatelyI, _ := s.segResendImmediately.LoadOrStore(lastSeq, make(chan bool))
	reSendCancel := reSendCancelI.(chan bool)
	reSendImmediately := reSendImmediatelyI.(chan bool)
	log.Println("SWND : set resend progress success , seqs is ", seqs, ", lastSeq is :", lastSeq)
	go func() {
		defer func() {
			t.Stop()
			s.resetAckNum(firstSeq)
			s.segResendCancel.Delete(lastSeq)
			s.segResendImmediately.Delete(lastSeq)
			for _, seq := range seqs {
				s.ackSeqCache.Store(seq, true)
			} // cancel result
			rI, _ := s.cancelResendResult.LoadOrStore(lastSeq, make(chan bool))
			r := rI.(chan bool)
			r <- true
		}()
		for {
			select {
			case <-t.C:
				s.sendCallBack("2", firstSeq, bs)
				continue
			case <-reSendImmediately:
				s.sendCallBack("3", firstSeq, bs)
				continue
			case <-reSendCancel:
				return
			}
		}

	}()
}

// readSeg
func (s *SWND) readSeg() (bs []byte) {
	for i := 0; i < int(s.mss); i++ {
		usable, b, _ := s.readySend.Pop()
		if !usable {
			break
		}
		bs = append(bs, b)

	}
	return bs
}

func (s *SWND) sendCallBack(tag string, firstSeq uint16, bs []byte) {
	log.Println("SWND : send , tag is", tag, ", seq is [", firstSeq, ",", firstSeq+uint16(len(bs))-1, "]")
	err := s.SendCallBack(firstSeq, bs)
	if err != nil {
		log.Println("SWND : send , tag is", tag, ", err , err is", err.Error())
	}
}

// sendAble
func (s *SWND) sendAble() (yes bool) {
	sentLen, currSendWinSize := s.sent.Len(), uint32(s.currSendWinSize)
	//defer func() {
	//	log.Println("SWND : send able is", yes, ", sentLen is", sentLen, ", currSendWinSize is", currSendWinSize)
	//}()
	return sentLen < currSendWinSize
}

// incSeq
func (s *SWND) incSeq(seq *uint16, step uint16) {
	*seq = (*seq+step)%s.maxSeq + s.minSeq
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
	*seq = ((*seq + s.maxSeq) - step) % s.maxSeq
	// (0+8-1)%8 = 7
	// (5+8-1)%8 = 4
}

// rto
//  retry time out
func (s *SWND) rto() uint16 {
	return 100
}

// quickResend
func (s *SWND) quickResend(ack uint16) (match bool) {

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
			log.Println("SWND : cancel resend finish , origin ack is", originAck)
			return
		case <-time.After(1000 * time.Millisecond):
			log.Println("SWND : cancel resend timeout , origin ack is", originAck)
			panic("cancel resend timeout , origin ack is" + fmt.Sprint(originAck))
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
			log.Println("SWND : print , sent len is ", s.sent.Len(), ", readySend len is", s.readySend.Len(), ", send win size is", s.currSendWinSize)
			time.Sleep(1000 * time.Millisecond)
		}
	}()

}
