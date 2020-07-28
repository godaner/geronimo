package v1

import (
	"errors"
	"net"
	"time"
)

const (
	dialRetryTime     = 4
	dialRetryInterval = 2000
)

func Dial(raddr *GAddr) (c *GConn, err error) {
	var conn *net.UDPConn
	err = errors.New("try dial")
	ret := 0
	for {
		if err == nil {
			break
		}
		if ret >= dialRetryTime {
			return nil, errors.New("dial err")
		}
		conn, err = net.DialUDP("udp", nil, raddr.toUDPAddr())
		ret++
		time.After(time.Duration(dialRetryInterval) * time.Microsecond)
	}
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
