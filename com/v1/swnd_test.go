package v1

import (
	"fmt"
	"testing"
	"time"
)

func TestSWND_Write(t *testing.T) {
	swnd := SWND{
		WriterCallBack: func(bs []byte) (err error) {
			fmt.Println(string(bs))
			return nil
		},
	}
	swnd.Write([]byte("this_a_msg"))
	fmt.Println(swnd.String())
	//for i := 1; i < 100; i++ {
	//	swnd.Ack(uint16(i))
	//}
	//fmt.Println(swnd.String())
	//time.Sleep(1*time.Millisecond)
	//for i := 1; i < 100; i++ {
	//	swnd.Ack(uint16(i))
	//}
	time.Sleep(3 * time.Second)
	fmt.Println(swnd.String())
	swnd.Ack(6)
	fmt.Println("ack")
	time.Sleep(1*time.Second)
	fmt.Println(swnd.String())
	time.Sleep(1000 * time.Hour)
}
