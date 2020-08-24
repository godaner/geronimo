package net

import (
	"errors"
	"fmt"
	"github.com/godaner/geronimo/rule"
	msg "github.com/godaner/geronimo/rule"
	"github.com/godaner/geronimo/rule/fac"
	"github.com/godaner/geronimo/win"
	"github.com/godaner/logger"
	loggerfac "github.com/godaner/logger/factory"
	"github.com/looplab/fsm"
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
	keepalive   = time.Duration(5) * time.Second
	keepaliveTo = 5 * keepalive
)
const (
	syn1ResendTime    = time.Duration(100) * time.Millisecond
	syn1MaxResendTime = time.Duration(100) * time.Millisecond
	fin1ResendTIme    = time.Duration(500) * time.Millisecond
	fin1MaxResendTIme = time.Duration(500) * time.Millisecond
	syn1ResendCount   = 10
	fin1ResendCount   = 10
)
const (
	StatusInit           = "StatusInit"
	StatusSynSent        = "StatusSynSent"
	StatusSerEstablished = "StatusSerEstablished"
	StatusCliEstablished = "StatusCliEstablished"
	StatusFinWait1       = "StatusFinWait1"
	StatusClosed         = "StatusClosed"
)
const (
	EventCliDial           = "EventCliDial"
	EventSerRecvSyn1       = "EventSerRecvSyn1"
	EventCliRecvSyn2       = "EventCliRecvSyn2"
	EventCliNotRecvSyn2Err = "EventCliNotRecvSyn2Err"
	EventRecvFin1          = "EventRecvFin1"
	EventRecvFin2          = "EventRecvFin2"
	EventNotRecvFin2Err    = "EventNotRecvFin2Err"
	EventClose             = "EventClose"
	EventForceClose        = "EventForceClose"
)

const (
	_ = iota
	FDial
	FListen
)
const (
	msl = time.Duration(1) * time.Minute
)

var (
	errNotEstablished = errors.New("not established")
	errNotReachable   = errors.New("host not reachable")
	errDialTimeout    = errors.New("dial timeout")
	errFINTimeout     = errors.New("fin timeout")
)

type GConn struct {
	*net.UDPConn
	initOnce, toAcceptOnce, keepaliveOnce, initSendWinOnce, initRecvWinOnce, initLoopReadUDPOnce sync.Once
	f                                                                                            uint8
	recvWin                                                                                      *win.RWND
	sendWin                                                                                      *win.SWND
	raddr                                                                                        *GAddr
	laddr                                                                                        *GAddr
	lis                                                                                          *GListener
	synSeqX, synSeqY, finSeqU, finSeqV                                                           uint16
	syn1Finish, fin1Finish                                                                       chan struct{}
	syn1ResendCount, fin1ResendCount                                                             uint8
	rdl, wdl                                                                                     time.Time
	keepaliveC                                                                                   chan struct{}
	mhs                                                                                          map[uint16]messageHandler
	logger                                                                                       logger.Logger
	fsm                                                                                          *fsm.FSM
	keepaliveTimer                                                                               *time.Timer
	MsgFac                                                                                       *fac.Fac
}

func (g *GConn) String() string {
	return fmt.Sprintf("GConn:p%v,l%v,r%v", &g, g.laddr.String(), g.raddr.String())
}

func (g *GConn) Read(b []byte) (n int, err error) {
	g.init()
	if g.Status() != StatusCliEstablished && g.Status() != StatusSerEstablished {
		return 0, errNotEstablished
	}
	return g.recvWin.Read(b, g.rdl)
}

func (g *GConn) Write(b []byte) (n int, err error) {
	g.init()
	if g.Status() != StatusCliEstablished && g.Status() != StatusSerEstablished {
		return 0, errNotEstablished
	}
	return len(b), g.sendWin.Write(b, g.wdl)
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
	g.rdl = t
	g.wdl = t
	return nil
}

func (g *GConn) SetReadDeadline(t time.Time) error {
	g.init()
	g.rdl = t
	return nil
}

func (g *GConn) SetWriteDeadline(t time.Time) error {
	g.init()
	g.wdl = t
	return nil
}

