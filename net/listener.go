package net

import "net"

type GListener struct {

}

func (G *GListener) Accept() (net.Conn, error) {
	panic("implement me")
}

func (G *GListener) Close() error {
	panic("implement me")
}

func (G *GListener) Addr() net.Addr {
	panic("implement me")
}

