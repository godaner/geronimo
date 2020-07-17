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
}

// Recv
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
	s.sendSig <- true
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
		s.sent = &datastruct.ByteBlockChan{Size: rule.MaxWinSize}
		s.sendSig = make(chan bool)
		s.trimAckSig = make(chan bool)
		s.readySend = &datastruct.ByteBlockChan{Size: rule.MaxWinSize}
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
			d, _ := s.ackSeqCache.Load(s.headSeq)
			if d == nil {
				return
			}
			s.sent.BlockPop()
			s.ackSeqCache.Delete(s.headSeq)
			s.incSeq(&s.headSeq, 1)
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
			firstSeq := s.tailSeq
			seqs := make([]uint16, 0)
			for _, b := range bs {
				s.sent.Push(b)
				seqs = append(seqs, s.tailSeq)
				s.incSeq(&s.tailSeq, 1)
			}
			// set segment timeout
			s.setSegResend(seqs, bs)
			// send
			s.send(firstSeq, bs)

		}
	}
	go func() {
		for {
			select {
			case <-s.sendSig:
				f()
				//case <-time.After(time.Duration(250) * time.Millisecond):
				//	f()
			}
		}
	}()
}
func (s *SWND) send(firstSeq uint16, bs []byte) {
	log.Println("SWND : send seq is [", firstSeq, ",", firstSeq+uint16(len(bs))-1, "]")
	err := s.SendCallBack(firstSeq, bs)
	if err != nil {
		log.Println("SWND : send err , err is", err.Error())
	}
}

// sendAble
func (s *SWND) sendAble() (yes bool) {
	return s.sent.Len() < uint32(s.currSendWinSize)
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

// incSeq
func (s *SWND) incSeq(seq *uint16, step uint16) {
	*seq = (*seq+step)%s.maxSeq + s.minSeq
}

// decSeq
func (s *SWND) decSeq(seq *uint16, step uint16) {
	*seq = ((*seq + s.maxSeq) - step) % s.maxSeq
	// (0+8-1)%8 = 7
	// (5+8-1)%8 = 4
}

// setSegResend
func (s *SWND) setSegResend(seqs []uint16, bs []byte) {
	lastSeq := seqs[len(seqs)-1]
	firstSeq := seqs[0]
	t := time.NewTicker(time.Duration(s.rto()) * time.Millisecond)
	reSendCancel := make(chan bool, 1)
	reSendImmediately := make(chan bool, 1)
	s.segResendCancel.Store(lastSeq, reSendCancel)
	s.segResendImmediately.Store(lastSeq, reSendImmediately)

	go func() {
		defer func() {
			t.Stop()
			s.segResendCancel.Delete(lastSeq)
			s.segResendImmediately.Delete(lastSeq)
			for _, seq := range seqs {
				s.ackSeqCache.Store(seq, true)
			}
			s.trimAckSig <- true
		}()
		for {
			select {
			case <-t.C:
				s.send(firstSeq,bs)
				continue
			case <-reSendImmediately:
				s.send(firstSeq,bs)
				continue
			case <-reSendCancel:
				return
			}
		}

	}()
}

// rto
//  retry time out
func (s *SWND) rto() uint16 {
	return 1000
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
			log.Println("SWND : send win size is", s.currSendWinSize)
			time.Sleep(1000 * time.Millisecond)
		}
	}()

}
