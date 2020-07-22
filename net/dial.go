package net

import (
	"errors"
	"fmt"
	"github.com/godaner/geronimo/rule"
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
	// sync req
	m1 := &v1.Message{}
	seq := uint32(rand.Int31n(2<<16 - 2))
	m1.SYN(seq)
	b := m1.Marshall()
	_, err = g.UDPConn.Write(b)
	if err != nil {
		return err
	}
	g.s = StatusSynSent
	// wait ack
	result := make(chan struct {
		err error
		bs  []byte
	})
	go func() {
		bs := make([]byte, udpmss, udpmss)
		_, err = g.UDPConn.Read(bs)
		result <- struct {
			err error
			bs  []byte
		}{err: err, bs: bs}
	}()
	select {
	case r := <-result: // send ack
		if r.err != nil {
			return err
		}
		m2 := &v1.Message{}
		m2.UnMarshall(r.bs)
		if m2.Flag()&rule.FlagACK == rule.FlagACK && m2.Flag()&rule.FlagSYN == rule.FlagSYN {
			if seq+1 != m2.AckN() {
				return errors.New("error ack number , seq is" + fmt.Sprint(seq) + ", ack is " + fmt.Sprint(m2.AckN()))
			}
			m3 := &v1.Message{}
			m3.ACK(m2.SeqN()+1, 0)
			b := m3.Marshall()
			_, err = g.UDPConn.Write(b)
			if err != nil {
				return err
			}
			g.s = StatusEstablished
		} else {
			return errors.New("sync err")
		}
	case <-time.After(time.Duration(syncTimeout) * time.Millisecond): // wait ack timeout
		return errors.New("sync timeout")
	}

	return nil
}
