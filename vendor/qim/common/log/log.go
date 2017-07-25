package log

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type (
	LogLevel byte
	LogType  byte
)

const (
	LOG_LEVEL_NONE  = LogLevel(0x0)
	LOG_LEVEL_FATAL = LOG_LEVEL_NONE | LogLevel(0x1)
	LOG_LEVEL_ERROR = LOG_LEVEL_FATAL | LogLevel(0x2)
	LOG_LEVEL_WARN  = LOG_LEVEL_ERROR | LogLevel(0x4)
	LOG_LEVEL_INFO  = LOG_LEVEL_WARN | LogLevel(0x8)
	LOG_LEVEL_DEBUG = LOG_LEVEL_INFO | LogLevel(0x10)
	LOG_LEVEL_ALL   = LOG_LEVEL_DEBUG
)

const (
	// Default. Write log to stdout
	STD LogType = iota

	// Write log to file.
	// If specify {filepath}, the path is {filepath}/logs
	// If no specify file, default path: ${GOPATH}/logs.
	// If ${GOPATH} is not set, the path is /tmp/logs.
	// File rotate by time(Hour)
	FILE

	// Write log to syslog
	SYSLOG
)

// logWriter is a generic log writer
type logWriter interface {
	write(string)
	trunc()
	close()
}

// Logger represents an logging object that generate line output accroding to logType.
// filepath is set when the logType is FILE.
// tag represent an object distinguish from other.
type Logger struct {
	logType   LogType
	logSimple bool
	logLevel  LogLevel
	filePath  string
	tag       string
	writer    logWriter
}

var logger *Logger

func init() {
	logger = &Logger{
		logType:   STD,
		logSimple: false,
		logLevel:  LOG_LEVEL_DEBUG,
		writer:    newStdWriter(),
	}
}

func WithTag(tag string) *Logger {
	logger.tag = tag
	return logger
}

func (l *Logger) WithType(lt string) *Logger {
	l.logType = stringToLogType(lt)
	switch l.logType {
	case STD:
		// DO NOTHING
	case FILE:
		l.writer = newFileWriter(l.tag, l.filePath)
	case SYSLOG:
		//l.writer = newSyslogWriter(l.tag)
	}
	return l
}

func (l *Logger) WithLevel(level string) *Logger {
	l.logLevel = stringToLogLevel(level)
	return l
}

func (l *Logger) WithSimple() *Logger {
	l.logSimple = true
	return l
}

func (l *Logger) WithFilePath(fp string) *Logger {
	l.filePath = fp
	if l.writer != nil {
		l.writer.close()
	}
	l.writer = newFileWriter(l.tag, l.filePath)
	return l
}

func (l *Logger) WithTrunc() *Logger {
	l.writer.trunc()
	return l
}

// NewLogger create a new Logger accroding logType.
func NewLogger(tag string, logType LogType, logLevel string, filePath string) *Logger {
	if logType == FILE && filePath == "" {
		goPath := os.Getenv("GOPATH")
		if goPath == "" {
			filePath = "/tmp/logs"
		}
		filePath = goPath + "/logs"
	}

	var writer logWriter
	switch logType {
	case STD:
		writer = newStdWriter()
	case FILE:
		writer = newFileWriter(tag, filePath)
	case SYSLOG:
		//writer = newSyslogWriter(tag)
	}

	return &Logger{
		logType:  logType,
		logLevel: stringToLogLevel(logLevel),
		filePath: filePath,
		tag:      tag,
		writer:   writer,
	}
}

// NewStdLogger create a new Logger write output to stand output.
func NewStdLogger(tag string, logLevel string) *Logger {
	return NewLogger(tag, STD, logLevel, "")
}

// NewFileLogger create a Logger write output to specific file.
func NewFileLogger(tag string, logLevel string, filePath string) *Logger {
	return NewLogger(tag, FILE, logLevel, filePath)
}

// NewSyslogLogger create a Logger write output to syslog.
// Default Priority is (LOG_INFO | LOG_LOCAL6)
func NewSyslogLogger(tag string, logLevel string) *Logger {
	return NewLogger(tag, SYSLOG, logLevel, "")
}

// Debug using the default formats for its operands
// and write log with [DEBUG] tag
func Debug(args ...interface{}) {
	if logger.logLevel >= LOG_LEVEL_DEBUG {
		logger.writer.write(logger.formatLogString(LOG_LEVEL_DEBUG, fmt.Sprintln(args...)))
	}
}

