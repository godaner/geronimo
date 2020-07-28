package v2

import (
	"github.com/godaner/geronimo/rule"
	v1 "github.com/godaner/geronimo/rule/v1"
	"log"
	"math/rand"
)

type messageHandler func(m *v1.Message) (err error)

// payloadMessageHandler
func (g *GConn) payloadMessageHandler(m *v1.Message) (err error) {
	log.Println("GConn#payloadMessageHandler : status is",g.s)
	if g.recvWin==nil{
		log.Println("GConn#payloadMessageHandler : recvWin is nil")
		return
	}
	return g.recvWin.RecvSegment(m.SeqN(), m.AttributeByType(rule.AttrPAYLOAD))
}

// syn1MessageHandler
func (g *GConn) syn1MessageHandler(m *v1.Message) (err error) {
	g.synSeqX = m.SeqN()
	if g.synSeqY == 0 {
		g.synSeqY = uint32(rand.Int31n(2<<16 - 2))
	}
	g.initWin()
	m1 := &v1.Message{}
	m1.SYN2(g.synSeqY, g.synSeqX+1)
	err = g.sendMessage(m1)
	if err != nil {
		log.Println("GConn#syn1MessageHandler : sendMessage1 err", err)
	}
	g.s = StatusEstablished
	g.lis.acceptResult <- &acceptRes{c: g, err: nil}
	return nil
}

// syn2MessageHandler
func (g *GConn) syn2MessageHandler(m *v1.Message) (err error) {
	g.synSeqY = m.SeqN()
	if g.synSeqX+1 != m.AckN() {
		log.Println("GConn#syn2MessageHandler : syncack seq x != ack")
		return
	}
	select {
	case g.syn1Finish <- true:
	default:
		log.Println("GConn#syn2MessageHandler : there are no syn1Finish suber")
		return
	}
	g.initWin()
	g.s = StatusEstablished
	return nil
}

// fin1MessageHandler
func (g *GConn) fin1MessageHandler(m *v1.Message) (err error) {
	g.finSeqU = m.SeqN()
	if g.finSeqV == 0 {
		g.finSeqV = uint32(rand.Int31n(2<<16 - 2))
	}
	if g.sendWin != nil {
		g.sendWin.Close(false)
	}
	if g.recvWin != nil {
		g.recvWin.Close()
	}
	m.FIN2(g.finSeqV, g.finSeqU+1)
	err = g.sendMessage(m)
	if err != nil {
		log.Println("GConn#fin1MessageHandler : sendMessage1 err", err)
	}
	g.closeUDPConn()
	g.s = StatusClosed
	return nil
}

// fin2MessageHandler
func (g *GConn) fin2MessageHandler(m *v1.Message) (err error) {
	g.finSeqV = m.SeqN()
	if m.AckN()-1 != g.finSeqU {
		log.Println("GConn#fin2MessageHandler : ack != sed u", m.AckN()-1, g.finSeqU)
		return
	}
	select {
	case g.fin1Finish <- true:
	default:
		log.Println("GConn#fin2MessageHandler : there are no fin1Finish suber")
		return
	}
	g.recvWin.Close()
	g.closeUDPConn()
	g.s = StatusClosed
	return nil
}

// ackMessageHandler
func (g *GConn) ackMessageHandler(m *v1.Message) (err error) {
	log.Println("GConn＃ackMessageHandler : status is",g.s)
	if g.sendWin==nil{
		log.Println("GConn＃ackMessageHandler : sendWin is nil")
		return
	}
	return g.sendWin.RecvAckSegment(m.WinSize(), m.AckN())
}