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

// fin1MessageHandler
func (g *GConn) fin1MessageHandler(m *v1.Message) {
	g.fin1SeqU = m.SeqN()
	g.fin1SeqV = uint32(rand.Int31n(2<<16 - 2))
	m.FIN2(g.fin1SeqV, g.fin1SeqU+1)
	err := g.sendMessage(m)
	if err != nil {
		log.Println("GConn#fin1MessageHandler : handleMessage ACKN StatusCloseWait err", err)
	}
	g.s = StatusCloseWait
	go func() {
		g.sendWin.Close()
		g.fin2SeqW = uint32(rand.Int31n(2<<16 - 2))
		m.FIN3(g.fin2SeqW, g.fin1SeqU+1)
		err := g.sendMessage(m)
		if err != nil {
			log.Println("GConn#fin1MessageHandler : handleMessage ACKN StatusLastAck err", err)
		}
		g.s = StatusLastAck
	}()
}

// fin2MessageHandler
func (g *GConn) fin2MessageHandler(m *v1.Message) {
	g.fin1SeqV = m.SeqN()
	if m.AckN()-1 != g.fin1SeqU {
		log.Println("GConn#fin2MessageHandler : ack != sed u", m.AckN()-1, g.fin1SeqU)
		return
	}
	g.s = StatusFinWait2
}

// fin3MessageHandler
func (g *GConn) fin3MessageHandler(m *v1.Message) {
	g.fin2SeqW = m.SeqN()
	if m.AckN()-1 != g.fin1SeqU {
		log.Println("GConn#fin3MessageHandler : ack != seq u", m.AckN()-1, g.fin1SeqU)
		return
	}
	m.FIN4(g.fin1SeqU+1, g.fin2SeqW+1)
	err := g.sendMessage(m)
	if err != nil {
		log.Println("GConn#fin3MessageHandler : send message err", err)
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

// fin4MessageHandler
func (g *GConn) fin4MessageHandler(m *v1.Message) {
	if m.AckN()-1 != g.fin2SeqW {
		log.Println("GConn#fin4MessageHandler : ack != seq w", m.AckN()-1, g.fin2SeqW)
		return
	}
	if m.SeqN()-1 != g.fin1SeqU {
		log.Println("GConn#fin4MessageHandler : seq != seq u", m.SeqN()-1, g.fin1SeqU)
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

// syn2MessageHandler
func (g *GConn) syn2MessageHandler(m *v1.Message) {
	g.synSeqY = m.SeqN()
	if g.synSeqX+1 != m.AckN() {
		log.Println("GConn#syn2MessageHandler : syncack seq x != ack")
		g.dialFinish <- errors.New("syncack err")
		return
	}
	m1 := &v1.Message{}
	m1.SYN3(g.synSeqX+1, g.synSeqY+1)
	err := g.sendMessage(m1)
	if err != nil {
		log.Println("GConn#syn2MessageHandler : Write err", err)
		g.dialFinish <- errors.New("ack err")
		return
	}
	g.dialFinish <- nil
	g.s = StatusEstablished
}

// syn3MessageHandler
func (g *GConn) syn3MessageHandler(m *v1.Message) {
	if g.synSeqX+1 != m.SeqN() {
		log.Println("GConn#syn3MessageHandler : seq x != m.seq")
		return
	}
	if g.synSeqY+1 != m.AckN() {
		log.Println("GConn#syn3MessageHandler : seq y != m.ack")
		return
	}
	g.s = StatusEstablished
	g.lis.acceptResult <- &acceptRes{c: g, err: nil}
}

// syn1MessageHandler
func (g *GConn) syn1MessageHandler(m *v1.Message) {
	g.synSeqX = m.SeqN()
	g.synSeqY = uint32(rand.Int31n(2<<16 - 2))
	m1 := &v1.Message{}
	m1.SYN2(g.synSeqY, g.synSeqX+1)
	err := g.sendMessage(m1)
	if err != nil {
		log.Println("GConn#syn1MessageHandler : Write err", err)
		return
	}
	g.s = StatusSynRecved
}
