package net

import (
	"errors"
	"fmt"
	"github.com/godaner/geronimo/logger"
	gologging "github.com/godaner/geronimo/logger/go-logging"
	"github.com/godaner/geronimo/rule"
	v1 "github.com/godaner/geronimo/rule/v1"
	"github.com/godaner/geronimo/win"
	"math/rand"
	"net"
	"strconv"
	"sync"
	"time"
)

const (
	udpmss = 1472
)
const (
	syn1ResendTime  = time.Duration(500) * time.Millisecond
	fin1ResendTIme  = time.Duration(500) * time.Millisecond
	syn1ResendCount = 5
	fin1ResendCount = 5
)
const (
	_ = iota
	StatusListen
	StatusSynSent
	StatusEstablished
	StatusFinWait1
	StatusClosed
)

const (
	_ = iota
	FDial
	FListen
)
const msl = 60 * 2

var (
	ErrConnStatus   = errors.New("status err")
	ErrConnClosed   = errors.New("closed")
	ErrNotReachable = errors.New("host not reachable")
	ErrDialTimeout  = errors.New("dial timeout")
	ErrFINTimeout   = errors.New("fin timeout")
)

type Status uint16

func (s Status) String() string {
	return fmt.Sprint(uint16(s))
}

type GConn struct {
	*net.UDPConn
	OverBose                                                        bool
	initOnce, initSendWinOnce, initRecvWinOnce, initLoopReadUDPOnce sync.Once
	f                                                               uint8
	s                                                               Status
	recvWin                                                         *win.RWND
	sendWin                                                         *win.SWND
	raddr                                                           *GAddr
	laddr                                                           *GAddr
	lis                                                             *GListener
	synSeqX, synSeqY, finSeqU, finSeqV, finSeqW                     uint16
	syn1Finish, syn2Finish, fin1Finish, fin3Finish                  chan bool
	syn1ResendCount, fin1ResendCount                                uint8
	mhs                                                             map[uint16]messageHandler
	logger                                                          logger.Logger
}

func (g *GConn) String() string {
	return fmt.Sprintf("GConn:p%v,l%v,r%v", &g, g.laddr.String(), g.raddr.String())
}

func (g *GConn) Read(b []byte) (n int, err error) {
	g.init()
	return g.recvWin.Read(b)
}

