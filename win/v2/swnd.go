package v2

import (
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

type WriterCallBack func(firstSeq uint16, bs []byte) (err error)

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
	SendCallBack WriterCallBack
	// helper
	sync.Once
	sendSig              chan bool // start send data to the list
	trimAckSig           chan bool // trim ack data
	readySend            *datastruct.ByteBlockChan
	segResendCancel      *sync.Map // for cancel resend
	segResendImmediately *sync.Map // for resend
	ackNum               *sync.Map // for quick resend
	ackSeqCache          *sync.Map // for slide head to right
	tailSeqLock          sync.RWMutex
	headSeqLock          sync.RWMutex
}

// RecvSegment
func (s *SWND) Write(bs []byte) {
	s.init()
	for _, b := range bs {
		s.readySend.Push(b)
	}
	s.sendSig <- true
}

// SetSendWinSize
func (s *SWND) SetSendWinSize(size uint16) {
	s.init()
	s.currRecvWinSize = size
	s.currSendWinSize = s.currRecvWinSize
}

// Ack
func (s *SWND) Ack(ackNs ...uint16) {
	s.init()
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
		s.sendSig = make(chan bool)
		s.trimAckSig = make(chan bool)
		s.readySend = &datastruct.ByteBlockChan{Size: rule.U1500}
		s.segResendCancel = &sync.Map{}
		s.segResendImmediately = &sync.Map{}
		s.ackNum = &sync.Map{}
		s.ackSeqCache = &sync.Map{}
		s.loopSend()
		s.loopTrimAck()
		s.loopPrint()
	})
}

// loopTrimAck
func (s *SWND) loopTrimAck() {
	f := func() {
		defer func() {
			s.sendSig <- true
		}()
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
	go func() {
		for {
			select {
			case <-s.trimAckSig:
				f()
				//case <-time.After(time.Duration(250) * time.Millisecond):
				//	f()
			}
		}
	}()
}

// loopSend
func (s *SWND) loopSend() {
	f := func() {
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
			s.send("1", firstSeq, bs)

		}
	}
	go func() {
		for {
			select {
			case <-s.sendSig:
				f()
			case <-time.After(time.Duration(250) * time.Millisecond):
				f()
			}
		}
	}()
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
	go func() {
		defer func() {
			t.Stop()
			s.resetAckNum(firstSeq)
			//s.segResendCancel.Delete(lastSeq)
			//s.segResendImmediately.Delete(lastSeq)
			for _, seq := range seqs {
				s.ackSeqCache.Store(seq, true)
			}
			s.trimAckSig <- true
		}()
		for {
			select {
			case <-t.C:
				s.send("2", firstSeq, bs)
				continue
			case <-reSendImmediately:
				s.send("3", firstSeq, bs)
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

func (s *SWND) send(tag string, firstSeq uint16, bs []byte) {
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

// incTailSeq
func (s *SWND) incTailSeq(step uint16) (tailSeq uint16) {
	s.tailSeqLock.Lock()
	defer s.tailSeqLock.Unlock()
	s.tailSeq = (s.tailSeq+step)%s.maxSeq + s.minSeq
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
	s.headSeq = (s.headSeq+step)%s.maxSeq + s.minSeq
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
		return false
	}
	c := ci.(chan bool)
	c <- true
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
	s.decSeq(&ack, 1)
	ci, _ := s.segResendCancel.Load(ack)
	if ci == nil {
		return
	}
	c := ci.(chan bool)
	c <- true
}

// loopPrint
func (s *SWND) loopPrint() {
	go func() {
		for {
			log.Println("SWND : print , send win size is", s.currSendWinSize)
			time.Sleep(1000 * time.Millisecond)
		}
	}()

}
