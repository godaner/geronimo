package net

import (
	"github.com/godaner/geronimo/rule"
	v1 "github.com/godaner/geronimo/rule/v1"
)

type messageHandler func(m *v1.Message) (err error)

// syn1MessageHandler
func (g *GConn) syn1MessageHandler(m *v1.Message) (err error) {
	g.logger.Notice("GConn#syn1MessageHandler : recv SYN1 start")
	g.s = StatusEstablished
	g.synSeqX = m.SeqN()
	if g.synSeqY == 0 {
		g.synSeqY = g.random()
	}
	g.initWin()
	m1 := &v1.Message{}
	m1.SYN2(g.synSeqY, g.synSeqX+1)
	err = g.sendMessage(m1)
	if err != nil {
		g.logger.Error("GConn#syn1MessageHandler : sendMessage1 err", err)
	}
	g.lis.acceptResult <- &acceptRes{c: g, err: nil}
	return nil
}

// syn2MessageHandler
func (g *GConn) syn2MessageHandler(m *v1.Message) (err error) {
	g.logger.Notice("GConn#syn2MessageHandler : recv  SYN2 start")
	g.synSeqY = m.SeqN()
	if g.synSeqX+1 != m.AckN() {
		g.logger.Error("GConn#syn2MessageHandler : syncack seq x != ack")
		return
	}
	select {
	case g.syn1Finish <- true:
	default:
		g.logger.Warning("GConn#syn2MessageHandler : there are no syn1Finish suber")
		return
	}
	return nil
}

// fin1MessageHandler
func (g *GConn) fin1MessageHandler(m *v1.Message) (err error) {
	go func() {
		g.s = StatusClosed
		g.logger.Notice("GConn#fin1MessageHandler : recv FIN1 start")
		g.finSeqU = m.SeqN()
		if g.finSeqV == 0 {
			g.finSeqV = g.random()
		}
		g.closeWin()
		g.logger.Debug("GConn#fin1MessageHandler : close win finish")
		m.FIN2(g.finSeqV, g.finSeqU+1)
		err = g.sendMessage(m)
		if err != nil {
			g.logger.Error("GConn#fin1MessageHandler : sendMessage1 err", err)
		}
		g.closeUDPConn()
	}()
	return nil
}

// fin2MessageHandler
func (g *GConn) fin2MessageHandler(m *v1.Message) (err error) {
	g.logger.Notice("GConn#fin2MessageHandler : recv FIN2 start")
	g.finSeqV = m.SeqN()
	if m.AckN()-1 != g.finSeqU {
		g.logger.Error("GConn#fin2MessageHandler : ack != sed u", m.AckN()-1, g.finSeqU)
		return
	}
	select {
	case g.fin1Finish <- true:
	default:
		g.logger.Warning("GConn#fin2MessageHandler : there are no fin1Finish suber")
		return
	}
	return nil
}

// ackMessageHandler
func (g *GConn) ackMessageHandler(m *v1.Message) (err error) {
	g.logger.Debug("GConn#ackMessageHandler : start")
	if g.sendWin == nil {
		g.logger.Error("GConn＃ackMessageHandler : sendWin is nil")
		g.Close()
		return
	}
	return g.sendWin.RecvAck(m.SeqN(), m.AckN(),m.WinSize())
}

// payloadMessageHandler
func (g *GConn) payloadMessageHandler(m *v1.Message) (err error) {
	g.logger.Debug("GConn#payloadMessageHandler : start")
	if g.recvWin == nil {
		g.logger.Error("GConn#payloadMessageHandler : recvWin is nil")
		g.Close()
		return
	}
	return g.recvWin.Recv(m.SeqN(), m.AttributeByType(rule.AttrPAYLOAD))
}
