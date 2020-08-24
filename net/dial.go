package net

import (
	"github.com/godaner/geronimo/rule/fac"
	"net"
)

func Dial(raddr *GAddr, options ...Option) (c *GConn, err error) {
	opts := &Options{}
	for _, o := range options {
		o(opts)
	}
	var conn *net.UDPConn
	conn, err = net.DialUDP("udp", nil, raddr.toUDPAddr())
	if err != nil {
		return nil, err
	}
	gc := &GConn{
		UDPConn:  conn,
		MsgFac:   &fac.Fac{Enc: opts.Enc},
		laddr:    fromUDPAddr(conn.LocalAddr().(*net.UDPAddr)),
		raddr:    raddr,
		f:        FDial,
	}
	err = gc.dial()
	if err != nil {
		return nil, err
	}
	return gc, nil
}
