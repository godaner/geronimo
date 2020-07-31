package go_logging

import (
	"os"
	"sync"
	"time"
)

type RotateWriter struct {
	lock     sync.Mutex
	current  string
	path     string
	filename string // should be set to the actual filename
	fp       *os.File
}

// Make a new RotateWriter. Return nil if error occurs during setup.
func NewLogWrite(path string, filename string) *RotateWriter {
	w := &RotateWriter{filename: filename}
	w.path = path
	w.startRotate()
	return w
}

// Write satisfies the io.Writer interface.
func (w *RotateWriter) Write(output []byte) (int, error) {
	w.lock.Lock()
	defer w.lock.Unlock()
	w.writingRotate()
	return w.fp.Write(output)
}

func (w *RotateWriter) startRotate() {
	w.lock.Lock()
	defer w.lock.Unlock()

	w.current = time.Now().Format("2006-01-02")
	filename := w.path + "/" + w.filename + "-" + w.current + ".log"
	var err error
	err = os.MkdirAll(w.path, 0777)
	if err != nil {
		panic(err)
	}

	w.fp, err = os.OpenFile(filename, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		panic(err)
	}
	return
}
func (w *RotateWriter) writingRotate() {
	ct := time.Now().Format("2006-01-02")
	t1, err := time.Parse("2006-01-02", ct)
	t2, err := time.Parse("2006-01-02", w.current)
	if err == nil && t2.Before(t1) {
		//处理逻辑
		w.current = ct
		// Close existing file if open
		if w.fp != nil {
			err = w.fp.Close()
			w.fp = nil
			if err != nil {
				return
			}
		}
		filename := w.path + "/" + w.filename + "-" + ct + ".log"
		w.fp, err = os.OpenFile(filename, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0777)
		if err != nil {
			panic(err)
		}
	}
	return
}
