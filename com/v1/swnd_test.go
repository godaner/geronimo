package v1

import (
	"fmt"
	"log"
	"os"
	"testing"
	"time"
)

func TestSWND_Write(t *testing.T) {
	devNull, _ := os.Open(os.DevNull)
	log.SetOutput(devNull)
	swnd := SWND{
		WriterCallBack: func(bs []byte) (err error) {
			fmt.Println(string(bs))
			return nil
		},
	}

	go func() {
		for ; ;  {
			fmt.Println(swnd.String())
			time.Sleep(3*time.Second)
		}
	}()
	swnd.Write([]byte("abcdefghij"))
	swnd.Write([]byte("klmnopqrst"))

	time.Sleep(3*time.Second)
	swnd.Ack(6)
	fmt.Println("ack1")

	time.Sleep(5*time.Second)
	swnd.Ack(10)
	fmt.Println("ack2")


	time.Sleep(1000 * time.Hour)
}