// Debugf formats according to a format specifier
// and write log with [DEBUG] tag
func Debugf(format string, args ...interface{}) {
	if logger.logLevel >= LOG_LEVEL_DEBUG {
		logger.writer.write(logger.formatLogString(LOG_LEVEL_DEBUG, fmt.Sprintf(format+"\n", args...)))
	}
}

// Info using the default formats for its operands
// and write log with [INFO] tag
func Info(args ...interface{}) {
	if logger.logLevel >= LOG_LEVEL_INFO {
		logger.writer.write(logger.formatLogString(LOG_LEVEL_INFO, fmt.Sprintln(args...)))
	}
}

// Infof formats according to a format specifier
// and write log with [INFO] tag
func Infof(format string, args ...interface{}) {
	if logger.logLevel >= LOG_LEVEL_INFO {
		logger.writer.write(logger.formatLogString(LOG_LEVEL_INFO, fmt.Sprintf(format+"\n", args...)))
	}
}

// Warn using the default formats for its operands
// and write log with [WARN] tag
func Warn(args ...interface{}) {
	if logger.logLevel >= LOG_LEVEL_WARN {
		logger.writer.write(logger.formatLogString(LOG_LEVEL_WARN, fmt.Sprintln(args...)))
	}
}

// Warnf formats according to a format specifier
// and write log with [WARN] tag
func Warnf(format string, args ...interface{}) {
	if logger.logLevel >= LOG_LEVEL_WARN {
		logger.writer.write(logger.formatLogString(LOG_LEVEL_WARN, fmt.Sprintf(format+"\n", args...)))
	}
}

// Error using the default formats for its operands
// and write log with [ERROR] tag
func Error(args ...interface{}) {
	if logger.logLevel >= LOG_LEVEL_ERROR {
		logger.writer.write(logger.formatLogString(LOG_LEVEL_ERROR, fmt.Sprintln(args...)))
	}
}

// Errorf formats according to a format specifier
// and write log with [ERROR] tag
func Errorf(format string, args ...interface{}) {
	if logger.logLevel >= LOG_LEVEL_ERROR {
		logger.writer.write(logger.formatLogString(LOG_LEVEL_ERROR, fmt.Sprintf(format+"\n", args...)))
	}
}

// Fatal using the default formats for its operands
// and write log with [FATAL] tag
func Fatal(args ...interface{}) {
	if logger.logLevel >= LOG_LEVEL_FATAL {
		logger.writer.write(logger.formatLogString(LOG_LEVEL_FATAL, fmt.Sprintln(args...)))
	}
}

// Fatalf formats according to a format specifier
// and write log with [FATAL] tag
func Fatalf(format string, args ...interface{}) {
	if logger.logLevel >= LOG_LEVEL_FATAL {
		logger.writer.write(logger.formatLogString(LOG_LEVEL_FATAL, fmt.Sprintf(format+"\n", args...)))
	}
}

// Debug using the default formats for its operands
// and write log with [DEBUG] tag
func (l *Logger) Debug(args ...interface{}) {
	if l.logLevel >= LOG_LEVEL_DEBUG {
		l.writer.write(l.formatLogString(LOG_LEVEL_DEBUG, fmt.Sprintln(args...)))
	}
}

// Debugf formats according to a format specifier
// and write log with [DEBUG] tag
func (l *Logger) Debugf(format string, args ...interface{}) {
	if l.logLevel >= LOG_LEVEL_DEBUG {
		l.writer.write(l.formatLogString(LOG_LEVEL_DEBUG, fmt.Sprintf(format+"\n", args...)))
	}
}

// Info using the default formats for its operands
// and write log with [INFO] tag
func (l *Logger) Info(args ...interface{}) {
	if l.logLevel >= LOG_LEVEL_INFO {
		l.writer.write(l.formatLogString(LOG_LEVEL_INFO, fmt.Sprintln(args...)))
	}
}

// Infof formats according to a format specifier
// and write log with [INFO] tag
func (l *Logger) Infof(format string, args ...interface{}) {
	if l.logLevel >= LOG_LEVEL_INFO {
		l.writer.write(l.formatLogString(LOG_LEVEL_INFO, fmt.Sprintf(format+"\n", args...)))
	}
}

// Warn using the default formats for its operands
// and write log with [WARN] tag
func (l *Logger) Warn(args ...interface{}) {
	if l.logLevel >= LOG_LEVEL_WARN {
		l.writer.write(l.formatLogString(LOG_LEVEL_WARN, fmt.Sprintln(args...)))
	}
}

