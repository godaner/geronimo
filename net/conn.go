package net

import (
	"net"
	"time"
)

type GConn struct {

}

func (G GConn) Read(b []byte) (n int, err error) {
	panic("implement me")
}

func (G GConn) Write(b []byte) (n int, err error) {
	panic("implement me")
}

func (G GConn) Close() error {
	panic("implement me")
}

func (G GConn) LocalAddr() net.Addr {
	panic("implement me")
}

func (G GConn) RemoteAddr() net.Addr {
	panic("implement me")
}

func (G GConn) SetDeadline(t time.Time) error {
	panic("implement me")
}

func (G GConn) SetReadDeadline(t time.Time) error {
	panic("implement me")
}

func (G GConn) SetWriteDeadline(t time.Time) error {
	panic("implement me")
}
