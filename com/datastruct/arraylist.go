package datastruct

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
)

type RangeCallBack func(index uint32, d interface{}) (cti bool)
type ArrayList struct {
	l []interface{}
	sync.RWMutex
}

func (a *ArrayList) Len() (l uint32) {
	return uint32(len(a.l))
}

func (a *ArrayList) LastIndex() uint32 {
	return a.Len() - 1
}

func (a *ArrayList) Get(index uint32) (d interface{}, err error) {
	a.RLock()
	defer a.RUnlock()
	lastIndex := a.LastIndex()
	if index > lastIndex {
		return nil, errors.New(fmt.Sprintf("index is out of range , index is : %v , list range is : [0,%v]", index, lastIndex))
	}
	return a.l[index], nil
}
func (a *ArrayList) Set(index uint32, d interface{}) (err error) {
	a.RLock()
	defer a.RUnlock()
	lastIndex := a.LastIndex()
	if index > lastIndex {
		return errors.New(fmt.Sprintf("index is out of range , index is : %v , list range is : [0,%v]", index, lastIndex))
	}
	a.l[index] = d
	return nil
}
func (a *ArrayList) Append(ds ...interface{}) {
	a.Lock()
	defer a.Unlock()
	a.l = append(a.l, ds...)
}

// KeepRight
//  arr[l:)
func (a *ArrayList) KeepRight(l uint32) (err error) {
	a.Lock()
	defer a.Unlock()
	lastIndex := a.LastIndex()
	if l < 0 || l > lastIndex {
		return errors.New(fmt.Sprintf("l is out of range , l is : %v , list range is : [0,%v]", l, lastIndex))
	}
	a.l = a.l[l:]
	return nil
}

// KeepRightRetLeft
//  arr[l:)
func (a *ArrayList) KeepRightRetLeft(l uint32) (left *ArrayList, err error) {
	a.Lock()
	defer a.Unlock()
	lastIndex := a.LastIndex()
	if l < 0 || l > a.Len() {
		return nil, errors.New(fmt.Sprintf("l is out of range , l is : %v , list range is : [0,%v]", l, lastIndex))
	}
	// 0 1 2 3
	// l=4
	lef := a.l[:l]
	if l >= a.Len() {
		a.l = []interface{}{}
	} else {
		a.l = a.l[l:]
	}
	return &ArrayList{l: lef}, nil
}

// Range
func (a *ArrayList) Range(f RangeCallBack) {
	a.RLock()
	defer a.RUnlock()
	for i, d := range a.l {
		cti := f(uint32(i), d)
		if !cti {
			return
		}
	}
}

// RangeWithStart
//  l[s:)
func (a *ArrayList) RangeWithStart(f RangeCallBack, s uint32) {
	a.RLock()
	defer a.RUnlock()
	le := a.Len()
	for i := s; i < le; i++ {
		cti := f(i, a.l[i])
		if !cti {
			return
		}
	}
}

// String
func (a *ArrayList) String() string {
	dsm := make([]string, 0)
	for _, d := range a.l {
		s := ""
		switch d.(type) {
		case fmt.Stringer:
			s = d.(fmt.Stringer).String()
		default:
			bs, _ := json.Marshal(d)
			s = string(bs)
		}
		dsm = append(dsm, s)
	}
	return strings.Join(dsm, ",")
}
