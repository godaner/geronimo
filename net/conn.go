package net

import (
	"errors"
	"fmt"
	"github.com/godaner/geronimo/rule"
	v1 "github.com/godaner/geronimo/rule/v1"
	v12 "github.com/godaner/geronimo/win/v1"
	"log"
	"math/rand"
	"net"
	"strconv"
	"sync"
	"time"
)

const (
	udpmss        = 1472
	syn1Timeout   = 2000
	syn2Timeout   = 2000
	syn1RetryTime = 4
	syn2RetryTime = 4
)
const (
	_                 = iota
	StatusListen      = iota
	StatusSynSent     = iota
	StatusSynRecved   = iota
	StatusEstablished = iota
	StatusFinWait1    = iota
	StatusCloseWait   = iota
	StatusFinWait2    = iota
	StatusLastAck     = iota
	StatusTimeWait    = iota
	StatusClosed      = iota
)

const (
	_       = iota
	FDial   = iota
	FListen = iota
)
const msl = 60 * 2

type Status uint16

func (s Status) String() string {
	return fmt.Sprint(uint16(s))
}

type GConn struct {
	sync.Once
	*net.UDPConn
	f                                              uint8
	s                                              Status
	recvWin                                        *v12.RWND
	sendWin                                        *v12.SWND
	raddr                                          *GAddr
	laddr                                          *GAddr
	lis                                            *GListener
	synSeqX, synSeqY, fin1SeqU, fin1SeqV, fin2SeqW uint32
	syn1RetryTime, syn2RetryTime                   uint8
	syn1Finish, syn2Finish                         chan bool
	closeFinish                                    chan bool
	mhs                                            map[uint16]messageHandler
	closeSignal                                    chan bool
}

func (g *GConn) init() {
	g.Do(func() {
		g.syn1Finish = make(chan bool)
		g.syn2Finish = make(chan bool)
		g.closeFinish = make(chan bool)
		g.closeSignal = make(chan bool)
		// init message handlers
		g.mhs = map[uint16]messageHandler{
			// syn
			rule.FlagSYN1: g.syn1MessageHandler,
			rule.FlagSYN2: g.syn2MessageHandler,
			rule.FlagSYN3: g.syn3MessageHandler,
			// fin
			rule.FlagFIN1: g.fin1MessageHandler,
			rule.FlagFIN2: g.fin2MessageHandler,
			rule.FlagFIN3: g.fin3MessageHandler,
			rule.FlagFIN4: g.fin4MessageHandler,
			// body
			rule.FlagPAYLOAD: g.payloadMessageHandler,
			// ack
			rule.FlagACK: g.ackMessageHandler,
		}
		g.recvWin = &v12.RWND{
			AckSender: func(ack uint32, receiveWinSize uint16) (err error) {
				m := &v1.Message{}
				m.ACK(ack, receiveWinSize)
				return g.sendMessage(m)
			},
		}
		g.sendWin = &v12.SWND{
			SegmentSender: func(seq uint32, bs []byte) (err error) {
				// send udp
				m := &v1.Message{}
				m.PAYLOAD(seq, bs)
				return g.sendMessage(m)
			},
		}
		if g.f == FDial {
			go func() {
				// recv udp
				bs := make([]byte, udpmss, udpmss)
				for {
					select {
					case <-g.closeSignal:
						return
					default:
						func() {
							n, err := g.UDPConn.Read(bs)
							if err != nil {
								log.Println("GConn#init : Read err", err)
								return
							}
							m := &v1.Message{}
							m.UnMarshall(bs[:n])
							g.handleMessage(m)
						}()
					}

				}
			}()
			return
		}

	})
}

// handleMessage
func (g *GConn) handleMessage(m *v1.Message) {
	g.init()
	mh, ok := g.mhs[m.Flag()]
	if ok && mh != nil {
		mh(m)
		return
	}
	log.Println("GConn#handleMessage : no message handler be found , conn status is", g.s, ", flag is", strconv.FormatUint(uint64(m.Flag()), 2))
	panic("no handler")
}

