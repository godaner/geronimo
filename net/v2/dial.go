package v2

import (
	"log"
	"net"
)


func Dial(raddr *GAddr) (c *GConn, err error) {
	var conn *net.UDPConn
	conn, err = net.DialUDP("udp", nil, raddr.toUDPAddr())
	if err!=nil{
		log.Println("Dial : udp dial err",err)
		return nil,err
	}

	//log.Println("Dial : udp dial success , , local addr is ",conn.LocalAddr().String(),", remote addr is",conn.RemoteAddr().String())
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
