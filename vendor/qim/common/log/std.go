package log

import (
	"os"
)

type stdWriter struct {
	w *os.File
}

func newStdWriter() *stdWriter {
	return &stdWriter{w: os.Stdout}
}

func (s *stdWriter) write(str string) {
	s.w.WriteString(str)
}

func (s *stdWriter) trunc() {
}

func (sw *stdWriter) close() {
	if sw.w != nil {
		sw.w.Close()
	}
	sw = nil
}
