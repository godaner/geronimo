package net

import (
	"fmt"
	"net"
)

type GAddr struct {
	IP   string
	Port int
}

func (g *GAddr) Network() string {
	return "geronimo"
}

func (g *GAddr) String() string {
	return fmt.Sprintf("%v:%v", g.IP, g.Port)
}

func (g *GAddr) toUDPAddr() *net.UDPAddr {

	return &net.UDPAddr{
		IP:   net.ParseIP(g.IP),
		Port: g.Port,
		Zone: "",
	}
}
func fromUDPAddr(udpAddr *net.UDPAddr) *GAddr {
	return &GAddr{
		IP:   udpAddr.IP.String(),
		Port: udpAddr.Port,
	}
}
