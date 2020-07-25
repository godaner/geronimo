package net

import (
	"errors"
	v1 "github.com/godaner/geronimo/rule/v1"
	"math/rand"
	"net"
	"time"
)

func Dial(raddr *GAddr) (c *GConn, err error) {
	conn, err := net.DialUDP("udp", nil, raddr.toUDPAddr()) // new local port
	gc := &GConn{
		UDPConn: conn,
		laddr:   fromUDPAddr(conn.LocalAddr().(*net.UDPAddr)),
		raddr:   raddr,
		f:       FDial,
	}
	//gc.init()
	err = gc.dial()
	if err != nil {
		return nil, err
	}
	return gc, nil
}
func (g *GConn) dial() (err error) {
	g.init()
	// sync req
	m1 := &v1.Message{}
	g.synSeqX = uint32(rand.Int31n(2<<16 - 2))
	m1.SYN1(g.synSeqX)
	err = g.sendMessage(m1)
	if err != nil {
		return err
	}
	g.s = StatusSynSent
	select {
	case err := <-g.dialFinish:
		return err
	case <-time.After(time.Duration(syncTimeout) * time.Millisecond): // wait ack timeout
		return errors.New("sync timeout")
	}
}