func (g *GConn) Status() (s string) {
	g.init()
	return g.fsm.Current()
}

func (g *GConn) init() {
	g.initOnce.Do(func() {
		g.logger = loggerfac.GetLogger(g.String())
		g.syn1Finish = make(chan struct{})
		g.fin1Finish = make(chan struct{})
		g.keepaliveC = make(chan struct{})
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
			// keepalive
			rule.FlagKeepAlive: g.keepaliveMessageHandler,
		}
		// fsm
		g.fsm = fsm.NewFSM(
			StatusInit,
			fsm.Events{
				{Name: EventCliDial, Src: []string{StatusInit}, Dst: StatusSynSent},
				{Name: EventSerRecvSyn1, Src: []string{StatusInit, StatusSerEstablished}, Dst: StatusSerEstablished},
				{Name: EventCliRecvSyn2, Src: []string{StatusSynSent}, Dst: StatusCliEstablished},
				{Name: EventCliNotRecvSyn2Err, Src: []string{StatusSynSent}, Dst: StatusClosed},
				{Name: EventClose, Src: []string{StatusSerEstablished, StatusCliEstablished}, Dst: StatusFinWait1},
				{Name: EventRecvFin1, Src: []string{StatusSerEstablished, StatusCliEstablished, StatusClosed}, Dst: StatusClosed},
				{Name: EventRecvFin2, Src: []string{StatusFinWait1}, Dst: StatusClosed},
				{Name: EventNotRecvFin2Err, Src: []string{StatusFinWait1}, Dst: StatusClosed},
				{Name: EventForceClose, Src: []string{StatusSerEstablished, StatusCliEstablished}, Dst: StatusClosed},
			},
			fsm.Callbacks{
				// dst : callback
				StatusSynSent: func(event *fsm.Event) {
					var err error
					defer func() {
						block, ok := event.Args[0].(chan error)
						if ok {
							block <- err
						}
					}()
					//block chan struct{},
					err = func() (err error) {
						// sync req
						m1 := g.MsgFac.New()
						if g.synSeqX == 0 {
							//g.synSeqX = uint32(rand.Int31n(2<<16 - 2))
							g.synSeqX = g.random()
						}
						m1.SYN1(g.synSeqX)
						//panic(1472-len(m1.Marshall()))
						err = g.sendMessage(m1)
						if err != nil {
							g.logger.Error("GConn#dial : send SYN1 err , maybe server not listening now , err is", err)
							return errNotReachable
						}
						// send udp success , mean dst can find , read it
						g.loopReadUDP()
						syn1ResendTime := syn1ResendTime
						for {
							if g.syn1ResendCount >= syn1ResendCount {
								return errDialTimeout
							}
							select {
							case <-g.syn1Finish:
								return nil
							case <-time.After(syn1ResendTime): // wait ack timeout
								g.syn1ResendCount++
								syn1ResendTime *= 2
								if syn1ResendTime > syn1MaxResendTime {
									syn1ResendTime = syn1MaxResendTime
								}
								err = g.sendMessage(m1)
								if err != nil {
									g.logger.Error("GConn#dial : resend SYN1 err , maybe server shutdown now , err is", err)
									return errNotReachable
								}
							}
						}
					}()

				},
				StatusSerEstablished: func(event *fsm.Event) {
					m := event.Args[0].(msg.Message)
					g.synSeqX = m.SeqN()
					if g.synSeqY == 0 {
						g.synSeqY = g.random()
					}
					g.initWin()
					m1 := g.MsgFac.New()
					m1.SYN2(g.synSeqY, g.synSeqX+1)
					err := g.sendMessage(m1)
					if err != nil {
						g.logger.Error("GConn#syn1MessageHandler : sendMessage1 err", err)
					}
					// keepalive
					g.keepalive()
					// to accept
					g.toAccept()
				},
				StatusCliEstablished: func(event *fsm.Event) {
					// init window
					g.initWin()
					//keepalive
					g.keepalive()
				},
				StatusClosed: func(event *fsm.Event) {
					// recv syn2 err , maybe wait for syn2 timeout, etc.
					if event.Event == EventCliNotRecvSyn2Err && event.Src == StatusSynSent {
						g.closeUDPConn()
						return
					}
					// recv fin1 , then close the conn
					if event.Event == EventRecvFin1 && (event.Src == StatusCliEstablished || event.Src == StatusSerEstablished || event.Src == StatusClosed) {
						m := event.Args[0].(msg.Message)
						g.logger.Notice("GConn#fin1MessageHandler : recv FIN1 start")
						g.finSeqU = m.SeqN()
						if g.finSeqV == 0 {
							g.finSeqV = g.random()
						}
						g.closeWin()
						g.logger.Debug("GConn#fin1MessageHandler : close win finish")
						m.FIN2(g.finSeqV, g.finSeqU+1)
						err := g.sendMessage(m)
						if err != nil {
							g.logger.Error("GConn#fin1MessageHandler : sendMessage1 err", err)
						}
						// wait 2msl , maybe recv fin1 again
						g.wait2msl()
						return
					}
					// recv fin2 success
					if event.Event == EventRecvFin2 && event.Src == StatusFinWait1 {
						g.closeRecvWin()
						g.closeUDPConn()
						return
					}
					// recv fin2 err , maybe wait for fin2 timeout, etc.
					if event.Event == EventNotRecvFin2Err && event.Src == StatusFinWait1 {
						g.closeRecvWin()
						g.closeUDPConn()
						return
					}
					// force close
					if event.Event == EventForceClose && (event.Src == StatusCliEstablished || event.Src == StatusSerEstablished) {
						g.closeWin()
						g.closeUDPConn()
						return
					}
				},
				StatusFinWait1: func(event *fsm.Event) {
					var err error
					defer func() {
						block, ok := event.Args[0].(chan error)
						if ok {
							block <- err
						}
					}()
					err = func() (err error) {
						g.closeSendWin()
						m := g.MsgFac.New()
						if g.finSeqU == 0 {
							g.finSeqU = g.random()
						}
						m.FIN1(g.finSeqU)
						err = g.sendMessage(m)
						if err != nil {
							g.logger.Error("GConn#close send FIN1 err , maybe server shutdown now , err is", err)
							return errNotReachable
						}
						fin1ResendTIme := fin1ResendTIme
						for {
							if g.fin1ResendCount >= fin1ResendCount {
								return errFINTimeout
							}
							select {
							case <-g.fin1Finish:
								return nil
							case <-time.After(fin1ResendTIme): // wait ack timeout
								g.fin1ResendCount++
								fin1ResendTIme *= 2
								if fin1ResendTIme > fin1MaxResendTIme {
									fin1ResendTIme = fin1MaxResendTIme
								}
								err = g.sendMessage(m)
								if err != nil {
									g.logger.Error("GConn#close : send FIN1 err , maybe server shutdown now , err , err is", err)
									return errNotReachable
								}
							}
						}
					}()
				},
			},
		)

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
							// not normal close , maybe server shutdown=
							err = g.fsm.Event(EventForceClose)
							if err != nil {
								g.logger.Error("GConn#init : read err , close status err", err)
							}
							return
						}
						m := g.MsgFac.New()
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
			FTag: g.String(),
			AckSender: func(seq, ack, receiveWinSize uint16) (err error) {
				m := g.MsgFac.New()
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
			FTag: g.String(),
			SegmentSender: func(seq uint16, bs []byte) (err error) {
				// send udp
				m := g.MsgFac.New()
				m.PAYLOAD(seq, bs)
				return g.sendMessage(m)
			},
		}
	})
}

