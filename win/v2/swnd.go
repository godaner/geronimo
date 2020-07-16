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
	WriterCallBack WriterCallBack
	// helper
	sync.Once
	sendSig              chan bool // start send data to the list
	readySend            *datastruct.ByteBlockChan
	segResendCancel      *sync.Map
	segResendImmediately *sync.Map
	ackNum               *sync.Map
}

// Write
func (s *SWND) Write(bs []byte) {
	s.init()
	for _, b := range bs {
		s.readySend.Push(b)
	}
	s.sendSig <- true
}

// SetRecvSendWinSize
func (s *SWND) SetRecvSendWinSize(size uint16) {
	s.init()
	s.currRecvWinSize = size
	s.currSendWinSize = s.currRecvWinSize
}

// Ack
func (s *SWND) Ack(ackNs ...uint16) {
	s.init()
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
		s.sent = &datastruct.ByteBlockChan{Size: rule.MaxWinSize}
		s.sendSig = make(chan bool)
		s.readySend = &datastruct.ByteBlockChan{Size: rule.MaxWinSize}
		s.segResendCancel = &sync.Map{}
		s.segResendImmediately = &sync.Map{}
		s.ackNum = &sync.Map{}
		s.send()
	})
}

// send
func (s *SWND) send() {
	go func() {
		for {
			select {
			case <-s.sendSig:
				func() {
					for {
						if !s.sendAble() {
							return
						}
						bs := s.readSeg()
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
						// write
						err := s.WriterCallBack(firstSeq, bs)
						if err != nil {
							log.Println("writer call back err , err is", err.Error())
						}
					}
				}()
			}
		}
	}()
}

// sendAble
func (s *SWND) sendAble() (yes bool) {
	return s.sent.Len()+s.readySend.Len() < uint32(s.currSendWinSize)
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
		for {
			select {
			case <-t.C:
				// write
				err := s.WriterCallBack(firstSeq, bs)
				if err != nil {
					log.Println("ticker retry writer call back err , err is", err.Error())
				}
				continue
			case <-reSendImmediately:
				// write
				err := s.WriterCallBack(firstSeq, bs)
				if err != nil {
					log.Println("immediately retry writer call back err , err is", err.Error())
				}
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
	return 200
}

// quickResend
func (s *SWND) quickResend(ack uint16) (match bool) {
	zero := uint8(0)
	numI, _ := s.ackNum.LoadOrStore(ack, &zero)
	num := numI.(*uint8)
	if *num < quickResendIfAckGEN {
		return false
	}
	ci, _ := s.segResendImmediately.Load(ack)
	c := ci.(chan bool)
	c <- true
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
