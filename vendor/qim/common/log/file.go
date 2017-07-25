package log

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type fileWriter struct {
	tag        string
	basePath   string
	fd         *os.File
	fileName   string
	trunc_flag bool
}

func newFileWriter(tag, basePath string) *fileWriter {
	if basePath == "" {
		goPath := os.Getenv("GOPATH")
		if goPath == "" {
			basePath = "/tmp/logs"
		}
		idx := strings.Index(goPath, ";")
		if idx != -1 {
			goPath = string([]byte(goPath)[:idx])
		}

		basePath = goPath + "/logs"
	}

	return &fileWriter{
		tag:      tag,
		basePath: basePath,
	}
}

func (fw *fileWriter) write(str string) {
	if fw.fd == nil || !checkFileExist(fw.fileName) {
		err := fw.createFile()
		if err != nil {
			fmt.Println("File ", fw.fileName, "write error:", err)
		}
	}
	fw.fd.WriteString(str)
}

func (fw *fileWriter) trunc() {
	fw.trunc_flag = true
}

func (fw *fileWriter) close() {
	if fw.fd != nil {
		fw.fd.Close()
	}
	fw = nil
}

// createFile create log file on the base path
// Log directory example: ${basePath}/logs/{$tag}/${tag}.log
// Use logrotate tool to rotate log
func (fw *fileWriter) createFile() error {
	if fw.fd != nil {
		fw.fd.Close()
	}
	//logDir := filepath.Join(fw.basePath, fmt.Sprintf("/%s/", fw.tag))
	logDir := filepath.Join(fw.basePath, "/")
	err := os.MkdirAll(logDir, os.ModePerm)
	if err != nil {
		return fmt.Errorf("Can't create dir:%s, error:%v", logDir, err)
	}
	fileName := filepath.Join(logDir, fmt.Sprintf("%s.log", fw.tag))
	fw.fileName = fileName

	if fw.trunc_flag {
		os.Remove(fileName)
	}

	f, err := os.OpenFile(fileName, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		return fmt.Errorf("Can't open log file:%s, error:%v", fileName, err)
	}
	fw.fd = f
	return nil
}

// checkFileExist check file exist or not
func checkFileExist(file string) bool {
	if _, err := os.Stat(file); os.IsNotExist(err) {
		return false
	}
	return true
}