// handleMessage
func (g *GConn) handleMessage(m msg.Message) (err error) {
	g.init()
	mh, ok := g.mhs[m.Flag()]
	if ok && mh != nil {
		return mh(m)
	}
	g.logger.Error("GConn#handleMessage : no message handler be found , flag is", m.Flag(), ", conn status is", g.fsm.Current(), ", flag is", strconv.FormatUint(uint64(m.Flag()), 2))
	panic("no handler")
}

// sendMessage
func (g *GConn) sendMessage(m msg.Message) (err error) {
	b, err := m.Marshall()
	if err != nil {
		g.logger.Error("GConn#sendMessage Marshall err", err)
		return
	}
	if g.f == FDial {
		g.logger.Debug("GConn#sendMessage : FDial udp from ", g.UDPConn.LocalAddr().String(), " to", g.UDPConn.RemoteAddr().String(), ", flag is", m.Flag())
		_, err = g.UDPConn.Write(b)
		if err != nil {
			g.logger.Error("GConn#sendMessage : FDial udp from ", g.UDPConn.LocalAddr().String(), " to", g.UDPConn.RemoteAddr().String(), ", flag is", m.Flag(), " err", err)
		}
		return err
	}
	if g.f == FListen {
		g.logger.Debug("GConn#sendMessage : FListen udp from ", g.UDPConn.LocalAddr().String(), " to", g.raddr.toUDPAddr().String(), ", flag is", m.Flag())
		_, err = g.UDPConn.WriteToUDP(b, g.raddr.toUDPAddr())
		if err != nil {
			g.logger.Error("GConn#sendMessage : FListen udp from ", g.UDPConn.LocalAddr().String(), " to", g.raddr.toUDPAddr().String(), ", flag is", m.Flag(), " err", err)
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
// only fdial
func (g *GConn) dial() (err error) {
	g.init()
	g.logger.Notice("GConn#dial : dial start")
	block := make(chan error, 1)
	err = g.fsm.Event(EventCliDial, block)
	if err != nil {
		g.logger.Error("GConn#dial : dial status err", err)
		return err
	}
	err = <-block
	if err != nil {
		g.logger.Error("GConn#dial : dial err", err)
		err = g.fsm.Event(EventCliNotRecvSyn2Err)
		if err != nil {
			g.logger.Error("GConn#dial : syn2 timeout status err", err)
			return err
		}
		return err
	}
	g.logger.Notice("GConn#dial : success")
	err = g.fsm.Event(EventCliRecvSyn2)
	if err != nil {
		g.logger.Error("GConn#dial : syn2 status err", err)
		return err
	}
	return nil
}
func (g *GConn) random() uint16 {
	i32 := rand.Int31n(1<<16 - 2)
	return uint16(i32)
}

// close
// fdial or flisten
func (g *GConn) close() (err error) {
	block := make(chan error, 1)
	err = g.fsm.Event(EventClose, block)
	if err != nil {
		g.logger.Error("GConn#close : close status err", err)
		return err
	}
	g.logger.Notice("GConn#close : start ")
	err = <-block
	if err != nil {
		g.logger.Error("GConn#close : close err", err)
		err = g.fsm.Event(EventNotRecvFin2Err)
		if err != nil {
			g.logger.Error("GConn#close : fin2 timeout status err", err)
			return err
		}
		return err
	}
	g.logger.Notice("GConn#close : success")
	err = g.fsm.Event(EventRecvFin2)
	if err != nil {
		g.logger.Error("GConn#close : fin2 status err", err)
		return err
	}
	g.logger.Notice("GConn#close : success ")
	return nil
}

// keepalive
func (g *GConn) keepalive() {
	g.keepaliveOnce.Do(func() {
		g.keepaliveTimer = time.NewTimer(keepaliveTo)
		// send keepalive
		go func() {
			for {
				select {
				case <-time.After(keepalive):
					err := g.sendMessage(g.MsgFac.New().KeepAlive())
					if err != nil {
						g.logger.Error("GConn#keepalive : send keepalive err", err)
						g.Close()
						return
					}
				}
			}
		}()

		// recv keepalive
		go func() {
			for {
				select {
				case <-g.keepaliveTimer.C:
					g.logger.Error("GConn#keepalive : keepalive timeout")
					g.keepaliveTimer.Stop()
					g.Close()
					return
				case <-g.keepaliveC:
					g.keepaliveTimer.Reset(keepaliveTo)
					continue
				}
			}
		}()
	})

}

// wait2msl
func (g *GConn) wait2msl() {
	// wait 2msl??
	go func() {
		<-time.After(2 * msl)
		g.closeUDPConn()
	}()
}

// toAccept
func (g *GConn) toAccept() {
	g.toAcceptOnce.Do(func() {
		g.lis.RecvNewConn(&acceptRes{c: g, err: nil})
	})
}
