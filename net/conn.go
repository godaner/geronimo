package net

import (
	"errors"
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
	udpmss            = 1472
	syncTimeout       = 2000
	lastSynAckTimeout = 1000
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
	dialFinish                                     chan error
	closeFinish                                    chan bool
}

func (g *GConn) init() {
	g.Do(func() {
		g.dialFinish = make(chan error)
		g.closeFinish = make(chan bool)
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
					func() {
						n, err := g.UDPConn.Read(bs)
						if err != nil {
							panic(err)
						}
						m := &v1.Message{}
						m.UnMarshall(bs[:n])
						g.handleMessage(m)
					}()
				}
			}()
			return
		}

	})
}

// handleMessage
func (g *GConn) handleMessage(m *v1.Message) {
	g.init()
	if m.Flag()&rule.FlagPAYLOAD == rule.FlagPAYLOAD {
		g.payloadMessageHandler(m)
		return
	}
	// connect
	if g.s == StatusListen && m.Flag()&rule.FlagSYN == rule.FlagSYN {
		g.connectSynMessageHandler(m)
		return
	}
	if g.s == StatusSynSent && m.Flag()&rule.FlagACK == rule.FlagACK && m.Flag()&rule.FlagSYN == rule.FlagSYN {
		g.connectSynAckMessageHandler(m)
		return
	}
	if g.s == StatusSynRecved && m.Flag()&rule.FlagACK == rule.FlagACK {
		g.connectAckMessageHandler(m)
		return
	}
	// fin
	if g.s == StatusEstablished && m.Flag()&rule.FlagFIN == rule.FlagFIN {
		g.finMessageHandler(m)
		return
	}
	if g.s == StatusFinWait1 && m.Flag()&rule.FlagACK == rule.FlagACK {
		g.fin1AckMessageHandler(m)
		return
	}
	if g.s == StatusFinWait2 && m.Flag()&rule.FlagACK == rule.FlagACK && m.Flag()&rule.FlagFIN == rule.FlagFIN {
		g.fin2AckMessageHandler(m)
		return
	}
	if g.s == StatusLastAck && m.Flag()&rule.FlagACK == rule.FlagACK {
		g.finLastAckMessageHandler(m)
		return
	}
	if g.s >= StatusEstablished && m.Flag()&rule.FlagACK == rule.FlagACK {
		g.ackMessageHandler(m)
		return
	}
	log.Println("conn status is", g.s, ", flag is", strconv.FormatUint(uint64(m.Flag()), 2))
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

func (g *GConn) close() (err error) {
	if g.s != StatusEstablished {
		return errors.New("status is not right")
	}
	g.sendWin.Close()
	m := &v1.Message{}
	g.fin1SeqU = uint32(rand.Int31n(2<<16 - 2))
	m.FIN(g.fin1SeqU)
	err = g.sendMessage(m)
	if err != nil {
		return err
	}
	g.s = StatusFinWait1
	<-g.closeFinish
	return nil
}