func (g *GConn) Write(b []byte) (n int, err error) {
	g.init()
	if g.Status() == StatusClosed {
		return 0, ErrConnClosed
	}
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

func (g *GConn) init() {
	g.initOnce.Do(func() {
		g.logger = gologging.GetLogger(g.String())
		g.syn1Finish = make(chan bool)
		g.syn2Finish = make(chan bool)
		g.fin1Finish = make(chan bool)
		g.fin3Finish = make(chan bool)
		// init message handlers
		g.mhs = map[uint16]messageHandler{
			// syn
			rule.FlagSYN1: g.syn1MessageHandler,
			rule.FlagSYN2: g.syn2MessageHandler,
			// fin
			rule.FlagFIN1: g.fin1MessageHandler,
			rule.FlagFIN2: g.fin2MessageHandler,
			// body
			rule.FlagPAYLOAD: g.payloadMessageHandler,
			// ack
			rule.FlagACK: g.ackMessageHandler,
		}

	})
}

// loopReadUDP
func (g *GConn) loopReadUDP() {
	g.initLoopReadUDPOnce.Do(func() {
		if g.f == FDial {
			go func() {
				defer g.logger.Warning("GConn#init : FDial stop loop read udp")
				// recv udp
				bs := make([]byte, udpmss, udpmss)
				for {
					select {
					default:
						n, err := g.UDPConn.Read(bs)
						if err != nil {
							// not normal close , maybe server shutdown
							if g.s == StatusEstablished {
								g.closeWin()
								g.closeUDPConn()
								g.s = StatusClosed
							}
							return
						}
						m := &v1.Message{}
						m.UnMarshall(bs[:n])
						err = g.handleMessage(m)
						if err != nil {
							g.logger.Error("GConn#init : handleMessage err", err)
							continue
						}
					}

				}
			}()
			return
		}
		panic("not from dial")
	})

}

// initWin
func (g *GConn) initWin() {
	g.initSendWin()
	g.initRecvWin()
}

// initRecvWin
func (g *GConn) initRecvWin() {
	g.initRecvWinOnce.Do(func() {
		g.recvWin = &win.RWND{
			OverBose: g.OverBose,
			FTag:     g.String(),
			AckSender: func(seq, ack, receiveWinSize uint16) (err error) {
				m := &v1.Message{}
				m.ACK(seq, ack, receiveWinSize)
				return g.sendMessage(m)
			},
		}
	})
}

// initSendWin
func (g *GConn) initSendWin() {
	g.initSendWinOnce.Do(func() {
		g.sendWin = &win.SWND{
			OverBose: g.OverBose,
			FTag:     g.String(),
			SegmentSender: func(seq uint16, bs []byte) (err error) {
				// send udp
				m := &v1.Message{}
				m.PAYLOAD(seq, bs)
				return g.sendMessage(m)
			},
		}
	})
}

// handleMessage
func (g *GConn) handleMessage(m *v1.Message) (err error) {
	g.init()
	mh, ok := g.mhs[m.Flag()]
	if ok && mh != nil {
		return mh(m)
	}
	g.logger.Warning("GConn#handleMessage : no message handler be found , flag is", m.Flag(), ", conn status is", g.s, ", flag is", strconv.FormatUint(uint64(m.Flag()), 2))
	panic("no handler")
}

// sendMessage
func (g *GConn) sendMessage(m *v1.Message) (err error) {
	b := m.Marshall()
	if g.f == FDial {
		g.logger.Debug("GConn : FDial udp from ", g.UDPConn.LocalAddr().String(), " to", g.UDPConn.RemoteAddr().String(), ", flag is", m.Flag())
		_, err = g.UDPConn.Write(b)
		if err != nil {
			g.logger.Error("GConn : FDial udp from ", g.UDPConn.LocalAddr().String(), " to", g.UDPConn.RemoteAddr().String(), ", flag is", m.Flag(), " err", err)
		}
		return err
	}
	if g.f == FListen {
		g.logger.Debug("GConn : FListen udp from ", g.UDPConn.LocalAddr().String(), " to", g.raddr.toUDPAddr().String(), ", flag is", m.Flag())
		_, err = g.UDPConn.WriteToUDP(b, g.raddr.toUDPAddr())
		if err != nil {
			g.logger.Error("GConn : FListen udp from ", g.UDPConn.LocalAddr().String(), " to", g.raddr.toUDPAddr().String(), ", flag is", m.Flag(), " err", err)
			panic("listener write err , err is : " + err.Error())
		}
		return err
	}
	return nil
}

// closeUDPConn
func (g *GConn) closeUDPConn() {
	if g.f == FDial {
		g.UDPConn.Close()
	}
	if g.f == FListen {
		g.lis.RmGConn(g.raddr.toUDPAddr().String())
	}
}
func (g *GConn) closeRecvWin() (err error) {
	if g.recvWin == nil {
		return
	}
	return g.recvWin.Close()
}
func (g *GConn) closeSendWin() (err error) {
	if g.sendWin == nil {
		return
	}
	return g.sendWin.Close()
}
func (g *GConn) closeWin() (err error) {
	err = g.closeSendWin()
	if err != nil {
		return err
	}
	return g.closeRecvWin()
}

// dial
// fdial
func (g *GConn) dial() (err error) {
	g.init()
	g.logger.Notice("GConn#dial : dial start")
	// sync req
	m1 := &v1.Message{}
	if g.synSeqX == 0 {
		//g.synSeqX = uint32(rand.Int31n(2<<16 - 2))
		g.synSeqX = g.random()
	}
	m1.SYN1(g.synSeqX)
	//panic(1472-len(m1.Marshall()))
	err = g.sendMessage(m1)
	if err != nil {
		g.logger.Error("GConn#dial : send SYN1 err , maybe server not listening now , err is", err)
		return ErrNotReachable
	}
	// send udp success , mean dst can find , read it
	g.loopReadUDP()
	g.s = StatusSynSent
	for {
		if g.syn1ResendCount >= syn1ResendCount {
			g.logger.Error("GConn#dial : send SYN1 fail , retry time is :" + fmt.Sprint(g.syn1ResendCount))
			g.closeUDPConn()
			g.s = StatusClosed
			// todo keepalive
			return ErrDialTimeout
		}
		select {
		case <-g.syn1Finish:
			g.initWin()
			g.s = StatusEstablished
			g.logger.Notice("GConn#dial : success")
			return nil
		//case <-g.readErr:
		//	g.closeUDPConn()
		//	g.s = StatusClosed
		//	g.logger.Error("GConn#dial : read udp err , maybe server not listening now")
		//	return nil
		case <-time.After(syn1ResendTime): // wait ack timeout
			g.syn1ResendCount++
			err = g.sendMessage(m1)
			if err != nil {
				g.logger.Error("GConn#dial : resend SYN1 err , maybe server shutdown now , err is", err)
				return ErrNotReachable
			}
		}
	}
}
func (g *GConn) random() uint16 {
	i32 := rand.Int31n(1<<16 - 2)
	return uint16(i32)
}

// close
// fdial or flisten
func (g *GConn) close() (err error) {
	defer func() {
		g.closeRecvWin()
		g.closeUDPConn()
		g.s = StatusClosed
	}()
	g.logger.Notice("GConn#close : start ")
	if g.s != StatusEstablished {
		g.logger.Warning("GConn#close : status is not StatusEstablished")
		return ErrConnStatus
	}
	g.s = StatusFinWait1
	g.closeSendWin()
	m := &v1.Message{}
	if g.finSeqU == 0 {
		g.finSeqU = g.random()
	}
	m.FIN1(g.finSeqU)
	err = g.sendMessage(m)
	if err != nil {
		g.logger.Error("GConn#close send FIN1 err , maybe server shutdown now , err is", err)
		return ErrNotReachable
	}
	for {
		if g.fin1ResendCount >= fin1ResendCount {
			g.logger.Error("GConn#close : send FIN1 fail , retry time is :" + fmt.Sprint(g.fin1ResendCount))
			return ErrFINTimeout
		}
		select {
		case <-g.fin1Finish:
			g.logger.Notice("GConn#close : success")
			return ErrNotReachable
		//case <-g.readErr: // just for fdail
		//g.closeRecvWin()
		//g.closeUDPConn()
		//g.s = StatusClosed
		//g.logger.Error("GConn#close : read udp err , maybe server not listening now")
		//return nil
		case <-time.After(fin1ResendTIme): // wait ack timeout
			g.fin1ResendCount++
			err = g.sendMessage(m)
			if err != nil {
				g.logger.Error("GConn#close : send FIN1 err , maybe server shutdown now , err , err is", err)
				return ErrNotReachable
			}
		}
	}
}

func (g *GConn) maxStatus(s1, s2 Status) (ms Status) {
	if s1 > s2 {
		return s1
	}
	return s2
}
