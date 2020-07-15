package v1

import (
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
const (
	windowHead = "window_head"
)

type WriterCallBack func(bs []byte) (err error)

// SWND
//  send window
type SWND struct {
	// status
	//ds     []*data
	ds     *ArrayList
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
	winLock sync.RWMutex // lock the win
}

// data
type data struct {
	b   byte
	seq uint16
}

func (d *data) String() string {
	return fmt.Sprintf("{%v:%v}", d.seq, string(d.b))
}
func (s *SWND) String() string {
	s.init()
	return fmt.Sprintf("ds is : %v , lar is : %v , lfs is : %v ,sws is : %v , mss is : %v !", s.ds.String(), s.lar, s.lfs, s.sws, s.mss)
}
func (s *SWND) Write(bs []byte) {
	s.init()
	s.ds.Append(s.bytes2dataInfs(bs)...)
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
		s.cSeq = s.minSeq
		s.lar = 0
		s.lfs = 0
		s.ds = &ArrayList{}
		s.ds.Append(windowHead) // for lar , lfs
		//s.ds = make([]*data, 0)
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
	s.rangeNotSendWindow(func(index uint16, d *data) (cti bool) {
		seqNs = append(seqNs, d.seq)
		return true
	})
	return seqNs
}

type rangeCallBack func(index uint16, d *data) (cti bool)

// rangeWindow
func (s *SWND) rangeWindow(f rangeCallBack) {
	s.ds.RangeWithStart(func(index uint32, d interface{}) (cti bool) {
		if !(int32(index) <= int32(s.lns()) && int32(index) < int32(s.ds.Len())) {
			return false
		}
		return f(uint16(index), d.(*data))
	}, uint32(s.lar+1))
}

// rangeWindowNotAck
func (s *SWND) rangeWindowNotAck(f rangeCallBack) {
	s.ds.RangeWithStart(func(index uint32, d interface{}) (cti bool) {
		if !(int32(index) <= s.lfs) {
			return false
		}
		return f(uint16(index), d.(*data))
	}, uint32(s.lar+1))
}

// rangeNotSendWindow
func (s *SWND) rangeNotSendWindow(f rangeCallBack) {
	start := s.lfs + 1
	s.ds.RangeWithStart(func(index uint32, d interface{}) (cti bool) {
		if !(index-uint32(start)+1 <= uint32(s.mss) && index < s.ds.Len() && s.notAckNum() < s.sws) {
			return false
		}
		return f(uint16(index), d.(*data))
	}, uint32(start))
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
					s.winLock.Lock()
					defer s.winLock.Unlock()
					log.Println("ackN is", ackN)
					ackN = ackN - 1
					if s.illegalAck(ackN) {
						return
					}
					log.Println("ackN2 is", ackN)
					// slide windows
					acNIndex := uint32(0)
					var acNData *data
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
						if d.seq == ackN {
							acNIndex = uint32(index)
							acNData = d
							return false
						}
						return true
					})

					// move pointer lar
					if acNData != nil {
						log.Println("slide window lar to", acNIndex)
						//  0    1    2    3    4    5    6
						// lar                      lfs
						// ackN = 2

						err := s.ds.KeepRight(acNIndex)
						if err != nil {
							log.Println("keep right window err", err)
						}
						s.lfs = s.lfs - int32(acNIndex)
						s.lar = 0
						s.ds.Set(0, windowHead)
					}
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
					s.winLock.Lock()
					defer s.winLock.Unlock()
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

// bytes2dataInfs
func (s *SWND) bytes2dataInfs(bs []byte) (ds []interface{}) {
	ds = make([]interface{}, 0)
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