// sendMessage
func (g *GConn) sendMessage(m *v1.Message) (err error) {
	b := m.Marshall()
	if g.f == FDial {
		_, err = g.UDPConn.Write(b)
		//log.Println("GConn : udp from ", g.UDPConn.LocalAddr().String(), " to", g.UDPConn.RemoteAddr().String())
		return err
	}
	if g.f == FListen {
		_, err = g.UDPConn.WriteToUDP(b, g.raddr.toUDPAddr())
		//log.Println("GConn : udp from ", g.UDPConn.LocalAddr().String(), " to", g.raddr.toUDPAddr().String())
		return err
	}
	return nil
}

// rmFromLis
func (g *GConn) rmFromLis() {
	if g.lis == nil {
		return
	}
	g.lis.gcs.Delete(g.raddr.toUDPAddr().String())
}
func (g *GConn) Read(b []byte) (n int, err error) {
	g.init()
	return g.recvWin.Read(b)
}

func (g *GConn) Write(b []byte) (n int, err error) {
	g.init()
	return len(b), g.sendWin.Write(b)
}

func (g *GConn) Close() error {
	g.init()
	return g.close()
}

func (g *GConn) LocalAddr() net.Addr {
	g.init()
	return g.laddr
}

func (g *GConn) RemoteAddr() net.Addr {
	g.init()
	return g.raddr
}

func (g *GConn) SetDeadline(t time.Time) error {
	g.init()
	panic("implement me")
}

func (g *GConn) SetReadDeadline(t time.Time) error {
	g.init()
	panic("implement me")
}

func (g *GConn) SetWriteDeadline(t time.Time) error {
	g.init()
	panic("implement me")
}

func (g *GConn) Status() (s Status) {
	g.init()
	return g.s
}

func (g *GConn) dial() (err error) {
	g.init()
	// sync req
	m1 := &v1.Message{}
	if g.synSeqX == 0 {
		g.synSeqX = uint32(rand.Int31n(2<<16 - 2))
	}
	m1.SYN1(g.synSeqX)
	err = g.sendMessage(m1)
	if err != nil {
		log.Println("GConn#dial sendMessage1 err , err is", err)
	}
	g.s = g.maxStatus(StatusSynSent, g.s)
	for {
		if g.syn1RetryTime >= syn1RetryTime {
			log.Println("GConn#dial : syn1 fail , retry time is :" + fmt.Sprint(g.syn1RetryTime))
			g.Close()
			return errors.New("syn1 fail , retry time is :" + fmt.Sprint(g.syn1RetryTime))
		}
		select {
		case <-g.syn1Finish:
			return
		case <-time.After(time.Duration(syn1Timeout) * time.Millisecond): // wait ack timeout
			err = g.sendMessage(m1)
			if err != nil {
				log.Println("GConn#dial sendMessage2 err , err is", err)
			}
			g.syn1RetryTime++
		}
	}
}

// close
func (g *GConn) close() (err error) {
	if g.s > StatusEstablished { //closing
		return
	}
	select {
	case <-g.closeSignal:
	default:
		close(g.closeSignal)
	}
	if g.s < StatusEstablished { // syncing
		g.sendWin.Close()
		g.recvWin.Close()
		g.UDPConn.Close()
		g.s = StatusClosed
		return
	}
	g.sendWin.Close()
	m := &v1.Message{}
	g.fin1SeqU = uint32(rand.Int31n(2<<16 - 2))
	m.FIN1(g.fin1SeqU)
	err = g.sendMessage(m)
	if err != nil {
		return err
	}
	g.s = StatusFinWait1
	<-g.closeFinish
	return g.UDPConn.Close()
}

func (g *GConn) maxStatus(s1, s2 Status) (ms Status) {
	if s1 > s2 {
		return s1
	}
	return s2
}
