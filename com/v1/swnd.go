package v1

import (
	"github.com/godaner/geronimo/rule"
	"log"
	"sync"
	"time"
)

// The Send-Window is as follow :
// sws = 6
//                    last ack received       last frame sent               last not send
//                         [lar)                  [lfs)                          [?) = sws-(lfs-lar)
//       --------------------|----------------------|-----------------------------|-----------------------
//          sent and ack         sent but not ack       allow send but not send       not allow send
//       0         1         2      3       4       5          6         7        8        9         10

type CallWrite func(bs []byte) (err error)

// SWND
//  send window
type SWND struct {
	sync.Once
	ds          []byte
	sws         uint16 // send window size
	lar         uint16 // last ack received
	lfs         uint16 // last frame sent
	tempDs      []byte
	acksChannel *sync.Map // map[uint16]chan uint16
	CallWrite   CallWrite
}

func (s *SWND) PrintDs() {
	s.init()
	log.Printf("PrintDs : ds is : %v , lar is : %v , lfs is : %v ,sws is : %v !", s.ds, s.lar, s.lfs, s.sws)
}
func (s *SWND) Write(bs []byte) {
	s.init()
	s.ds = append(s.ds, bs...)
	s.cacheDs()
	s.windowSend()
}
func (s *SWND) SetSWS(sws uint16) {
	s.init()
	s.sws = sws
	s.windowSend()
}
func (s *SWND) Ack(ackNs ...uint16) {
	s.init()
	for _, ackN := range ackNs {
		s.ack(ackN)
	}
	s.windowSend()
	s.ifEnableCacheDs()
}
func (s *SWND) ack(ackN uint16) {
	if s.illegalAck(ackN) {
		return
	}
	// ack channel
	ackCI, _ := s.acksChannel.LoadOrStore(ackN, make(chan uint16, 1))
	ackC, _ := ackCI.(chan uint16)
	ackC <- ackN
	// slide windows
	s.lar = ackN
}

func (s *SWND) init() {
	s.Do(func() {
		s.sws = rule.NormalWinSize
		s.ds = make([]byte, 1)
		s.tempDs = make([]byte, 1)
		s.lar = 0
		s.lfs = 0
		s.acksChannel = &sync.Map{}
	})
}
func (s *SWND) illegalAck(ackN uint16) (yes bool) {
	return ackN > s.lfs || ackN <= s.lar
}
func (s *SWND) notSendNum() (num uint16) {
	return s.sws - s.notAckNum()
}
func (s *SWND) notAckNum() (num uint16) {
	return s.lfs - s.lar
}
func (s *SWND) lns() (index uint16) {
	return s.lfs + s.notSendNum()
}

// windowSend
func (s *SWND) windowSend() {
	if s.notSendNum() <= 0 {
		return
	}
	dsLast := uint16(len(s.ds) - 1)
	lns := s.lns()
	ds := make([]byte, 0)
	for i := s.lfs + 1; i <= lns && i <= dsLast; i++ {
		ds = append(ds, s.ds[i])
		s.setTimeout(i)
	}
	err := s.CallWrite(ds)
	if err != nil {
		return
	}
	s.lfs += uint16(len(ds))
}

// setTimeout
func (s *SWND) setTimeout(index uint16) {
	go func() {
		t := time.NewTicker(time.Duration(s.rto()) * time.Millisecond)
		ackCI, _ := s.acksChannel.LoadOrStore(index, make(chan uint16, 1))
		ackC, _ := ackCI.(chan uint16)
		select {
		case <-t.C: // timeout
			s.send(index)
			return
		case <-ackC: // not timeout
			return
		}
	}()
}

// send
func (s *SWND) send(index uint16) {
	err := s.CallWrite([]byte{s.ds[index]})
	if err != nil {
		return
	}
	s.setTimeout(index)
}

// rto
//  uint is : ms
func (s *SWND) rto() (rto uint16) {
	return 5000
}

// cacheDs
func (s *SWND) cacheDs() {
	if len(s.ds) > rule.MaxWinSize {
		p1 := s.ds[:rule.MaxWinSize+1]
		p2 := s.ds[rule.MaxWinSize+1:]
		s.ds = p1
		s.tempDs = append(s.tempDs, p2...)
	}
}

// ifEnableCacheDs
func (s *SWND) ifEnableCacheDs() {
	if s.lar >= rule.MaxWinSize && len(s.ds) >= rule.MaxWinSize {
		copy(s.ds, s.tempDs)
		s.tempDs = make([]byte, 1)
		s.lar = 0
		s.lfs = 0
		s.acksChannel = &sync.Map{}
		s.cacheDs()
	}
}