// Warnf formats according to a format specifier
// and write log with [WARN] tag
func (l *Logger) Warnf(format string, args ...interface{}) {
	if l.logLevel >= LOG_LEVEL_WARN {
		l.writer.write(l.formatLogString(LOG_LEVEL_WARN, fmt.Sprintf(format+"\n", args...)))
	}
}

// Error using the default formats for its operands
// and write log with [ERROR] tag
func (l *Logger) Error(args ...interface{}) {
	if l.logLevel >= LOG_LEVEL_ERROR {
		l.writer.write(l.formatLogString(LOG_LEVEL_ERROR, fmt.Sprintln(args...)))
	}
}

// Errorf formats according to a format specifier
// and write log with [ERROR] tag
func (l *Logger) Errorf(format string, args ...interface{}) {
	if l.logLevel >= LOG_LEVEL_ERROR {
		l.writer.write(l.formatLogString(LOG_LEVEL_ERROR, fmt.Sprintf(format+"\n", args...)))
	}
}

// Fatal using the default formats for its operands
// and write log with [FATAL] tag
func (l *Logger) Fatal(args ...interface{}) {
	if l.logLevel >= LOG_LEVEL_FATAL {
		l.writer.write(l.formatLogString(LOG_LEVEL_FATAL, fmt.Sprintln(args...)))
	}
}

// Fatalf formats according to a format specifier
// and write log with [FATAL] tag
func (l *Logger) Fatalf(format string, args ...interface{}) {
	if l.logLevel >= LOG_LEVEL_FATAL {
		l.writer.write(l.formatLogString(LOG_LEVEL_FATAL, fmt.Sprintf(format+"\n", args...)))
	}
}

// formatLogString add the essential infomation to string
func (l *Logger) formatLogString(level LogLevel, log string) string {
	now := time.Now().Format("2006-01-02 15:04:05.000")
	if l.logSimple {

		var funcInfo string
		if l.logLevel == LOG_LEVEL_DEBUG {
			_, file, line, ok := runtime.Caller(2)
			if ok {
				funcInfo = fmt.Sprintf(" [%s:%d]", filepath.Base(file), line)
			}
		}

		log = strings.Replace(log, "\n", " ", -1)

		return fmt.Sprintf("%s [%s]: %s%s\n", now, logLevelToString(level), log, funcInfo)

	} else {
		hostname, _ := os.Hostname()
		pid := os.Getpid()
		if l.logLevel == LOG_LEVEL_DEBUG {
			funcPC, file, line, ok := runtime.Caller(2)
			var funcInfo string
			if ok {
				funcInfo = fmt.Sprintf("%s:%d:%s", filepath.Base(file), line, runtime.FuncForPC(funcPC).Name())
			}
			return fmt.Sprintf("%s %s %s[%d]: [%s] [%s]: %s", now, hostname, l.tag, pid, logLevelToString(level), funcInfo, log)
		}
		return fmt.Sprintf("%s %s %s[%d]: [%s]: %s", now, hostname, l.tag, pid, logLevelToString(level), log)
	}
}

// logLevelToString convert LogLevel to string
func logLevelToString(level LogLevel) string {
	switch level {
	case LOG_LEVEL_FATAL:
		return "FATAL"
	case LOG_LEVEL_ERROR:
		return "ERROR"
	case LOG_LEVEL_WARN:
		return "WARN "
	case LOG_LEVEL_INFO:
		return "INFO "
	case LOG_LEVEL_DEBUG:
		return "DEBUG"
	}
	return "ALL  "
}

// stringToLogType convert string logType to LogType
func stringToLogType(lt string) LogType {
	switch strings.ToLower(lt) {
	case "std":
		return STD
	case "file":
		return FILE
	case "syslog":
		return SYSLOG
	}
	panic("Invalid log type")
}

// stringToLogLevel convert string loglevel to LogLevel
func stringToLogLevel(level string) LogLevel {
	switch strings.ToLower(level) {
	case "fatal":
		return LOG_LEVEL_FATAL
	case "error":
		return LOG_LEVEL_ERROR
	case "warn":
		return LOG_LEVEL_WARN
	case "info":
		return LOG_LEVEL_INFO
	case "debug":
		return LOG_LEVEL_DEBUG
	}
	panic("Invalid log level")
}
