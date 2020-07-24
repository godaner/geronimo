package net

import (
	"errors"
	"github.com/godaner/geronimo/rule"
	v1 "github.com/godaner/geronimo/rule/v1"
	"log"
	"math/rand"
	"time"
)

type messageHandler func(m *v1.Message)

// payloadMessageHandler
func (g *GConn) payloadMessageHandler(m *v1.Message) {
	g.recvWin.RecvSegment(m.SeqN(), m.AttributeByType(rule.AttrPAYLOAD))
}

// finMessageHandler
func (g *GConn) finMessageHandler(m *v1.Message) {
	g.fin1SeqU = m.SeqN()
	g.fin1SeqV = uint32(rand.Int31n(2<<16 - 2))
	m.ACKN(g.fin1SeqV, g.fin1SeqU+1, 0)
	err := g.sendMessage(m)
	if err != nil {
		log.Println("GConn : handleMessage ACKN StatusCloseWait err", err)
	}
	g.s = StatusCloseWait
	go func() {
		g.sendWin.Close()
		g.fin2SeqW = uint32(rand.Int31n(2<<16 - 2))
		m.FINACK(g.fin2SeqW, g.fin1SeqU+1, 0)
		err := g.sendMessage(m)
		if err != nil {
			log.Println("GConn : handleMessage ACKN StatusLastAck err", err)
		}
		g.s = StatusLastAck
	}()
}

// fin1AckMessageHandler
func (g *GConn) fin1AckMessageHandler(m *v1.Message) {
	g.fin1SeqV = m.SeqN()
	if m.AckN()-1 != g.fin1SeqU {
		log.Println("GConn : handleMessage fin1SeqU StatusFinWait2 err", m.AckN()-1, g.fin1SeqU)
		return
	}
	g.s = StatusFinWait2
}

// fin2AckMessageHandler
func (g *GConn) fin2AckMessageHandler(m *v1.Message) {
	g.fin2SeqW = m.SeqN()
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
		g.closeFinish <- true
		<-time.After(time.Duration(2*msl) * time.Second)
		g.recvWin.Close()
		g.rmFromLis()
		g.s = StatusClosed
	}()
}

// finLastAckMessageHandler
func (g *GConn) finLastAckMessageHandler(m *v1.Message) {
	if m.AckN()-1 != g.fin2SeqW {
		log.Println("GConn : handleMessage fin2SeqW StatusLastAck err", m.AckN()-1, g.fin2SeqW)
		return
	}
	if m.SeqN()-1 != g.fin1SeqU {
		log.Println("GConn : handleMessage fin1SeqU StatusLastAck err", m.SeqN()-1, g.fin1SeqU)
		return
	}
	g.rmFromLis()
	g.recvWin.Close()
	g.s = StatusClosed
	return
}

// ackMessageHandler
func (g *GConn) ackMessageHandler(m *v1.Message) {
	g.sendWin.RecvAckSegment(m.WinSize(), m.AckN())
	return
}

// connectSynAckMessageHandler
func (g *GConn) connectSynAckMessageHandler(m *v1.Message) {
	g.synSeqY = m.SeqN()
	if g.synSeqX+1 != m.AckN() {
		log.Println("GConn#connectSynAckMessageHandler : syncack seq x != ack")
		g.dialFinish <- errors.New("syncack err")
		return
	}
	m1 := &v1.Message{}
	m1.ACKN(g.synSeqX+1, g.synSeqY+1, 0)
	err := g.sendMessage(m1)
	if err != nil {
		log.Println("GConn#connectSynAckMessageHandler : Write err", err)
		g.dialFinish <- errors.New("ack err")
		return
	}
	g.dialFinish <- nil
	g.s = StatusEstablished
}

// connectAckMessageHandler
func (g *GConn) connectAckMessageHandler(m *v1.Message) {
	if g.synSeqX+1 != m.SeqN() {
		log.Println("GConn#connectAckMessageHandler : seq x != m.seq")
		return
	}
	if g.synSeqY+1 != m.AckN() {
		log.Println("GConn#connectAckMessageHandler : seq y != m.ack")
		return
	}
	g.s = StatusEstablished
	g.lis.acceptResult<-&acceptRes{c:g,err:nil}
}

// connectSynMessageHandler
func (g *GConn) connectSynMessageHandler(m *v1.Message) {
	g.synSeqX = m.SeqN()
	g.synSeqY = uint32(rand.Int31n(2<<16 - 2))
	m1 := &v1.Message{}
	m1.SYNACK(g.synSeqY, g.synSeqX+1, 0)
	err := g.sendMessage(m1)
	if err != nil {
		log.Println("GConn#connectSynMessageHandler : Write err", err)
		return
	}
	g.s = StatusSynRecved
}
