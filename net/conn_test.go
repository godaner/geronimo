package net

import (
	"fmt"
	"io"
	"net"
	"os"
	"testing"
	"time"
)
func TestDial(t *testing.T) {
	go func() {
		r()
	}()
	go func() {
		s()
	}()
	time.Sleep(1000000*time.Hour)
}
func r(){
	//devNull, _ := os.Open(os.DevNull)
	//log.SetOutput(devNull)
	c2,_:=Dial(&net.UDPAddr{
		IP:   net.ParseIP("192.168.6.6"),
		Port: 2222,
		Zone: "",
	},&net.UDPAddr{
		IP:   net.ParseIP("192.168.6.6"),
		Port: 1111,
		Zone: "",
	})

	file, err := os.Create("./test")
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
}
func s(){
	begin := time.Now()
	conn,_:=Dial(&net.UDPAddr{
		IP:   net.ParseIP("192.168.6.6"),
		Port: 1111,
		Zone: "",
	},&net.UDPAddr{
		IP:   net.ParseIP("192.168.6.6"),
		Port: 2222,
		Zone: "",
	})

	info, err := os.Stat("./source1")
	if err != nil {
		panic(err)
	}
	size := info.Size()
	fmt.Println(size)

	file, err := os.Open("./source1")

	if err != nil {
		panic(err)
	}
	b := make([]byte, 1024)
	var count int64
	for i := 0; ; i++ {

		n, err := file.Read(b)
		if err != nil {
			//panic(err)
			fmt.Println(err)
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
	c1,_:=Dial(&net.UDPAddr{
		IP:   net.ParseIP("192.168.6.6"),
		Port: 1111,
		Zone: "",
	},&net.UDPAddr{
		IP:   net.ParseIP("192.168.6.6"),
		Port: 2222,
		Zone: "",
	})
	c2,_:=Dial(&net.UDPAddr{
		IP:   net.ParseIP("192.168.6.6"),
		Port: 2222,
		Zone: "",
	},&net.UDPAddr{
		IP:   net.ParseIP("192.168.6.6"),
		Port: 1111,
		Zone: "",
	})
	s:=[]byte{}
	for i:=0;i<100;i++{
		s=append(s,[]byte("kecasdadad")...)
	}
	go func() {
		for ; ;  {
			c1.Write(s)
			//return
			//time.Sleep(1*time.Millisecond)
		}
	}()
	go func() {
		for ; ;  {
			bs:=make([]byte,len(s),len(s))
			io.ReadFull(c2,bs)
			fmt.Println(string(bs))
			//time.Sleep(1000*time.Millisecond)
			//n,_:=c2.Read(bs)
			//fmt.Println(string(bs[0:n]))
		}
	}()
	time.Sleep(1000*time.Second)
}
