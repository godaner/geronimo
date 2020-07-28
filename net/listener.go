package net

import (
	"errors"
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
	closeSignal  chan bool
}
type acceptRes struct {
	c   net.Conn
	err error
}

func (g *GListener) init() {
	g.Do(func() {
		g.acceptResult = make(chan *acceptRes)
		g.closeSignal = make(chan bool)
		g.gcs = &sync.Map{} //map[string]*GConn{}
		go func() {
			bs := make([]byte, udpmss, udpmss)
			for {
				select {
				case <-g.closeSignal:
					return
				default:
				}
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
					err = gc.handleMessage(m1)
					if err != nil {
						log.Println("GListener#init : handleMessage err", err)
						return
					}
				}()
			}
		}()
	})
}
func (g *GListener) Accept() (c net.Conn, err error) {
	g.init()
	r := <-g.acceptResult
	if r == nil {
		return nil, errors.New("nil accept")
	}
	return r.c, r.err
}

func (g *GListener) Close() error {
	g.init()
	select {
	case <-g.closeSignal:
	default:
		close(g.closeSignal)
	}
	g.gcs.Range(func(key, value interface{}) bool {
		c := value.(*GConn)
		c.Close()
		g.gcs.Delete(key)
		return true
	})
	return nil
}

func (g *GListener) Addr() net.Addr {
	g.init()
	return g.laddr
}
