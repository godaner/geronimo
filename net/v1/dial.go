package v1

import (
	"net"
)


func Dial(raddr *GAddr) (c *GConn, err error) {
	var conn *net.UDPConn
	conn, err = net.DialUDP("udp", nil, raddr.toUDPAddr())
	if err!=nil{
		return nil,err
	}
	gc := &GConn{
		UDPConn: conn,
		laddr:   fromUDPAddr(conn.LocalAddr().(*net.UDPAddr)),
		raddr:   raddr,
		f:       FDial,
	}
	err = gc.dial()
	if err != nil {
		return nil, err
	}
	return gc, nil
}
