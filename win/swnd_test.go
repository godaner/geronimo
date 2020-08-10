package win

import (
	"fmt"
	"log"
	"os"
	"testing"
	"time"
)

func TestSWND_Write(t *testing.T) {
	// DefWinSize = 8 !!!!
	// mms = 2
	devNull, _ := os.Open(os.DevNull)
	log.SetOutput(devNull)
	swnd := SWND{
		SegmentSender: func(firstSeq uint16, bs []byte) (err error) {
			fmt.Println(firstSeq,string(bs))
			return nil
		},
	}

	go func() {
		for {
			//fmt.Println(swnd.String())
			time.Sleep(2 * time.Second)
		}
	}()
	//swnd.Recv([]byte("wosi"))
	go func() {
		time.Sleep(1 * time.Second)
		swnd.Write([]byte("abcdefghij"))
		swnd.Write([]byte("klmnopqrst"))
	}()

	time.Sleep(2 * time.Second)
	swnd.RecvAck(2) // win = 9
	fmt.Println("ack1")
	time.Sleep(2 * time.Second)
	swnd.RecvAck(4) // win = 9
	fmt.Println("ack2")
	//time.Sleep(5 * time.Second)
	//swnd.RecvAck(11)
	//fmt.Println("ack2")

	time.Sleep(1000 * time.Hour)
}