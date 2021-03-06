package net

import (
	"crypto/md5"
	"fmt"
	loggerfac "github.com/godaner/logger/factory"
	"io"
	"log"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"testing"
	"time"
)

func TestDial(t *testing.T) {
	loggerfac.Init("TestDial")
	devNull, _ := os.Open(os.DevNull)
	log.SetOutput(devNull)

	go func() {
		s()
	}()

	r()
	//go func() {
	//	s()
	//}()
	time.Sleep(1000000 * time.Hour)
}
func r() {
	l, err := Listen(&GAddr{
		IP:   "192.168.6.6",
		Port: 2222,
	})
	if err != nil {
		panic(err)
	}
	i := 1
	//for {
	c2, err := l.Accept()
	//fmt.Println("Listen", c2.(*GConn).Status() == StatusEstablished)

	if err != nil {
		panic(err)
	}
	s := "./dst" + fmt.Sprint(i)
	i++
	//go func() {
	file, err := os.Create(s)
	if err != nil {
		panic(err)
	}

	b := make([]byte, 10240)
	for i := 0; ; i++ {
		n, err := c2.Read(b)
		if err != nil {
			fmt.Println("read", err)
			break
		}
		_, err = file.Write(b[:n])
		if err != nil {
			panic(err)
		}
	}

	file.Close()
	fmt.Println("ccc")
	//}()
	//}
}
func s() {
	begin := time.Now()
	conn, err := Dial(&GAddr{
		IP:   "192.168.6.6",
		Port: 2222,
	})
	if err != nil {
		panic(err)
	}
	//fmt.Println("Dial", conn.Status() == StatusEstablished)
	info, err := os.Stat("./src2")
	if err != nil {
		panic(err)
	}
	size := info.Size()
	fmt.Println(size)

	file, err := os.Open("./src2")

	if err != nil {
		panic(err)
	}
	b := make([]byte, 1024)
	var count int64
	for i := 0; ; i++ {

		n, err := file.Read(b)
		if err != nil {
			//panic(err)
			fmt.Println(err, n)
			break
		}
		count += int64(n)
		_, err = conn.Write(b[:n])
		if err != nil {
			panic(err)
		}

		//print planned speed
		if i%1000 == 0 {
			fmt.Println(float64(count) / float64(size))
		}

	}

	//conn.Write(b)
	conn.Close()
	fmt.Println("s , status", conn.Status())
	end := time.Now()

	fmt.Println(end.Unix() - begin.Unix())
}
func md5S(bs []byte) (s string) {
	w := md5.New()
	io.WriteString(w, string(bs))
	md5str2 := fmt.Sprintf("%x", w.Sum(nil))
	return md5str2
}
func TestGConn_Close(t *testing.T) {

	loggerfac.Init("TestGConn_Close")
	devNull, _ := os.Open(os.DevNull)
	log.SetOutput(devNull)
	s := []byte{}
	for i := 0; i < 10; i++ {
		s = append(s, []byte("kecasdadad")...)
	}
	smd5 := md5S(s)
	go func() {
		l, err := Listen(&GAddr{
			IP:   "192.168.6.6",
			Port: 2222,
		})
		if err != nil {
			panic(err)
		}
		c2, err := l.Accept()
		if err != nil {
			panic(err)
		}
		i := uint64(0)
		for {
			bs := make([]byte, len(s), len(s))
			n, err := io.ReadFull(c2, bs)
			fmt.Println(i, n, err, string(bs))
			ssmd5 := md5S(bs)
			if ssmd5 != smd5 {
				panic("not right md5")
			}
			i++
		}
	}()
	time.Sleep(500 * time.Millisecond)
	c1, err := Dial(&GAddr{
		IP:   "192.168.6.6",
		Port: 2222,
	})
	if err != nil {
		panic(err)
	}
	go func() {
		go func() {
			time.Sleep(3 * time.Second)
			fmt.Println("c1 close start ")
			c1.Close()
			fmt.Println("c1 status", c1.Status())
		}()
		for i := 0; i < 10; i++ {
			_, err := c1.Write(s)
			fmt.Println("send　：", err)
			if err != nil {
				break
			}
			time.Sleep(1 * time.Second)
		}
	}()
	time.Sleep(1000 * time.Second)
}

