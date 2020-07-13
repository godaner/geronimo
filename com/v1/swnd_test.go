package v1

import (
	"fmt"
	"testing"
	"time"
)

func TestSWND_Write(t *testing.T) {
	swnd := SWND{
		CallWrite: func(bs []byte) (err error) {
			fmt.Println(string(bs))
			return nil
		},
	}
	swnd.Write([]byte("this_a_msg,zkzzzzzzzzzzzzzzzzzzzzzzzz"))
	swnd.PrintDs()
	for i := 1; i < 100; i++ {
		swnd.Ack(uint16(i))
	}
	swnd.PrintDs()
	time.Sleep(1*time.Millisecond)
	for i := 1; i < 100; i++ {
		swnd.Ack(uint16(i))
	}
	time.Sleep(1000 * time.Hour)
}
