package log

import (
	"fmt"
	"log/syslog"
)

type syslogWriter struct {
	writer *syslog.Writer
}

func newSyslogWriter(tag string) *syslogWriter {
	writer, err := syslog.New(syslog.LOG_INFO|syslog.LOG_LOCAL6, tag)
	if err != nil {
		fmt.Printf("New syslog error:%v\n", err)
	}
	return &syslogWriter{
		writer: writer,
	}
}

func (sw *syslogWriter) write(str string) {
	if sw.writer != nil {
		sw.writer.Info(str)
	}
}

func (sw *syslogWriter) close() {
	if sw.writer != nil {
		sw.writer.Close()
	}
	sw = nil
}
