package v1

import (
	"errors"
	"fmt"
	"github.com/godaner/geronimo/logger"
	gologging "github.com/godaner/geronimo/logger/go-logging"
	"github.com/godaner/geronimo/rule"
	v1 "github.com/godaner/geronimo/rule/v1"
	v12 "github.com/godaner/geronimo/win/v1"
	"math/rand"
	"net"
	"strconv"
	"sync"
	"time"
)

const (
	udpmss        = 1472
	syn1Timeout   = 500
	syn2Timeout   = 500
	fin1Timeout   = 500
	fin3Timeout   = 500
	syn1RetryTime = 5
	syn2RetryTime = 5
	fin1RetryTime = 5
	fin3RetryTime = 5
)
const (
	_ = iota
	StatusListen
	StatusSynSent
	StatusSynRecved
	StatusEstablished
	StatusFinWait1
	StatusCloseWait
	StatusFinWait2
	StatusLastAck
	StatusTimeWait
	StatusClosed
)

const (
	_ = iota
	FDial
	FListen
)
const msl = 60 * 2

var (
	ErrConnStatus          = errors.New("status err")
	ErrConnClosed          = errors.New("closed")
	ErrDialBasicConnClosed = errors.New("dial but basic conn closed")
	ErrDialTimeout         = errors.New("dial timeout")
	ErrFINTimeout          = errors.New("fin timeout")
)

type Status uint16

func (s Status) String() string {
	return fmt.Sprint(uint16(s))
}

