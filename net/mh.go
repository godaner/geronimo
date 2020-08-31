package net

import (
	"errors"
	"github.com/godaner/geronimo/protocol"
	msg "github.com/godaner/geronimo/protocol"
	"time"
)

type messageHandler func(m msg.Message) (err error)

// syn1MessageHandler
func (g *GConn) syn1MessageHandler(m msg.Message) (err error) {
	g.logger.Notice("GConn#syn1MessageHandler : recv SYN1 start")
	//g.s = StatusEstablished
	err = g.fsm.Event(EventSerRecvSyn1, m)
	if err != nil {
		g.logger.Error("GConn#syn1MessageHandler : status err", err)
		return err
	}
	return nil
}

// syn2MessageHandler
func (g *GConn) syn2MessageHandler(m msg.Message) (err error) {
	g.logger.Notice("GConn#syn2MessageHandler : recv  SYN2 start")
	g.synSeqY = m.SeqN()
	if g.synSeqX+1 != m.AckN() {
		g.logger.Error("GConn#syn2MessageHandler : syncack seq x != ack")
		return
	}
	select {
	case g.syn1Finish <- struct{}{}:
	default:
		g.logger.Warning("GConn#syn2MessageHandler : there are no syn1Finish suber")
		return
	}
	return nil
}

// fin1MessageHandler
func (g *GConn) fin1MessageHandler(m msg.Message) (err error) {
	go func() {
		err = g.fsm.Event(EventRecvFin1, m)
		if err != nil {
			g.logger.Error("GConn#fin1MessageHandler : status err", err)
			return
		}
	}()
	return nil
}

// fin2MessageHandler
func (g *GConn) fin2MessageHandler(m msg.Message) (err error) {
	g.logger.Notice("GConn#fin2MessageHandler : recv FIN2 start")
	g.finSeqV = m.SeqN()
	if m.AckN()-1 != g.finSeqU {
		g.logger.Error("GConn#fin2MessageHandler : ack != sed u", m.AckN()-1, g.finSeqU)
		return
	}
	select {
	case g.fin1Finish <- struct{}{}:
	default:
		g.logger.Warning("GConn#fin2MessageHandler : there are no fin1Finish suber")
		return
	}
	return nil
}

// ackMessageHandler
func (g *GConn) ackMessageHandler(m msg.Message) (err error) {
	g.logger.Debug("GConn#ackMessageHandler : start")
	if g.sendWin == nil {
		g.logger.Error("GConn＃ackMessageHandler : sendWin is nil")
		g.Close()
		return
	}
	return g.sendWin.RecvAck(m.SeqN(), m.AckN(), m.WinSize())
}

// payloadMessageHandler
func (g *GConn) payloadMessageHandler(m msg.Message) (err error) {
	g.logger.Debug("GConn#payloadMessageHandler : start")
	if g.recvWin == nil {
		g.logger.Error("GConn#payloadMessageHandler : recvWin is nil")
		g.Close()
		return
	}
	return g.recvWin.Recv(m.SeqN(), m.AttributeByType(protocol.AttrPAYLOAD))
}

// keepaliveMessageHandler
func (g *GConn) keepaliveMessageHandler(m msg.Message) (err error) {
	g.logger.Debug("GConn#keepaliveMessageHandler : start")
	select {
	case g.keepaliveC <- struct{}{}:
		return nil
	case <-time.After(time.Duration(100) * time.Millisecond):
		return errors.New("no keepalive handler")
	}
}
