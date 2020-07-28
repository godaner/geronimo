package v2

import (
	"errors"
	"fmt"
	"github.com/godaner/geronimo/rule"
	v1 "github.com/godaner/geronimo/rule/v1"
	v12 "github.com/godaner/geronimo/win/v1"
	"log"
	"math/rand"
	"net"
	"strconv"
	"sync"
	"time"
)

const (
	udpmss        = 1472
	syn1Timeout   = 50
	syn2Timeout   = 500
	fin1Timeout   = 500
	fin3Timeout   = 500
	syn1RetryTime = 10
	syn2RetryTime = 10
	fin1RetryTime = 10
	fin3RetryTime = 10
)
const (
	_                 = iota
	StatusListen      = iota
	StatusSynSent     = iota
	StatusSynRecved   = iota
	StatusEstablished = iota
	StatusFinWait1    = iota
	StatusCloseWait   = iota
	StatusFinWait2    = iota
	StatusLastAck     = iota
	StatusTimeWait    = iota
	StatusClosed      = iota
)

const (
	_       = iota
	FDial   = iota
	FListen = iota
)
const msl = 60 * 2

var (
	ErrConnStatus = errors.New("status err")
	ErrConnClosed = errors.New("closed")
)

type Status uint16

func (s Status) String() string {
	return fmt.Sprint(uint16(s))
}

type GConn struct {
	sync.Once
	*net.UDPConn
	f                                                          uint8
	s                                                          Status
	recvWin                                                    *v12.RWND
	sendWin                                                    *v12.SWND
	raddr                                                      *GAddr
	laddr                                                      *GAddr
	lis                                                        *GListener
	synSeqX, synSeqY, finSeqU, finSeqV, finSeqW                uint32
	syn1RetryTime, syn2RetryTime, fin1RetryTime, fin3RetryTime uint8
	syn1Finish, syn2Finish, fin1Finish, fin3Finish             chan bool
	mhs                                                        map[uint16]messageHandler
	stopReadFDialUDPSignal                                     chan bool
	loopReadFDialUDPOnce                                       sync.Once
}

func (g *GConn) init() {
	g.Do(func() {
		g.syn1Finish = make(chan bool)
		g.syn2Finish = make(chan bool)
		g.fin1Finish = make(chan bool)
		g.fin3Finish = make(chan bool)
		g.stopReadFDialUDPSignal = make(chan bool)
		// init message handlers
		g.mhs = map[uint16]messageHandler{
			// syn
			rule.FlagSYN1: g.syn1MessageHandler,
			rule.FlagSYN2: g.syn2MessageHandler,
			// fin
			rule.FlagFIN1: g.fin1MessageHandler,
			rule.FlagFIN2: g.fin2MessageHandler,
			// body
			rule.FlagPAYLOAD: g.payloadMessageHandler,
			// ack
			rule.FlagACK: g.ackMessageHandler,
		}

	})
}
// initLoopFDialReadUDP
func (g *GConn) initLoopFDialReadUDP() {
	g.loopReadFDialUDPOnce.Do(func() {
		if g.f == FDial {
			go func() {
				// recv udp
				bs := make([]byte, udpmss, udpmss)
				for {
					select {
					case <-g.stopReadFDialUDPSignal:
						return
					default:
						//func() {
						n, err := g.UDPConn.Read(bs)
						if err != nil {
							log.Println("GConn#init : Read err", err)
							g.UDPConn.Close() // todo need do this ??
							if g.sendWin!=nil{
								g.sendWin.Close(true)
							}
							if g.recvWin!=nil{
								g.recvWin.Close()
							}
							return
						}
						m := &v1.Message{}
						m.UnMarshall(bs[:n])
						err = g.handleMessage(m)
						if err != nil {
							log.Println("GConn#init : handleMessage err", err)
							continue
						}
						//}()
					}

				}
			}()
			return
		}
		panic("not from dial")
	})

}

// initWin
func (g *GConn) initWin() {
	g.initSendWin()
	g.initRecvWin()
}

// initRecvWin
func (g *GConn) initRecvWin() {
	g.recvWin = &v12.RWND{
		AckSender: func(ack uint32, receiveWinSize uint16) (err error) {
			m := &v1.Message{}
			m.ACK(ack, receiveWinSize)
			return g.sendMessage(m)
		},
	}
}

// initSendWin
func (g *GConn) initSendWin() {
	g.sendWin = &v12.SWND{
		SegmentSender: func(seq uint32, bs []byte) (err error) {
			// send udp
			m := &v1.Message{}
			m.PAYLOAD(seq, bs)
			return g.sendMessage(m)
		},
	}
}

// handleMessage
func (g *GConn) handleMessage(m *v1.Message) (err error) {
	g.init()
	mh, ok := g.mhs[m.Flag()]
	if ok && mh != nil {
		return mh(m)
	}
	log.Println("GConn#handleMessage : no message handler be found , flag is", m.Flag(), ", conn status is", g.s, ", flag is", strconv.FormatUint(uint64(m.Flag()), 2))
	panic("no handler")
}

