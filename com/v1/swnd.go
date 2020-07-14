package v1

import (
	"encoding/json"
	"fmt"
	"github.com/godaner/geronimo/rule"
	"log"
	"sync"
	"time"
)

// The Send-Window is as follow :
// sws = 5
// seq range = 0-5
//
//                    last ack received       last frame sent       last not send
//                         [lar)                  [lfs)             [lns) = sws-(lfs-lar)
//       --------------------|----------------------|--------------------|--------------------------------
//          sent and ack        sent but not ack    allow send but not send       not allow send
// seq = 0         1         2      3       4       5          0         1        2        3         4
//
// index=0         1         2      3       4       5          6         7        8        9         10
type WriterCallBack func(bs []byte) (err error)

// SWND
//  send window
type SWND struct {
	// status
	ds     []*data
	sws    uint16 // send window size
	mss    uint16 // mss
	lar    int32  // last ack received
	lfs    int32  // last frame sent
	maxSeq uint16 // max seq
	minSeq uint16 // min seq
	cSeq   uint16 // current seq
	// outer
	WriterCallBack WriterCallBack
	// helper
	sendWindowSignal chan bool
	ackSignal        chan uint16
	ackChannel       *sync.Map // map[uint16]chan uint16
	sync.Once
}

// data
type data struct {
	b   byte
	seq uint16
}

func (s *SWND) String() string {
	s.init()
	dsm := make([]map[string]interface{}, 0)
	for _, d := range s.ds {
		dsm = append(dsm, map[string]interface{}{
			"seq": d.seq,
			"b":   d.b,
		})
	}
	dsms, _ := json.Marshal(dsm)
	return fmt.Sprintf("ds is : %v , lar is : %v , lfs is : %v ,sws is : %v , mss is : %v !", string(dsms), s.lar, s.lfs, s.sws, s.mss)
}
func (s *SWND) Write(bs []byte) {
	s.init()
	ds := s.bytes2datas(bs)
	s.ds = append(s.ds, ds...)
	s.sendWindowSignal <- true
}
func (s *SWND) SetSWS(sws uint16) {
	s.init()
	s.sws = sws
	s.sendWindowSignal <- true
}
func (s *SWND) Ack(ackNs ...uint16) {
	s.init()
	for _, ackN := range ackNs {
		s.ackSignal <- ackN
	}
}

func (s *SWND) init() {
	s.Do(func() {
		s.sws = rule.DefWinSize
		s.mss = rule.MSS
		s.maxSeq = rule.MaxSeqN
		s.minSeq = rule.MinSeqN
		s.lar = -1
		s.lfs = -1
		s.ds = make([]*data, 0)
		s.ackChannel = &sync.Map{}
		s.sendWindowSignal = make(chan bool)
		s.ackSignal = make(chan uint16)
		s.sendWindow()
		s.recvAck()
	})
}

// incSeq
func (s *SWND) incSeq(seq *uint16, step uint16) {
	*seq = (*seq + step) % s.maxSeq
}

// readWindowSegment
//  split window data to segment by mss
func (s *SWND) readWindowSegment() (seqNs []uint16) {
	start := int(s.lfs + 1)
	for i := start; i-start+1 <= int(s.mss) && i < len(s.ds) && s.notAckNum() < s.sws; i++ {
		seqNs = append(seqNs, s.ds[i].seq)
	}
	return seqNs
}

type rangeCallBack func(index uint16, d *data) (cti bool)

// rangeWindow
func (s *SWND) rangeWindow(f rangeCallBack) {
	for i := s.lar + 1; i <= int32(s.lns()) && i < int32(len(s.ds)); i++ {
		cti := f(uint16(i), s.ds[i])
		if !cti {
			return
		}
	}
}

// rangeWindow
func (s *SWND) rangeWindowNotAck(f rangeCallBack) {
	for i := s.lar + 1; i <= s.lfs; i++ {
		cti := f(uint16(i), s.ds[i])
		if !cti {
			return
		}
	}
}

// containUint16
func (s *SWND) containUint16(tar uint16, src []uint16) (yes bool) {
	for _, sr := range src {
		if tar == sr {
			return true
		}
	}
	return false
}

