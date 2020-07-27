package net

import (
	"fmt"
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

// syn1MessageHandler
func (g *GConn) syn1MessageHandler(m *v1.Message) {
	g.synSeqX = m.SeqN()
	if g.synSeqY == 0 {
		g.synSeqY = uint32(rand.Int31n(2<<16 - 2))
	}
	m1 := &v1.Message{}
	m1.SYN2(g.synSeqY, g.synSeqX+1)
	err := g.sendMessage(m1)
	if err != nil {
		log.Println("GConn#syn1MessageHandler : sendMessage1 err", err)
	}
	g.s = g.maxStatus(StatusSynRecved, g.s)
	go func() {
		for {
			if g.syn2RetryTime >= syn2RetryTime {
				log.Println("GConn#syn1MessageHandler : syn2 fail , retry time is :" + fmt.Sprint(g.syn2RetryTime))
				g.lis.gcs.Delete(g.raddr.toUDPAddr().String())
				//g.Close()
				return
			}
			select {
			case <-g.syn2Finish:
				return
			case <-time.After(time.Duration(syn2Timeout) * time.Millisecond): // wait ack timeout
				err = g.sendMessage(m1)
				if err != nil {
					log.Println("GConn#syn1MessageHandler : sendMessage2 err", err)
				}
				g.syn2RetryTime++
			}
		}
	}()

}

// syn2MessageHandler
func (g *GConn) syn2MessageHandler(m *v1.Message) {
	g.synSeqY = m.SeqN()
	if g.synSeqX+1 != m.AckN() {
		log.Println("GConn#syn2MessageHandler : syncack seq x != ack")
		return
	}
	m1 := &v1.Message{}
	m1.SYN3(g.synSeqX+1, g.synSeqY+1)
	err := g.sendMessage(m1)
	if err != nil {
		log.Println("GConn#syn2MessageHandler : Write err", err)
		return
	}
	select {
	case g.syn1Finish <- true:
	default:
		log.Println("GConn#syn2MessageHandler : 3 there are no syn1Finish suber")
	}
	g.s = g.maxStatus(StatusEstablished, g.s)
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
	select {
	case g.syn2Finish <- true:
	default:
		log.Println("GConn#syn3MessageHandler : there are no syn2Finish suber")
	}
	g.s = StatusEstablished
	g.lis.acceptResult <- &acceptRes{c: g, err: nil}
}

// fin1MessageHandler
func (g *GConn) fin1MessageHandler(m *v1.Message) {
	g.fin1SeqU = m.SeqN()
	if g.fin1SeqV == 0 {
		g.fin1SeqV = uint32(rand.Int31n(2<<16 - 2))
	}
	m.FIN2(g.fin1SeqV, g.fin1SeqU+1)
	err := g.sendMessage(m)
	if err != nil {
		log.Println("GConn#fin1MessageHandler : handleMessage ACKN StatusCloseWait err", err)
	}
	g.s = StatusCloseWait
	go func() {
		g.sendWin.Close()
		if g.fin2SeqW == 0 {
			g.fin2SeqW = uint32(rand.Int31n(2<<16 - 2))
		}
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
