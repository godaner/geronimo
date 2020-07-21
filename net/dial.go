package net

import (
	"github.com/godaner/geronimo/rule"
	v1 "github.com/godaner/geronimo/rule/v1"
	"math/rand"
	"net"
	"time"
)

func Dial(raddr *net.UDPAddr) (c *GConn, err error) {
	conn, err := net.DialUDP("udp", nil, raddr)
	gc := &GConn{
		UDPConn: conn,
	}
	gc.init()
	err = gc.dial()
	if err != nil {
		return nil, err
	}
	return gc, nil
}
func (g *GConn) dial() (err error) {
	// sync req
	m1 := &v1.Message{}
	seq := uint32(rand.Int31n(2<<31 - 2))
	m1.SYN(seq)
	b := m1.Marshall()
	_, err = g.UDPConn.Write(b)
	if err != nil {
		return err
	}
	result := make(chan struct {
		err error
		bs  []byte
	})
	// wait ack
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
			m3 := &v1.Message{}
			if seq+1 != m3.AckN() {
				return errors.New("error ack number")
			}
			m3.ACK(m2.SeqN()+1, 0)
			b := m3.Marshall()
			_, err = g.UDPConn.Write(b)
			if err != nil {
				return err
			}
		} else {
			return errors.New("sync err")
		}
	case <-time.After(time.Duration(syncTimeout) * time.Millisecond): // wait ack timeout
		return errors.New("sync timeout")
	}

	return nil
}