// sendMessage
func (g *GConn) sendMessage(m *v1.Message) (err error) {
	b := m.Marshall()
	if g.f == FDial {
		_, err = g.UDPConn.Write(b)
		//log.Println("GConn : udp from ", g.UDPConn.LocalAddr().String(), " to", g.UDPConn.RemoteAddr().String())
		return err
	}
	if g.f == FListen {
		_, err = g.UDPConn.WriteToUDP(b, g.raddr.toUDPAddr())
		//log.Println("GConn : udp from ", g.UDPConn.LocalAddr().String(), " to", g.raddr.toUDPAddr().String())
		return err
	}
	return nil
}

// closeUDPConn
func (g *GConn) closeUDPConn() {
	if g.f == FDial {
		select {
		case <-g.stopReadFDialUDPSignal:
		default:
			close(g.stopReadFDialUDPSignal)
		}
		g.UDPConn.Close()
	}
	if g.f == FListen {
		g.lis.gcs.Delete(g.raddr.toUDPAddr().String())
	}
}
func (g *GConn) Read(b []byte) (n int, err error) {
	g.init()
	return g.recvWin.Read(b)
}

func (g *GConn) Write(b []byte) (n int, err error) {
	g.init()
	if g.Status() == StatusClosed {
		return 0, ErrConnClosed
	}
	return len(b), g.sendWin.Write(b)
}

func (g *GConn) Close() error {
	g.init()
	return g.close()
}

func (g *GConn) LocalAddr() net.Addr {
	g.init()
	return g.laddr
}

func (g *GConn) RemoteAddr() net.Addr {
	g.init()
	return g.raddr
}

func (g *GConn) SetDeadline(t time.Time) error {
	g.init()
	panic("implement me")
}

func (g *GConn) SetReadDeadline(t time.Time) error {
	g.init()
	panic("implement me")
}

func (g *GConn) SetWriteDeadline(t time.Time) error {
	g.init()
	panic("implement me")
}

func (g *GConn) Status() (s Status) {
	g.init()
	return g.s
}

func (g *GConn) dial() (err error) {
	g.init()
	g.initLoopFDialReadUDP()
	// sync req
	m1 := &v1.Message{}
	if g.synSeqX == 0 {
		g.synSeqX = uint32(rand.Int31n(2<<16 - 2))
	}
	m1.SYN1(g.synSeqX)
	err = g.sendMessage(m1)
	if err != nil {
		//g.closeUDPConn()
		log.Println("GConn#dial sendMessage1 err , err is", err)
		//return err
	}
	g.s = StatusSynSent
	for {
		if g.syn1RetryTime >= syn1RetryTime {
			log.Println("GConn#dial : syn1 fail , retry time is :" + fmt.Sprint(g.syn1RetryTime))
			g.closeUDPConn()
			return errors.New("syn1 fail , retry time is :" + fmt.Sprint(g.syn1RetryTime))
		}
		select {
		case <-g.syn1Finish:
			log.Println("GConn#dial : dial success")
			return
		case <-time.After(time.Duration(syn1Timeout) * time.Millisecond): // wait ack timeout
			err = g.sendMessage(m1)
			if err != nil {
				g.closeUDPConn()
				log.Println("GConn#dial sendMessage2 err , err is", err)
				return err
			}
			g.syn1RetryTime++
		}
	}
}

// close
func (g *GConn) close() (err error) {
	log.Println("GConn#close : start , status is",g.s)
	if g.s != StatusEstablished {
		return ErrConnStatus
	}
	g.sendWin.Close(false)
	m := &v1.Message{}
	if g.finSeqU == 0 {
		g.finSeqU = uint32(rand.Int31n(2<<16 - 2))
	}
	m.FIN1(g.finSeqU)
	err = g.sendMessage(m)
	if err != nil {
		log.Println("GConn#close sendMessage1 err , err is", err)
	}
	g.s = StatusFinWait1
	for {
		if g.fin1RetryTime >= fin1RetryTime {
			log.Println("GConn#close : fin1 fail , retry time is :" + fmt.Sprint(g.fin1RetryTime))
			g.sendWin.Close(true)
			g.recvWin.Close()
			g.closeUDPConn()
			return errors.New("fin1 fail , retry time is :" + fmt.Sprint(g.fin1RetryTime))
		}
		select {
		case <-g.fin1Finish:
			log.Println("GConn#close : finish")
			return
		case <-time.After(time.Duration(fin1Timeout) * time.Millisecond): // wait ack timeout
			err = g.sendMessage(m)
			if err != nil {
				log.Println("GConn#close sendMessage2 err , err is", err)
				g.sendWin.Close(true)
				g.recvWin.Close()
				g.closeUDPConn()
				return
			}
			g.fin1RetryTime++
		}
	}
}

func (g *GConn) maxStatus(s1, s2 Status) (ms Status) {
	if s1 > s2 {
		return s1
	}
	return s2
}