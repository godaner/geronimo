package win

import (
	"fmt"
	"github.com/smallnest/ringbuffer"
	"testing"
	"time"
)
func TestBQ_Len(t *testing.T) {
	a:=[]byte{'a','b','c'}
	b:=a[1:]
	a=append(a,'d')
	b=append(b,'d')
	b=append(b,'d')
	c:=[]byte{'b','c'}
	c=append(c,'d')
	c=append(c,'d')
	fmt.Println(len(a),cap(a))
	fmt.Println(len(b),cap(b))
	fmt.Println(len(c),cap(c))

}
func TestByteBlockChan_Push(t *testing.T) {
	b:=&bq{Size: 10}
	b.BlockPush([]byte("zhang")...)
	b.BlockPush([]byte("zhang")...)
	go func() {
		time.Sleep(2*time.Second)
		bs:=make([]byte,5,5)
		n:=b.Pop(bs)
		fmt.Println(string(bs[:n]))
	}()
	b.BlockPush([]byte("zhang")...)
	fmt.Println(b.Len())
	go func() {
		time.Sleep(2*time.Second)
		bs:=make([]byte,5,5)
		n:=b.Pop(bs)
		fmt.Println(string(bs[:n]))
	}()
	b.BlockPush([]byte("zhang")...)
	fmt.Println(b.Len())

}
func TestByteBlockChan_Len(t *testing.T) {
	s:=time.Now().UnixNano()
	c:=make(chan bool,65535)
	for i:=0;i<65535;i++{
		c<-true
	}
	for i:=0;i<65535;i++{
		<-c
	}
	fmt.Println(time.Now().UnixNano()-s)
	s=time.Now().UnixNano()
	ss:=make([]bool,0)
	for i:=0;i<65535;i++{
		ss=append(ss,true)
	}
	n:=len(ss)
	sss:=make([]bool,0)
	copy(sss,ss[:n])
	ss=ss[n:]
	fmt.Println(time.Now().UnixNano()-s)
}

func TestByteBlockChan_PopWithStop(t *testing.T) {
	//bs:=make([]byte,5)
	//buf:=bytes.NewBuffer(make([]byte,5))
	//_,err:=buf.Read(bs)
	//fmt.Println(err,string(bs))
	//_,err=buf.Read(bs)
	//fmt.Println(err,string(bs))
	buf :=ringbuffer.New(5)
	bs:=make([]byte,5)
	_,err:=buf.Read(bs)
	fmt.Println(err,string(bs))
	//io.ReadFull()
}
func TestByteBlockChan_Pop(t *testing.T) {
	b:=&bq{Size: 10}
	bs:=make([]byte,5,5)
	go func() {
		time.Sleep(2*time.Second)
		b.BlockPush([]byte("zhang")...)
	}()
	n:=b.Pop(bs)
	fmt.Println(string(bs[:n]),b.Len())
	go func() {
		time.Sleep(2*time.Second)
		b.BlockPush([]byte("zhang")...)
	}()
	n=b.Pop(bs)
	fmt.Println(string(bs[:n]),b.Len())

}