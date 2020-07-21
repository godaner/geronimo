package net

import (
	"github.com/godaner/geronimo/rule"
	v1 "github.com/godaner/geronimo/rule/v1"
	v12 "github.com/godaner/geronimo/win/v1"
	"net"
	"sync"
	"time"
)

type GConn struct {
	recvWin *v12.RWND
	sendWin *v12.SWND
	sync.Once
	*net.UDPConn
}

func Dial(laddr, raddr *net.UDPAddr) (c *GConn, err error) {
	conn, err := net.DialUDP("udp", laddr, raddr)
	return &GConn{
		UDPConn: conn,
	}, nil
}
func (g *GConn) init() {
	g.Do(func() {
		g.recvWin = &v12.RWND{
			AckSender: func(ack uint32, receiveWinSize uint16) (err error) {
				m := &v1.Message{}
				m.ACK(ack, receiveWinSize)
				b := m.Marshall()
				_, err = g.UDPConn.Write(b)
				return err
			},
		}
		g.sendWin = &v12.SWND{
			SegmentSender: func(firstSeq uint32, bs []byte) (err error) {
				// send udp
				m := &v1.Message{}
				m.PAYLOAD(firstSeq, bs)
				b := m.Marshall()
				_, err = g.UDPConn.Write(b)
				return err
			},
		}
		go func() {
			// recv udp
			bs := make([]byte, 1472, 1472)
			for {
				_, err := g.UDPConn.Read(bs)
				if err != nil {
					panic(err)
				}
				m := &v1.Message{}
				m.UnMarshall(bs)
				if m.Flag()&rule.FlagPAYLOAD == rule.FlagPAYLOAD {
					g.recvWin.RecvSegment(m.SeqN(), m.AttributeByType(rule.AttrPAYLOAD))
					continue
				}
				if m.Flag()&rule.FlagACK == rule.FlagACK {
					g.sendWin.RecvAckSegment(m.WinSize(),m.AckN())
					continue
				}
				panic("no handler")
			}

		}()

	})
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
	//panic("implement me")
	return nil
}

func (g *GConn) LocalAddr() net.Addr {
	panic("implement me")
}

func (g *GConn) RemoteAddr() net.Addr {
	panic("implement me")
}

func (g *GConn) SetDeadline(t time.Time) error {
	panic("implement me")
}

func (g *GConn) SetReadDeadline(t time.Time) error {
	panic("implement me")
}

func (g *GConn) SetWriteDeadline(t time.Time) error {
	panic("implement me")
}