func TestGConn_Close2(t *testing.T) {
	loggerfac.Init("TestGConn_Close2")
	go func() {
		l, err := Listen(&GAddr{
			IP:   "192.168.6.6",
			Port: 3333,
		})
		if err != nil {
			panic(err)
		}
		c2, err := l.Accept()
		if err != nil {
			panic(err)
		}
		fmt.Println(c2)
		bs := make([]byte, 15, 15)
		n, err := c2.Read(bs)
		fmt.Println(n, err, string(bs))
	}()
	c1, err := Dial(&GAddr{
		IP:   "192.168.6.6",
		Port: 3333,
	})
	if err != nil {
		panic(err)
	}
	fmt.Println("w dial", c1, err)
	n, err := c1.Write([]byte("ccccccccccccccc"))
	fmt.Println("w n", n)
	if err != nil {
		fmt.Println("w err", err)
	}
	err = c1.Close()
	fmt.Println(c1, err)

	time.Sleep(1000 * time.Second)
}
func TestGListener_Accept(t *testing.T) {
	loggerfac.Init("TestGListener_Accept")
	s1 := "s1"
	s2 := "s2"
	go func() {
		l, err := Listen(&GAddr{
			IP:   "192.168.6.6",
			Port: 3333,
		})
		if err != nil {
			panic(err)
		}
		i := 10
		for {
			c1, err := l.Accept()
			if err != nil {
				panic(err)
			}
			go func(i int) {
				bs := make([]byte, 2, 2)
				n, err := c1.Read(bs)
				fmt.Println("c"+fmt.Sprint(i)+" r", n, err, string(bs))
				n, err = c1.Write([]byte(s2))
				fmt.Println("c"+fmt.Sprint(i)+" w", n, err)
			}(i)
			i++
		}

	}()
	go func() {
		c2, err := Dial(&GAddr{
			IP:   "192.168.6.6",
			Port: 3333,
		})
		n, err := c2.Write([]byte(s1))
		fmt.Println("c2 w", n, err)
		bs := make([]byte, 2, 2)
		n, err = c2.Read(bs)
		fmt.Println("c2 r", n, err, string(bs))
		err = c2.Close()
		fmt.Println("c2 c", err, c2.Status())
	}()
	go func() {
		c3, err := Dial(&GAddr{
			IP:   "192.168.6.6",
			Port: 3333,
		})
		n, err := c3.Write([]byte(s1))
		fmt.Println("c3 w", n, err)
		bs := make([]byte, 2, 2)
		n, err = c3.Read(bs)
		fmt.Println("c3 r", n, err, string(bs))
		err = c3.Close()
		fmt.Println("c3 c", err, c3.Status())
	}()
	time.Sleep(1000 * time.Second)
}

func TestGListener_Close(t *testing.T) {
	loggerfac.Init("TestGListener_Close")
	hello1 := "hello1"
	hello2 := "hello2"
	// listen
	go func() {
		l, err := Listen(&GAddr{
			IP:   "192.168.6.6",
			Port: 3333,
		})
		if err != nil {
			panic(err)
		}
		for {
			c1, err := l.Accept()
			if err != nil {
				panic(err)
			}
			go func() {
				for {
					bs := make([]byte, len(hello1), len(hello1))
					n, err := c1.Read(bs)
					fmt.Println("s r", n, err, string(bs))
					n, err = c1.Write([]byte(hello2))
					fmt.Println("s w", n, err)
					c1.Close()
					return
				}

			}()
		}

	}()
	go func() {
		for {
			go func() {
				c2, err := Dial(&GAddr{
					IP:   "192.168.6.6",
					Port: 3333,
				})
				n, err := c2.Write([]byte(hello1))
				fmt.Println("c w", n, err)
				bs := make([]byte, len(hello2), len(hello2))
				n, err = c2.Read(bs)
				fmt.Println("c r", n, err, string(bs))
				err = c2.Close()
				return
			}()
			time.Sleep(1000 * time.Millisecond)
		}
	}()
	time.Sleep(1000 * time.Second)
}
func TestGConn_LocalAddr(t *testing.T) {
	loggerfac.Init("TestGConn_LocalAddr")
	// listen
	go func() {
		l, err := Listen(&GAddr{
			IP:   "192.168.6.6",
			Port: 3333,
		})
		if err != nil {
			panic(err)
		}
		fmt.Println(l)
		l.Accept()

	}()
	go func() {
		time.Sleep(1 * time.Second)
		c2, err := Dial(&GAddr{
			IP:   "192.168.6.6",
			Port: 3333,
		})
		fmt.Println(c2, err)
	}()
	time.Sleep(1000 * time.Second)
}

