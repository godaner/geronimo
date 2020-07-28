package net

import (
	"fmt"
	"github.com/godaner/geronimo/rule"
	v1 "github.com/godaner/geronimo/rule/v1"
	"log"
	"math/rand"
	"time"
)

type messageHandler func(m *v1.Message) (err error)

// payloadMessageHandler
func (g *GConn) payloadMessageHandler(m *v1.Message) (err error) {
	return g.recvWin.RecvSegment(m.SeqN(), m.AttributeByType(rule.AttrPAYLOAD))
}

// syn1MessageHandler
func (g *GConn) syn1MessageHandler(m *v1.Message) (err error) {
	g.synSeqX = m.SeqN()
	if g.synSeqY == 0 {
		g.synSeqY = uint32(rand.Int31n(2<<16 - 2))
	}
	m1 := &v1.Message{}
	m1.SYN2(g.synSeqY, g.synSeqX+1)
	err = g.sendMessage(m1)
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
	return nil
}

// syn2MessageHandler
func (g *GConn) syn2MessageHandler(m *v1.Message) (err error) {
	g.synSeqY = m.SeqN()
	if g.synSeqX+1 != m.AckN() {
		log.Println("GConn#syn2MessageHandler : syncack seq x != ack")
		return
	}
	m1 := &v1.Message{}
	m1.SYN3(g.synSeqX+1, g.synSeqY+1)
	err = g.sendMessage(m1)
	if err != nil {
		log.Println("GConn#syn2MessageHandler : Write err", err)
		return
	}
	select {
	case g.syn1Finish <- true:
	default:
		log.Println("GConn#syn2MessageHandler : there are no syn1Finish suber")
		return
	}
	g.s = g.maxStatus(StatusEstablished, g.s)
	return nil
}

// syn3MessageHandler
func (g *GConn) syn3MessageHandler(m *v1.Message) (err error) {
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
		return
	}
	g.s = StatusEstablished
	g.lis.acceptResult <- &acceptRes{c: g, err: nil}
	return nil
}

// fin1MessageHandler
func (g *GConn) fin1MessageHandler(m *v1.Message) (err error) {
	g.finSeqU = m.SeqN()
	if g.finSeqV == 0 {
		g.finSeqV = uint32(rand.Int31n(2<<16 - 2))
	}
	m.FIN2(g.finSeqV, g.finSeqU+1)
	err = g.sendMessage(m)
	if err != nil {
		log.Println("GConn#fin1MessageHandler : sendMessage1 err", err)
	}
	g.s = StatusCloseWait
	go func() {
		g.sendWin.Close()
		if g.finSeqW == 0 {
			g.finSeqW = uint32(rand.Int31n(2<<16 - 2))
		}
		//fmt.Printf("fin1 %v,%p\n",g.finSeqW,g)
		m.FIN3(g.finSeqW, g.finSeqU+1)
		err := g.sendMessage(m)
		if err != nil {
			log.Println("GConn#fin1MessageHandler : sendMessage2 err", err)
		}
		g.s = StatusLastAck
		for {
			if g.fin3RetryTime >= fin3RetryTime {
				log.Println("GConn#fin1MessageHandler : fin3 fail , retry time is :" + fmt.Sprint(g.fin3RetryTime))
				// todo
				return
			}
			select {
			case <-g.fin3Finish:
				return
			case <-time.After(time.Duration(fin3Timeout) * time.Millisecond): // wait ack timeout
				err = g.sendMessage(m)
				if err != nil {
					log.Println("GConn#fin1MessageHandler sendMessage3 err , err is", err)
				}
				g.fin3RetryTime++
			}
		}
	}()
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
	g.s = StatusFinWait2
	return nil
}

// fin3MessageHandler
func (g *GConn) fin3MessageHandler(m *v1.Message) (err error) {
	g.finSeqW = m.SeqN()
	if m.AckN()-1 != g.finSeqU {
		log.Println("GConn#fin3MessageHandler : ack != seq u", m.AckN()-1, g.finSeqU)
		return
	}
	m.FIN4(g.finSeqU+1, g.finSeqW+1)
	err = g.sendMessage(m)
	if err != nil {
		log.Println("GConn#fin3MessageHandler : send message err", err)
	}
	g.recvWin.Close()
	g.s = StatusTimeWait
	go func() {
		<-time.After(time.Duration(2*msl) * time.Second)
		g.closeUDPConn()
		g.s = StatusClosed
	}()
	return nil
}

// fin4MessageHandler
func (g *GConn) fin4MessageHandler(m *v1.Message) (err error) {
	//fmt.Printf("fin4 %v,%p\n",g.finSeqW,g)
	if m.AckN()-1 != g.finSeqW {
		log.Println("GConn#fin4MessageHandler : ack != seq w", m.AckN()-1, g.finSeqW)
		return
	}
	if m.SeqN()-1 != g.finSeqU {
		log.Println("GConn#fin4MessageHandler : seq != seq u", m.SeqN()-1, g.finSeqU)
		return
	}
	select {
	case g.fin3Finish <- true:
	default:
		log.Println("GConn#fin4MessageHandler : there are no fin3Finish suber")
		return
	}
	g.recvWin.Close()
	g.closeUDPConn()
	g.s = StatusClosed
	return nil
}

// ackMessageHandler
func (g *GConn) ackMessageHandler(m *v1.Message) (err error) {
	return g.sendWin.RecvAckSegment(m.WinSize(), m.AckN())
}
