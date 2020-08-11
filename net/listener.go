package net

import (
	"errors"
	"fmt"
	"github.com/godaner/geronimo/logger"
	gologging "github.com/godaner/geronimo/logger/go-logging"
	v1 "github.com/godaner/geronimo/rule/v1"
	"net"
	"sync"
)

const (
	msgQSize = 100
)

func Listen(addr *GAddr, options ...Option) (l net.Listener, err error) {
	opts := &Options{}
	for _, o := range options {
		o(opts)
	}
	c, err := net.ListenUDP("udp", addr.toUDPAddr())
	if err != nil {
		panic(err)
		return nil, err
	}
	return &GListener{
		OverBose: opts.OverBose,
		laddr:    addr,
		c:        c,
	}, nil
}

type GListener struct {
	sync.Once
	rmConnLock   sync.RWMutex
	closeLock    sync.RWMutex
	OverBose     bool
	laddr        *GAddr
	c            *net.UDPConn
	gcs          *sync.Map //map[string]*GConn
	acceptResult chan *acceptRes
	msgs         *sync.Map
	closes       *sync.Map
	closeSignal  chan bool
	logger       logger.Logger
}
type acceptRes struct {
	c   net.Conn
	err error
}

func (g *GListener) init() {
	g.Do(func() {
		g.logger = gologging.GetLogger(fmt.Sprintf("%v%v", "GListener", &g))
		g.acceptResult = make(chan *acceptRes)
		g.closeSignal = make(chan bool)
		g.gcs = &sync.Map{} //map[string]*GConn{}
		g.msgs = &sync.Map{}
		g.closes = &sync.Map{}
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
						g.logger.Error("GListener : ReadFromUDP err", err)
						return
					}
					m1 := &v1.Message{}
					m1.UnMarshall(bs[:n])
					gcI, _ := g.gcs.Load(rAddr.String())
					gc, _ := gcI.(*GConn)
					if gc == nil {
						// first connect
						gc = &GConn{
							UDPConn:  g.c,
							OverBose: g.OverBose,
							raddr:    fromUDPAddr(rAddr),
							laddr:    fromUDPAddr(g.c.LocalAddr().(*net.UDPAddr)),
							s:        StatusListen,
							f:        FListen,
							lis:      g,
						}
						g.gcs.Store(rAddr.String(), gc)
						msg := make(chan *v1.Message, msgQSize)
						g.msgs.Store(rAddr.String(), msg)
						closeS := make(chan bool)
						g.closes.Store(rAddr.String(), closeS)
						go func() {
							for {
								select {
								case <-closeS:
									return
								case m := <-msg:
									err = gc.handleMessage(m)
									if err != nil {
										g.logger.Error("GListener#init : handleMessage err", err)
										return
									}
								}
							}
						}()
					}
					g.logger.Debug("GListener#init : recv msg from", rAddr.String(), ", msg flag is ", m1.Flag())
					msgI, _ := g.msgs.Load(rAddr.String())
					msg, ok := msgI.(chan *v1.Message)
					if !ok {
						g.logger.Error("GListener#init : msg chan is close")
						return
					}
					select {
					case msg <- m1:
						//return
						//case <-time.After(time.Duration(1) * time.Second):
						//	panic("send msg timeout")
					}
				}()
			}
		}()
	})
}

// RmGConn
func (g *GListener) RmGConn(addr interface{}) {
	g.init()
	g.rmConnLock.Lock()
	defer g.rmConnLock.Unlock()
	if g == nil {
		return
	}
	g.gcs.Delete(addr)
	g.msgs.Delete(addr)
	csI, ok := g.closes.Load(addr)
	if !ok {
		return
	}
	cs := csI.(chan bool)
	select {
	case <-cs:
		return
	default:
		close(cs)
	}
	g.closes.Delete(addr)

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
	g.closeLock.Lock()
	defer g.closeLock.Unlock()
	select {
	case <-g.closeSignal:
	default:
		close(g.closeSignal)
	}
	g.gcs.Range(func(key, value interface{}) bool {
		c := value.(*GConn)
		c.Close()
		return true
	})
	return nil
}

func (g *GListener) Addr() net.Addr {
	g.init()
	return g.laddr
}
