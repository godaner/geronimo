package net

import (
	"errors"
	"fmt"
	"github.com/godaner/geronimo/rule"
	v1 "github.com/godaner/geronimo/rule/v1"
	"log"
	"math/rand"
	"net"
	"sync"
	"time"
)

func Listen(addr *GAddr) (l net.Listener, err error) {
	c, err := net.ListenUDP("udp", addr.toUDPAddr())
	if err != nil {
		log.Println("Listen : ListenUDP err", err)
		return nil, err
	}
	return &GListener{
		laddr: addr,
		c:     c,
	}, nil
}

type GListener struct {
	sync.Once
	laddr        *GAddr
	c            *net.UDPConn
	gcs          *sync.Map //map[string]*GConn
	acceptResult chan *acceptRes
	estdResult   map[string]chan *estdRes
}
type estdRes struct {
	err error
	m   *v1.Message
}
type acceptRes struct {
	c   net.Conn
	err error
}

func (g *GListener) init() {
	g.Do(func() {
		g.acceptResult = make(chan *acceptRes)
		g.estdResult = map[string]chan *estdRes{}
		g.gcs = &sync.Map{} //map[string]*GConn{}
		go func() {
			bs := make([]byte, udpmss, udpmss)
			for {
				func() {
					n, rAddr, err := g.c.ReadFromUDP(bs)
					if err != nil {
						log.Println("GListener : ReadFromUDP err", err)
						return
					}
					//fmt.Println(rAddr)
					m1 := &v1.Message{}
					m1.UnMarshall(bs[:n])
					gcI, _ := g.gcs.Load(rAddr.String())
					gc, _ := gcI.(*GConn)
					if gc != nil {
						if gc.Status() >= StatusEstablished {
							gc.handleMessage(m1)
							return
						}
						if gc.Status() == StatusSynRecved && m1.Flag()&rule.FlagACK == rule.FlagACK {
							g.estdResult[rAddr.String()] <- &estdRes{
								err: nil,
								m:   m1,
							}
							return
						}
					}
					if m1.Flag()&rule.FlagSYN == rule.FlagSYN { // recv sync
						if gc != nil && gc.Status() != StatusSynRecved {
							log.Println("GListener : syn err , gc is exits")
							return
						}
						if gc == nil {
							// first connect
							gc = &GConn{
								UDPConn: g.c,
								raddr:   fromUDPAddr(rAddr),
								laddr:   fromUDPAddr(g.c.LocalAddr().(*net.UDPAddr)),
								s:       StatusSynRecved,
								f:       FListen,
								lis:     g,
							}
						}
						g.gcs.Store(rAddr.String(), gc)
						// sync ack
						seq := uint32(rand.Int31n(2<<16 - 2))
						m2 := &v1.Message{}
						m2.SYNACK(seq, m1.SeqN()+1, 0)
						_, err = g.c.WriteToUDP(m2.Marshall(), rAddr)
						if err != nil {
							log.Println("GListener : Write err", err)
							return
						}
						// wait established ack
						res := make(chan *estdRes)
						g.estdResult[rAddr.String()] = res
						go func() (c *GConn, err error) {
							defer func() {
								g.acceptResult <- &acceptRes{
									c:   c,
									err: err,
								}
							}()
							select {
							case r := <-res: // recv established ack
								if r.err != nil {
									return nil, err
								}
								if seq+1 != r.m.AckN() {
									return nil, errors.New("error ack number , seq is" + fmt.Sprint(seq) + ", ack is " + fmt.Sprint(r.m.AckN()))
								}
								gc.s = StatusEstablished
								return gc, nil
							case <-time.After(time.Duration(ackTimeout) * time.Millisecond): // wait ack timeout
								return nil, errors.New("sync timeout")
							}
						}()
						return
					}

					panic("unknown err")
				}()
			}
		}()
	})
}
func (g *GListener) Accept() (c net.Conn, err error) {
	g.init()
	r := <-g.acceptResult
	return r.c, r.err
}

func (g *GListener) Close() error {
	g.init()
	//panic("implement me")
	return nil // todo
}

func (g *GListener) Addr() net.Addr {
	g.init()
	return g.laddr
}