type GConn struct {
	*net.UDPConn
	initOnce, initSendWinOnce, initRecvWinOnce, initReadFDialUDPOnce sync.Once
	f                                                                uint8
	s                                                                Status
	recvWin                                                          *v12.RWND
	sendWin                                                          *v12.SWND
	raddr                                                            *GAddr
	laddr                                                            *GAddr
	lis                                                              *GListener
	synSeqX, synSeqY, finSeqU, finSeqV, finSeqW                      uint32
	syn1RetryTime, syn2RetryTime, fin1RetryTime, fin3RetryTime       uint8
	syn1Finish, syn2Finish, fin1Finish, fin3Finish                   chan bool
	mhs                                                              map[uint16]messageHandler
	fDialCloseUDPSignal, fListenCloseUDPSignal                       chan bool
	logger                                                           logger.Logger
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
		g.fDialCloseUDPSignal = make(chan bool)
		g.fListenCloseUDPSignal = make(chan bool)
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

// initLoopFDialReadUDP
func (g *GConn) initLoopFDialReadUDP() {
	g.initReadFDialUDPOnce.Do(func() {
		if g.f == FDial {
			go func() {
				defer g.logger.Warning("GConn#init : stop loop read udp")
				// recv udp
				bs := make([]byte, udpmss, udpmss)
				for {
					select {
					case <-g.fDialCloseUDPSignal:
						return
					default:
						//func() {
						n, err := g.UDPConn.Read(bs)
						if err != nil {
							g.logger.Error("GConn#init : Read err", err)
							// just for fdial
							g.closeUDPConn()
							g.closeSendWin()
							g.closeRecvWin()
							g.s = StatusClosed
							return
						}
						m := &v1.Message{}
						m.UnMarshall(bs[:n])
						err = g.handleMessage(m)
						if err != nil {
							g.logger.Error("GConn#init : handleMessage err", err)
							continue
						}
						//}()
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
		g.recvWin = &v12.RWND{
			FTag: g.String(),
			AckSender: func(ack uint32, receiveWinSize uint16) (err error) {
				m := &v1.Message{}
				m.ACK(ack, receiveWinSize)
				return g.sendMessage(m)
			},
		}
	})
}

// initSendWin
func (g *GConn) initSendWin() {
	g.initSendWinOnce.Do(func() {
		g.sendWin = &v12.SWND{
			FTag: g.String(),
			SegmentSender: func(seq uint32, bs []byte) (err error) {
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
			g.closeUDPConn()
		}
		return err
	}
	if g.f == FListen {
		g.logger.Debug("GConn : FListen udp from ", g.UDPConn.LocalAddr().String(), " to", g.raddr.toUDPAddr().String(), ", flag is", m.Flag())
		_, err = g.UDPConn.WriteToUDP(b, g.raddr.toUDPAddr())
		if err != nil {
			g.logger.Error("GConn : FListen udp from ", g.UDPConn.LocalAddr().String(), " to", g.raddr.toUDPAddr().String(), ", flag is", m.Flag(), " err", err)
			g.closeUDPConn() // todo ?????
		}
		return err
	}
	return nil
}

// closeUDPConn
func (g *GConn) closeUDPConn() {
	if g.f == FDial {
		select {
		case <-g.fDialCloseUDPSignal:
		default:
			close(g.fDialCloseUDPSignal)
		}
		g.UDPConn.Close()
	}
	if g.f == FListen {
		g.lis.gcs.Delete(g.raddr.toUDPAddr().String())
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
func (g *GConn) dial() (err error) {
	g.init()
	g.logger.Debug("GConn#dial dial start")
	g.initLoopFDialReadUDP()
	// sync req
	m1 := &v1.Message{}
	if g.synSeqX == 0 {
		g.synSeqX = uint32(rand.Int31n(2<<16 - 2))
	}
	m1.SYN1(g.synSeqX)
	err = g.sendMessage(m1)
	if err != nil {
		g.logger.Error("GConn#dial sendMessage1 err , err is", err)
	}
	g.s = StatusSynSent
	for {
		if g.syn1RetryTime >= syn1RetryTime {
			g.logger.Debug("GConn#dial : syn1 fail , retry time is :" + fmt.Sprint(g.syn1RetryTime))
			g.closeUDPConn()
			//return errors.New("syn1 fail , retry time is :" + fmt.Sprint(g.syn1RetryTime))
			return ErrDialTimeout
		}
		select {
		case <-g.syn1Finish:
			g.initWin()
			g.s = StatusEstablished
			g.logger.Notice("GConn#dial : success")
			return nil
		case <-g.fDialCloseUDPSignal:
			g.logger.Warning("GConn#dial : udp conn is closed")
			g.s = StatusClosed
			return ErrDialBasicConnClosed
		case <-time.After(time.Duration(syn1Timeout) * time.Millisecond): // wait ack timeout
			err = g.sendMessage(m1)
			if err != nil {
				g.logger.Error("GConn#dial sendMessage2 err , err is", err)
				continue
			}
			g.syn1RetryTime++
		}
	}
}

// close
// fdial or flisten
func (g *GConn) close() (err error) {
	g.logger.Debug("GConn#close : start ")
	if g.s != StatusEstablished {
		return ErrConnStatus
	}
	g.closeSendWin()
	m := &v1.Message{}
	if g.finSeqU == 0 {
		g.finSeqU = uint32(rand.Int31n(2<<16 - 2))
	}
	m.FIN1(g.finSeqU)
	err = g.sendMessage(m)
	if err != nil {
		g.logger.Error("GConn#close sendMessage1 err , err is", err)
	}
	g.s = StatusFinWait1
	for {
		if g.fin1RetryTime >= fin1RetryTime {
			g.logger.Debug("GConn#close : fin1 fail , retry time is :" + fmt.Sprint(g.fin1RetryTime))
			g.closeRecvWin()
			g.closeUDPConn()
			g.s = StatusClosed
			//return errors.New("fin1 fail , retry time is :" + fmt.Sprint(g.fin1RetryTime))
			return ErrFINTimeout
		}
		select {
		case <-g.fin1Finish:
			g.closeRecvWin()
			g.closeUDPConn()
			g.s = StatusClosed
			g.logger.Debug("GConn#close : success")
			return nil
		case <-g.fDialCloseUDPSignal:
			// for fdial
			g.logger.Warning("GConn#close : fdial udp conn is closed")
			g.closeRecvWin()
			g.s = StatusClosed
			return ErrDialBasicConnClosed
		case <-g.fListenCloseUDPSignal:
			// for flisten
			g.logger.Warning("GConn#close : fdial udp conn is closed")
			g.closeRecvWin()
			g.s = StatusClosed
			g.logger.Warning("GConn#close : flisten udp conn is closed")
			return ErrDialBasicConnClosed
		case <-time.After(time.Duration(fin1Timeout) * time.Millisecond): // wait ack timeout
			err = g.sendMessage(m)
			if err != nil {
				g.logger.Error("GConn#close sendMessage2 err , err is", err)
				continue
			}
			g.fin1RetryTime++
		}
	}
}

func (g *GConn) maxStatus(s1, s2 Status) (ms Status) {
	if s1 > s2 {
		return s1
	}
	return s2
}
