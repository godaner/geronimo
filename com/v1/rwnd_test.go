package v1

import (
	"fmt"
	"testing"
	"time"
)

func TestRWND_ReadFull(t *testing.T) {
	// DefWinSize = 8
	rwnd := &RWND{
		AckCallBack: func(ack, receiveWinSize uint16) (err error) {
			fmt.Println(ack, receiveWinSize)
			return nil
		},
	}
	go func() {
		bs := make([]byte, 8)
		for ; ;  {
			rwnd.ReadFull(bs)
			fmt.Println(string(bs))
		}
	}()
	rwnd.Write(4, []byte("dasb"))
	time.Sleep(1 * time.Second)
	rwnd.Write(0, []byte("this"))
	time.Sleep(1 * time.Second)
	rwnd.Write(8, []byte("zhan"))
	time.Sleep(1 * time.Second)
	rwnd.Write(12, []byte("6666"))
	time.Sleep(1000 * time.Second)
}
