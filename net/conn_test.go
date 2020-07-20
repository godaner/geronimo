package net

import (
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"testing"
	"time"
)

func TestGConn_Read(t *testing.T) {
	devNull, _ := os.Open(os.DevNull)
	log.SetOutput(devNull)
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
