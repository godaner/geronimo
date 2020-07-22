package net

import (
	"github.com/godaner/geronimo/rule"
	v1 "github.com/godaner/geronimo/rule/v1"
	v12 "github.com/godaner/geronimo/win/v1"
	"log"
	"net"
	"sync"
	"time"
)

const (
	udpmss      = 1472
	syncTimeout = 2000
	ackTimeout  = 2000
)
const (
	_                 = iota
	StatusSynSent     = iota
	StatusSynRecved   = iota
	StatusEstablished = iota
	StatusFinWait1    = iota
	StatusCloseWait   = iota
	StatusFinWait2    = iota
	StatusLastAck     = iota
	StatusTimeWait    = iota
)
const (
	_       = iota
	FDial   = iota
	FListen = iota
)

type Status uint16
type GConn struct {
	sync.Once
	*net.UDPConn
	f                uint8
	s                Status
	fromListenerData chan *v1.Message
	recvWin          *v12.RWND
	sendWin          *v12.SWND
	raddr            *GAddr
	laddr            *GAddr
}

func (g *GConn) init() {
	g.Do(func() {
		g.fromListenerData = make(chan *v1.Message)
		g.recvWin = &v12.RWND{
			AckSender: func(ack uint32, receiveWinSize uint16) (err error) {
				m := &v1.Message{}
				m.ACK(ack, receiveWinSize)
				b := m.Marshall()
				if g.f == FDial {
					_, err = g.UDPConn.Write(b)
					log.Println("GConn : udp from ", g.UDPConn.LocalAddr().String(), " to", g.UDPConn.RemoteAddr().String())
					return err
				}
				if g.f == FListen {
					_, err = g.UDPConn.WriteToUDP(b, g.raddr.toUDPAddr())
					log.Println("GConn : udp from ", g.UDPConn.LocalAddr().String(), " to", g.raddr.toUDPAddr().String())
					return err
				}
				return nil
			},
		}
		g.sendWin = &v12.SWND{
			SegmentSender: func(firstSeq uint32, bs []byte) (err error) {
				// send udp
				m := &v1.Message{}
				m.PAYLOAD(firstSeq, bs)
				b := m.Marshall()
				if g.f == FDial {
					_, err = g.UDPConn.Write(b)
					log.Println("GConn : udp from ", g.UDPConn.LocalAddr().String(), " to", g.UDPConn.RemoteAddr().String())
					return err
				}
				if g.f == FListen {
					_, err = g.UDPConn.WriteToUDP(b, g.raddr.toUDPAddr())
					log.Println("GConn : udp from ", g.UDPConn.LocalAddr().String(), " to", g.raddr.toUDPAddr().String())
					return err
				}
				return nil
			},
		}
		if g.f == FListen {
			go func() {
				for {
					select {
					case m := <-g.fromListenerData:
						if m.Flag()&rule.FlagPAYLOAD == rule.FlagPAYLOAD {
							g.recvWin.RecvSegment(m.SeqN(), m.AttributeByType(rule.AttrPAYLOAD))
							continue
						}
						if m.Flag()&rule.FlagACK == rule.FlagACK {
							g.sendWin.RecvAckSegment(m.WinSize(), m.AckN())
							continue
						}
						panic("no handler")
					}

				}

			}()
			return
		}
		if g.f == FDial {
			go func() {
				// recv udp
				bs := make([]byte, udpmss, udpmss)
				for {
					func(){
						n, err := g.UDPConn.Read(bs)
						if err != nil {
							panic(err)
						}
						m := &v1.Message{}
						m.UnMarshall(bs[:n])
						if m.Flag()&rule.FlagPAYLOAD == rule.FlagPAYLOAD {
							g.recvWin.RecvSegment(m.SeqN(), m.AttributeByType(rule.AttrPAYLOAD))
							return
						}
						if m.Flag()&rule.FlagACK == rule.FlagACK {
							g.sendWin.RecvAckSegment(m.WinSize(), m.AckN())
							return
						}
						panic("no handler")
					}()
				}
			}()
			return
		}

	})
}

func (g *GConn) dataFromListener(m *v1.Message) {
	g.init()
	g.fromListenerData <- m
}
func (g *GConn) Read(b []byte) (n int, err error) {
	g.init()
	return g.recvWin.Read(b)
}

func (g *GConn) Write(b []byte) (n int, err error) {
	g.init()
	g.sendWin.Write(b)
	return len(b), nil
}

func (g *GConn) Close() error {
	g.init()
	return nil // todo how to close ???
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
