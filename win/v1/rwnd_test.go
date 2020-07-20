package v1

import (
	"fmt"
	"log"
	"os"
	"testing"
	"time"
)

func TestRWND_ReadFull(t *testing.T) {
	// DefWinSize = 8 !!!!
	devNull, _ := os.Open(os.DevNull)
	log.SetOutput(devNull)
	rwnd := &RWND{
		AckSender: func(ack, receiveWinSize uint16) (err error) {
			fmt.Println(ack, receiveWinSize)
			return nil
		},
	}
	go func() {
		bs := make([]byte, 8)
		for ; ;  {
			//n,_:=rwnd.Read(bs)
			n,_:=rwnd.ReadFull(bs)
			fmt.Println(string(bs),n)
		}
	}()
	rwnd.RecvSegment(4, []byte("dasb"))
	time.Sleep(1 * time.Second)
	rwnd.RecvSegment(0, []byte("this"))
	time.Sleep(1 * time.Second)
	rwnd.RecvSegment(8, []byte("zhan"))
	time.Sleep(1 * time.Second)
	rwnd.RecvSegment(12, []byte("6666"))
	time.Sleep(1 * time.Second)
	rwnd.RecvSegment(0, []byte("7777"))
	time.Sleep(1 * time.Second)
	rwnd.RecvSegment(4, []byte("8888"))
	time.Sleep(1000 * time.Second)
}
