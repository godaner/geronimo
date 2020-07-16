package net

import (
	"encoding/binary"
	"github.com/godaner/geronimo/rule"
	v1 "github.com/godaner/geronimo/rule/v1"
	v2 "github.com/godaner/geronimo/win/v2"
	"net"
	"sync"
	"time"
)

type GConn struct {
	recvWin *v2.RWND
	sendWin *v2.SWND
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
		g.recvWin = &v2.RWND{
			AckCallBack: func(ack, receiveWinSize uint16) (err error) {
				m := &v1.Message{}
				m.ACK(ack, receiveWinSize)
				b := m.Marshall()
				length := make([]byte, 4, 4)
				binary.BigEndian.PutUint32(length, uint32(len(b)))
				//b = append(length, b...)
				_, err = g.UDPConn.Write(length)
				_, err = g.UDPConn.Write(b)
				return err
			},
		}
		g.sendWin = &v2.SWND{
			WriterCallBack: func(firstSeq uint16, bs []byte) (err error) {
				// send udp
				m := &v1.Message{}
				m.PAYLOAD(firstSeq, bs)
				b := m.Marshall()
				length := make([]byte, 4, 4)
				binary.BigEndian.PutUint32(length, uint32(len(b)))
				//b = append(length, b...)
				_, err = g.UDPConn.Write(length)
				_, err = g.UDPConn.Write(b)
				return err
			},
		}
		go func() {
			// recv udp
			l := make([]byte, 4, 4)
			for {
				//_,err:=io.ReadFull(g.UDPConn,l)
				//_,err:=g.UDPConn.Read(l)
				_,_,err:=g.UDPConn.ReadFrom(l)
				if err!=nil{
					panic(err)
				}
				length := binary.BigEndian.Uint32(l)
				bs := make([]byte, length, length)
				//_, err =io.ReadFull(g.UDPConn,bs)
				//_,err=g.UDPConn.Read(bs)
				_,_,err=g.UDPConn.ReadFrom(bs)
				if err!=nil{
					panic(err)
				}
				m := &v1.Message{}
				m.UnMarshall(bs)

				if m.Flag()&rule.FlagPAYLOAD == rule.FlagPAYLOAD {
					g.recvWin.Recv(m.SeqN(), m.AttributeByType(rule.AttrPAYLOAD))
				}
				if m.Flag()&rule.FlagACK == rule.FlagACK {
					g.sendWin.SetRecvSendWinSize(m.WinSize())
					g.sendWin.Ack(m.AckN())
				}
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
	panic("implement me")
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
