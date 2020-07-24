package net

import (
	v1 "github.com/godaner/geronimo/rule/v1"
	"log"
	"net"
	"sync"
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
	//estdResult   map[string]chan *estdRes
}
//type estdRes struct {
//	err error
//	m   *v1.Message
//}
type acceptRes struct {
	c   net.Conn
	err error
}

func (g *GListener) init() {
	g.Do(func() {
		g.acceptResult = make(chan *acceptRes)
		//g.estdResult = map[string]chan *estdRes{}
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
					m1 := &v1.Message{}
					m1.UnMarshall(bs[:n])
					gcI, _ := g.gcs.Load(rAddr.String())
					gc, _ := gcI.(*GConn)
					if gc == nil {
						// first connect
						gc = &GConn{
							UDPConn: g.c,
							raddr:   fromUDPAddr(rAddr),
							laddr:   fromUDPAddr(g.c.LocalAddr().(*net.UDPAddr)),
							s:       StatusListen,
							f:       FListen,
							lis:     g,
						}
						g.gcs.Store(rAddr.String(), gc)
					}
					gc.handleMessage(m1)
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