// recvAck
func (s *SWND) recvAck() {
	go func() {
		for {
			select {
			case ackN := <-s.ackSignal:
				func() {
					log.Println("ackN is", ackN)
					ackN = ackN - 1
					if s.illegalAck(ackN) {
						return
					}
					log.Println("ackN2 is", ackN)
					// slide windows
					s.rangeWindow(func(index uint16, d *data) (cti bool) {
						if d.seq > ackN {
							return false
						}
						// cancel ack timeout
						ackCI, ok := s.ackChannel.Load(d.seq)
						if ok && ackCI != nil {
							ackC, ok := ackCI.(chan uint16)
							if ok && ackC != nil {
								ackC <- ackN
								s.ackChannel.Delete(d.seq)
								log.Println("cancel ack timeout", d.seq)
							}
						}
						// move lar
						if d.seq == ackN {
							log.Println("slide window lar to", index)
							s.ds = s.ds[index+1:]
							s.lfs = s.lfs - int32(index) - 1
							s.lar = -1
						}
						return true
					})
					s.sendWindowSignal <- true
				}()
			}

		}
	}()
}

// sendWindow
func (s *SWND) sendWindow() {
	go func() {
		for {
			select {
			case <-s.sendWindowSignal:
				func() {
					for { // send segment one by one
						seqNs := s.readWindowSegment()
						if len(seqNs) <= 0 {
							return
						}
						s.sendSegment(seqNs...)
						s.lfs += int32(len(seqNs))
					}
				}()
			}

		}
	}()
}
func (s *SWND) illegalAck(ackN uint16) (yes bool) {
	index := s.getAckNIndex(ackN)
	return int32(index) > s.lfs || int32(index) <= s.lar
}
func (s *SWND) getAckNIndex(ackN uint16) (index uint16) {
	s.rangeWindow(func(i uint16, d *data) (cti bool) {
		if d.seq == ackN {
			index = i
			return false
		}
		return true
	})
	return index
}
func (s *SWND) notSendNum() (num uint16) {
	return s.sws - s.notAckNum()
}
func (s *SWND) notAckNum() (num uint16) {
	return uint16(s.lfs - s.lar)
}
func (s *SWND) lns() (index uint16) {
	return uint16(s.lfs + int32(s.notSendNum()))
}

// setSegmentTimeout
//  set timeout in last seq number
func (s *SWND) setSegmentTimeout(seqNs ...uint16) {
	go func() {
		t := time.NewTicker(time.Duration(s.rto()) * time.Millisecond)
		log.Println("set timeout with seq", seqNs[len(seqNs)-1])
		ackCI, _ := s.ackChannel.LoadOrStore(seqNs[len(seqNs)-1], make(chan uint16, 1))
		ackC, _ := ackCI.(chan uint16)
		select {
		case <-t.C: // timeout
			s.sendSegment(seqNs...)
			return
		case <-ackC: // not timeout
			return
		}
	}()
}

// sendSegment
func (s *SWND) sendSegment(seqNs ...uint16) {
	ds := s.getDataBySeqNs(seqNs...)
	err := s.WriterCallBack(s.datas2bytes(ds))
	if err != nil {
		return
	}
	s.setSegmentTimeout(seqNs...)
}

// getDataBySeqNs
func (s *SWND) getDataBySeqNs(seqNs ...uint16) (ds []*data) {
	s.rangeWindow(func(index uint16, d *data) (cti bool) {
		if s.containUint16(d.seq, seqNs) {
			ds = append(ds, d)
		}
		return true
	})
	return ds
}

// data2bytes
func (s *SWND) datas2bytes(ds []*data) (bs []byte) {
	for _, d := range ds {
		bs = append(bs, d.b)
	}
	return bs
}

// datasSeqNs
func (s *SWND) datasSeqNs(ds []*data) (seqNs []uint16) {
	for _, d := range ds {
		seqNs = append(seqNs, d.seq)
	}
	return seqNs
}

// bytes2datas
func (s *SWND) bytes2datas(bs []byte) (ds []*data) {
	ds = make([]*data, 0)
	for _, b := range bs {
		ds = append(ds, &data{
			b:   b,
			seq: s.cSeq,
		})
		s.incSeq(&s.cSeq, 1)
	}
	return ds
}

// rto
//  uint is : ms
func (s *SWND) rto() (rto uint16) {
	return 5000
}
