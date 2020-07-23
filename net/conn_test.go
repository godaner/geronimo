package net

import (
	"fmt"
	"io"
	_ "net/http/pprof"
	"os"
	"testing"
	"time"
)

func TestDial(t *testing.T) {
	//devNull, _ := os.Open(os.DevNull)
	//log.SetOutput(devNull)
	go func() {
		r()
	}()
	go func() {
		s()
	}()
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
	for {
		c2, err := l.Accept()
		fmt.Println("Listen", c2.(*GConn).Status() == StatusEstablished)

		if err != nil {
			panic(err)
		}
		s := "./dst" + fmt.Sprint(i)
		i++
		go func() {
			file, err := os.Create(s)
			if err != nil {
				panic(err)
			}

			b := make([]byte, 10240)
			for i := 0; ; i++ {
				n, err := c2.Read(b)
				if err != nil {
					fmt.Println(err)
					break
				}
				_, err = file.Write(b[:n])
				if err != nil {
					panic(err)
				}
			}

			file.Close()
		}()
	}
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
	fmt.Println("Dial", conn.Status() == StatusEstablished)
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

	end := time.Now()

	fmt.Println(end.Unix() - begin.Unix())
}

func TestGConn_Read(t *testing.T) {
	//devNull, _ := os.Open(os.DevNull)
	//log.SetOutput(devNull)
	go func(){
		//<-time.After(600*time.Millisecond)
		//panic("log")
	}()
	s := []byte{}
	for i := 0; i < 100; i++ {
		s = append(s, []byte("kecasdadad")...)
	}
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
		for {
			bs := make([]byte, len(s), len(s))
			n,err:=io.ReadFull(c2, bs)
			fmt.Println(n,err,string(bs))
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
		for {
			c1.Write(s)
		}
	}()

	time.Sleep(1000 * time.Second)
}