func TestGConn_Read(t *testing.T) {
	loggerfac.Init("TestGConn_Read2")
	go func() {
		err := http.ListenAndServe(":5555", nil)
		if err != nil {
			panic(err)
		}
	}()
	devNull, _ := os.Open(os.DevNull)
	log.SetOutput(devNull)
	go func() {
		//<-time.After(600*time.Millisecond)
		//panic("log")
	}()
	s := []byte{}
	for i := 0; i < 1000; i++ {
		s = append(s, []byte("kecasdadad")...)
	}
	smd5 := md5S(s)
	go func() {
		l, err := Listen(&GAddr{
			IP:   "192.168.6.6",
			Port: 2222,
		})
		if err != nil {
			panic(err)
		}
		for {
			c2, err := l.Accept()
			if err != nil {
				panic(err)
			}
			go func() {
				for {
					bs := make([]byte, len(s), len(s))
					io.ReadFull(c2, bs)
					//n, err := io.ReadFull(c2, bs)
					//fmt.Println(i, n,len(bs), err, string(bs))
					ssmd5 := md5S(bs)
					if ssmd5 != smd5 {
						//time.Sleep(1*time.Second)
						panic("not right md5")
					}
				}
			}()
		}

	}()
	time.Sleep(500 * time.Millisecond)
	for i := 0; i < 2; i++ {
		go func() {
			c1, err := Dial(&GAddr{
				IP:   "192.168.6.6",
				Port: 2222,
			})
			if err != nil {
				panic(err)
			}
			for {
				c1.Write(s)
			}
		}()
	}

	time.Sleep(1000 * time.Hour)
}
func TestGConn_Read2(t *testing.T) {

	loggerfac.Init("TestGConn_Read2")
	go func() {
		err := http.ListenAndServe(":5555", nil)
		if err != nil {
			panic(err)
		}
	}()
	devNull, _ := os.Open(os.DevNull)
	log.SetOutput(devNull)
	go func() {
		//<-time.After(600*time.Millisecond)
		//panic("log")
	}()
	s := []byte{}
	for i := 0; i < 1000; i++ {
		s = append(s, []byte("kecasdadad")...)
	}
	smd5 := md5S(s)
	go func() {
		l, err := Listen(&GAddr{
			IP:   "192.168.6.6",
			Port: 2222,
		},SetEnc("aes-256-cfb@123qwe"))
		if err != nil {
			panic(err)
		}
		for {
			c2, err := l.Accept()
			if err != nil {
				panic(err)
			}
			go func() {
				for {
					bs := make([]byte, len(s), len(s))
					io.ReadFull(c2, bs)
					//n, err := io.ReadFull(c2, bs)
					//fmt.Println(i, n,len(bs), err, string(bs))
					ssmd5 := md5S(bs)
					if ssmd5 != smd5 {
						//time.Sleep(1*time.Second)
						panic("not right md5")
					}
				}
			}()
		}

	}()
	time.Sleep(500 * time.Millisecond)
	for i := 0; i < 2; i++ {
		go func() {
			c1, err := Dial(&GAddr{
				IP:   "192.168.6.6",
				Port: 2222,
			},SetEnc("aes-256-cfb@123qwe"))
			if err != nil {
				panic(err)
			}
			for {
				c1.Write(s)
			}
		}()
	}

	time.Sleep(1000 * time.Hour)
}

func TestGConn_RemoteAddr(t *testing.T) {
	loggerfac.Init("TestGConn_RemoteAddr")
	go func() {
		time.Sleep(2 * time.Second)
		c, err := net.DialUDP("udp", nil, &net.UDPAddr{
			IP:   net.ParseIP("127.0.0.1"),
			Port: 5555,
			Zone: "",
		})
		if err != nil {
			panic(err)
		}
		_, err = c.Write([]byte("hello"))
		if err != nil {
			panic(err)
		}
		c.Close()
	}()
	c, err := net.ListenUDP("udp", &net.UDPAddr{
		IP:   nil,
		Port: 5555,
		Zone: "",
	})
	if err != nil {
		panic(err)
	}
	go func() {
		time.Sleep(5 * time.Second)
		_, err = c.Write([]byte("hello2"))
		if err != nil {
			panic(err)
		}
		fmt.Println("res")
	}()
	for {
		bs := make([]byte, 1024, 1024)
		n, udp, err := c.ReadFromUDP(bs)
		if err != nil {
			panic(err)
		}
		fmt.Println(n, udp.String())
	}
}
func TestGConn_SetDeadline(t *testing.T) {
	m := 10
	n := 50
	for i := 0; i < m; i++ {
		fmt.Println(n)
		n *= 2
	}
}
