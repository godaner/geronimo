package net

import (
	"github.com/godaner/geronimo/rule"
	v1 "github.com/godaner/geronimo/rule/v1"
	v12 "github.com/godaner/geronimo/win/v1"
	"log"
	"math/rand"
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
	f                uint8
	s                Status
	fromListenerData chan *v1.Message
	recvWin          *v12.RWND
	sendWin          *v12.SWND
	raddr            *GAddr
	laddr            *GAddr
	lis              *GListener
	//finAck           chan bool
	fin1SeqU uint32
	fin2SeqW uint32
}

func (g *GConn) init() {
	g.Do(func() {
		g.fromListenerData = make(chan *v1.Message)
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
	if m.Flag()&rule.FlagPAYLOAD == rule.FlagPAYLOAD {
		g.recvWin.RecvSegment(m.SeqN(), m.AttributeByType(rule.AttrPAYLOAD))
		return
	}
	if m.Flag()&rule.FlagACK == rule.FlagACK {
		g.sendWin.RecvAckSegment(m.WinSize(), m.AckN())
		return
	}
	if g.s == StatusEstablished && m.Flag()&rule.FlagFIN == rule.FlagFIN {
		g.recvWin.Close()
		seq := uint32(rand.Int31n(2<<16 - 2))
		m.ACKN(m.SeqN()+1, seq, 0)
		err := g.sendMessage(m)
		if err != nil {
			log.Println("GConn : handleMessage ACKN StatusCloseWait err", err)
		}
		g.s = StatusCloseWait
		go func() {
			g.sendWin.Close()
			<-g.sendWin.CloseFinish()
			g.s = StatusLastAck
			g.fin2SeqW = uint32(rand.Int31n(2<<16 - 2))
			m.FINACK(g.fin2SeqW, g.fin1SeqU+1, 0)
			err := g.sendMessage(m)
			if err != nil {
				log.Println("GConn : handleMessage ACKN StatusLastAck err", err)
			}
		}()
		return
	}
	if g.s == StatusFinWait1 && m.Flag()&rule.FlagACK == rule.FlagACK {
		if m.AckN()-1 != g.fin1SeqU {
			log.Println("GConn : handleMessage fin1SeqU StatusFinWait2 err", m.AckN()-1, g.fin1SeqU)
			return
		}
		g.s = StatusFinWait2
		return
	}
	if g.s == StatusFinWait2 && m.Flag()&rule.FlagACK == rule.FlagACK && m.Flag()&rule.FlagFIN == rule.FlagFIN {
		if m.AckN()-1 != g.fin1SeqU {
			log.Println("GConn : handleMessage fin1SeqU StatusTimeWait err", m.AckN()-1, g.fin1SeqU)
			return
		}
		m.ACKN(g.fin1SeqU+1, g.fin2SeqW+1, 0)
		err := g.sendMessage(m)
		if err != nil {
			log.Println("GConn : handleMessage ACKN StatusTimeWait err", err)
		}
		g.s = StatusTimeWait
		go func() {
			<-time.After(time.Duration(2*msl) * time.Second)
			g.recvWin.Close()
			g.rmFromLis()
			g.s = StatusClosed
		}()
		return
	}
	if g.s == StatusLastAck && m.Flag()&rule.FlagACK == rule.FlagACK {
		if m.AckN()-1 != g.fin2SeqW {
			log.Println("GConn : handleMessage fin2SeqW StatusLastAck err", m.AckN()-1, g.fin2SeqW)
			return
		}
		if m.SeqN()-1 != g.fin1SeqU {
			log.Println("GConn : handleMessage fin1SeqU StatusLastAck err", m.SeqN()-1, g.fin1SeqU)
			return
		}
		g.rmFromLis()
		g.s = StatusClosed
		return
	}
	panic("no handler")
}
func (g *GConn) rmFromLis() {
	if g.lis == nil {
		return
	}
	g.lis.gcs.Delete(g.raddr.toUDPAddr().String())
}
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
	return g.close() // todo how to close ???
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
	m := &v1.Message{}
	fin1Seq := uint32(rand.Int31n(2<<16 - 2))
	m.FIN(fin1Seq)
	err = g.sendMessage(m)
	if err != nil {
		return err
	}
	g.sendWin.Close()
	g.s = StatusFinWait1
	return nil
}
