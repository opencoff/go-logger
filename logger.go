// Copyright 2009 The Go Authors. All rights reserved.
//
// Changes Copyright 2012, Sudhi Herle <sudhi -at- herle.net>
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package Logger is an enhanced derivative of the Golang 'log'
// package.
//
// The list of enhancements are:
//
//   - All I/O is done in an asynchronous go-routine; thus, the caller
//     does not incur any overhead beyond the formatting of the
//     strings.
//
//   - Logger and timestamps:
//
//   - Date, time are optional logging elements; the flags
//     `Ldate`, `Ltime` when used in the 'flag' argument
//     of the constructor functions will determine if date OR time
//     is logged.
//
//   - Logger implicitly works and logs date/time in UTC (NEVER localtime).
//
//   - The time is by default logged at millisecond resolution;
//     the flag `Lmicroseconds` causes timestamps to be printed in
//     microsecond resolution.
//
//   - A Logger instance can log relative timestamps with the flag
//     `Lreltime`. Relative timestamps are always logged at the full resolution
//     of the available OS time source (Nanoseconds on major platforms).
//     Use of `Lreltime` supercedes `Ldate` and `Ltime`.
//
//   - *NB*: when `Lreltime` is in effect, the very first log
//     message will have a full timestamp and *NOT* the relative timestamp.
//     This is to ensure that there is a frame of reference for future
//     log messages. Similarly, when log files are rotated, the first line after
//     rotation will have the full absolute timestamp.
//
//   - Log levels define a heirarchy (from most-verbose to
//     least-verbose):
//
//     LOG_DEBUG
//     LOG_INFO
//     LOG_WARN
//     LOG_ERR
//     LOG_CRIT
//     LOG_EMERG
//
//   - An instance of a logger is configured with a given log level;
//     and it only prints log messages "above" the configured level.
//     e.g., if a logger is configured with level of `INFO`, then it will
//     print all log messages with `INFO` and higher priority;
//     in particular, it won't print `DEBUG` messages.
//
//   - A single program can have multiple loggers; each with a
//     different priority.
//
//   - The logger method Backtrace() will print a stack backtrace to
//     the configured output stream. Log levels are NOT
//     considered when backtraces are printed.
//
//   - The `Panic()` and `Fatal()` logger methods implicitly print the
//     stack backtrace (upto 5 levels).
//
//   - `DEBUG, ERR, CRIT` log outputs (via `Debug(), Err() and Crit()`
//     methods) also print the source file location from whence they
//     were invoked. `Lfullpath` flag is honored for the backtrace.
//
//   - A Logger instance can be turned into a stdlib's Logger via the
//     `Logger.StdLogger()` method.
//     instance.
//
//   - Callers can create a new logger instance if they have an
//     io.writer instance of their own - in case the existing output
//     streams (File and Syslog) are insufficient.
//
//   - Any logger instance can create child-loggers with a different
//     priority and prefix (but same destination); this is useful in large
//     programs with different modules.
//
//   - Compressed log rotation based on daily ToD (configurable ToD) -- only
//     available for file-backed destinations.
package logger

import (
	"compress/gzip"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	stdlog "log"
	"log/syslog"
	"os"
	"path"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// These flags define which text to prefix to each log entry generated by the Logger.
const (
	// Bits or'ed together to control what's printed. There is no control over the
	// order they appear (the order listed here) or the format they present (as
	// described in the comments).  A colon appears after these items:
	//  2009/01/23 01:23:23.123123 /a/b/c/d.go:23: message
	Ldate         = 1 << iota // the date: 2009/01/23
	Ltime                     // the time: 01:23:23
	Lmicroseconds             // microsecond resolution: 01:23:23.123123.  assumes Ltime.
	Lfileloc                  // put file name and line number in the log
	Lfullpath                 // full file path and line number: /a/b/c/d.go:23
	Lreltime                  // print relative time from start of program

	// Internal flags
	lSyslog // set to indicate that output destination is syslog; Ldate|Ltime|Lmicroseconds are ignored
	lPrefix // set if prefix is non-zero
	lClose  // close the file when done
	lSublog // Set if this is a sub-logger
	lRotate // Rotate the logs

	Lstdflag = Ldate | Ltime // initial values for the standard logger
)

// Log priority. These form a heirarchy:
//
//	LOG_DEBUG
//	LOG_INFO
//	LOG_WARN
//	LOG_ERR
//	LOG_CRIT
//	LOG_EMERG
//
// An instance of a logger is configured with a given log level;
// and it only prints log messages "above" the configured level.
type Priority int

const (
	// Maximum number of daily logs we will store
	_MAX_LOGFILES     = 7
	_PANIC_BACKTRACES = 6

	// line length of a log buffer
	_LOGBUFSZ = 256
)

// Log Priorities
const (
	LOG_NONE Priority = iota
	LOG_DEBUG
	LOG_INFO
	LOG_WARN
	LOG_ERR
	LOG_CRIT
	LOG_EMERG

	// keep in the end
	logMax
)

// Map string names to actual priority levels. Useful for taking log
// levels defined in config files and turning them into usable
// priorities.
var prioName = map[string]Priority{
	"LOG_DEBUG": LOG_DEBUG,
	"LOG_INFO":  LOG_INFO,
	"LOG_WARN":  LOG_WARN,
	"LOG_ERR":   LOG_ERR,
	"LOG_ERROR": LOG_ERR,
	"LOG_CRIT":  LOG_CRIT,
	"LOG_EMERG": LOG_EMERG,
	"LOG_NONE":  LOG_NONE,

	"DEBUG":     LOG_DEBUG,
	"INFO":      LOG_INFO,
	"WARNING":   LOG_WARN,
	"WARN":      LOG_WARN,
	"ERR":       LOG_ERR,
	"ERROR":     LOG_ERR,
	"CRIT":      LOG_CRIT,
	"CRITICAL":  LOG_CRIT,
	"EMERG":     LOG_EMERG,
	"EMERGENCY": LOG_EMERG,
	"NONE":      LOG_NONE,
}

// Map log priorities to their string names
var prioString = map[Priority]string{
	LOG_DEBUG: "DEBUG",
	LOG_INFO:  "INFO",
	LOG_WARN:  "WARNING",
	LOG_ERR:   "ERROR",
	LOG_CRIT:  "CRITICAL",
	LOG_EMERG: "EMERGENCY",
	LOG_NONE:  "NONE",
}

func (p Priority) String() string {
	if p < logMax {
		return prioString[p]
	}
	return fmt.Sprintf("invalid-prio-%d", int(p))
}

// Since we now have sub-loggers, we need a way to keep the output
// channel and its close status together. This struct keeps the
// abstraction together. There is only ever _one_ instance of this
// struct in a top-level logger.
type outch struct {
	logch  chan qev // buffered channel
	closed atomic.Bool
	wg     sync.WaitGroup
	pool   sync.Pool
}

// A Logger represents an active logging object that generates lines of
// output to an io.Writer.  Each logging operation makes a single call to
// the Writer's Write method.  A Logger can be used simultaneously from
// multiple goroutines; it guarantees serialized access to the Writer.
type Logger interface {
	// New creates a sub-logger with revised priority and prefix
	New(prefix string, prio Priority) Logger

	// Close flushes pending I/O and closes this logger instance
	Close() error

	// Loggable returns true if we the logger can write a log at
	// level 'p'
	Loggable(p Priority) bool

	// Fatal writes a log message with stack backtrace and invokes panic()
	Fatal(format string, v ...interface{})

	// Crit write a log message iff the logger priority is LOG_CRIT or higher
	Crit(format string, v ...interface{})

	// Error writes a log message iff the logger priority is LOG_ERR or higher
	Error(format string, v ...interface{})

	// Warn writes a log message iff the logger priority is LOG_WARN or higher
	Warn(format string, v ...interface{})

	// Info writes a log message iff the logger priority is LOG_INFO or higher
	Info(format string, v ...interface{})

	// Debug writes a log message iff the logger priority is LOG_DEBUG or higher
	Debug(format string, v ...interface{})

	// Prio returns the current logger priority
	Prio() Priority

	// Prefix returns the current logger prefix
	Prefix() string

	// Convert this logger instance into one that looks like the stdlib Logger
	StdLogger() *stdlog.Logger
}

// A RotatableLogger represents an active _file backed_ Logger instance
type RotatableLogger interface {
	Logger

	EnableRotation(hh, mm, ss int, keep int) error
}

// file and syslog backed logger
type xLogger struct {
	mu     sync.Mutex // ensures atomic changes to properties
	prio   Priority   // Logging priority
	prefix string     // prefix to write at beginning of each line
	flag   int        // properties
	out    io.Writer  // destination for output
	name   string     // file name for file backed logs

	relstart atomic.Bool
	start    time.Time // start time when the logger was created
	rot_n    int       // number of days of logs to keep

	ch *outch // output chan

	// cached pointer of stdlogger
	stdlogger atomic.Pointer[stdlog.Logger]
}

var _ Logger = &xLogger{}
var _ RotatableLogger = &xLogger{}

func barePrefix(s string) string {
	if s[0] == '[' {
		s = s[1:]
	}
	if i := strings.LastIndex(s, "] "); i > 0 {
		s = s[:i]
	}
	return s
}

func defaultFlag(flag int) int {
	if flag == 0 {
		flag = Lstdflag
	}

	// Reltime overrides any date+timestamp
	// We however retain Lmicroseconds
	if (flag & Lreltime) != 0 {
		flag &= ^(Ldate | Ltime)
	}

	if (flag & Lfullpath) > 0 {
		flag |= Lfileloc
	}

	flag &= ^(lSyslog | lPrefix | lClose)
	return flag
}

// Convert a string to equivalent Priority
func ToPriority(s string) (p Priority, ok bool) {
	s = strings.ToUpper(s)
	p, ok = prioName[s]
	return
}

// make a new logger instance
func newLogger(out io.Writer, prio Priority, pref string, flag int) *xLogger {
	if len(pref) > 0 {
		flag |= lPrefix
		pref = fmt.Sprintf("[%s] ", pref)
	}

	// default priority is important messages
	if prio <= 0 {
		prio = LOG_WARN
	}

	ll := &xLogger{
		prio:   prio,
		prefix: pref,
		flag:   flag,
		out:    out,
		start:  time.Now().UTC(),
		ch: &outch{
			logch: make(chan qev, runtime.NumCPU()),
			pool: sync.Pool{
				New: func() any { return make([]byte, 0, _LOGBUFSZ) },
			},
		},
	}

	ll.dprintf(0, LOG_INFO, "Logger at level %s started.", ll.prio.String())
	ll.ch.wg.Add(1)
	go ll.qrunner()
	return ll
}

// Creates a new Logger instance at the given priority. The log output is
// sent to 'out' - an `io.Writer`.
// The prefix appears at the beginning of each generated log line.
// The flag argument defines the logging properties such as timestamps,
// file & line numbers.
func New(out io.Writer, prio Priority, prefix string, flag int) (Logger, error) {
	flag = defaultFlag(flag)
	return newLogger(out, prio, prefix, defaultFlag(flag)), nil
}

// Creates a new file-backed logger instance at the given priority.
// This function erases the previous file contents.  The prefix appears
// at the beginning of each generated log line.  The flag argument defines
// the logging properties such as timestamps, file & line numbers.
//
// NB: This is the only constructor that allows you to subsequently
// configure a log-rotator.
func NewFilelog(file string, prio Priority, prefix string, flag int) (RotatableLogger, error) {
	// We use O_RDWR because we will likely rotate the file and it
	// will help us to seek(0) and read the logs for purposes of
	// compressing it.
	logfd, err := os.OpenFile(file, os.O_RDWR|os.O_CREATE|os.O_TRUNC|os.O_SYNC, 0600)
	if err != nil {
		s := fmt.Sprintf("Can't open log file '%s': %s", file, err)
		return nil, errors.New(s)
	}

	ll := newLogger(logfd, prio, prefix, defaultFlag(flag)|lClose)
	ll.name = file
	return ll, nil
}

// Creates a new syslog-backed logger instance at the given priority.
// The prefix appears at the beginning of each generated log line.
// The flag argument defines the logging properties such as timestamps,
// file & line numbers.
//
// *NB*: This is not supported/tested on Win32/Win64.
func NewSyslog(prio Priority, prefix string, flag int) (Logger, error) {
	flag = defaultFlag(flag)
	tag := path.Base(os.Args[0])

	wr, err := syslog.New(syslog.LOG_NOTICE|syslog.LOG_DAEMON, tag)
	if err != nil {
		return nil, fmt.Errorf("%s: syslog: %w", tag, err)
	}

	return newLogger(wr, prio, prefix, flag|lSyslog), nil
}

// Creates a new logging instance. The log destination is controlled by the
// 'name' argument. It can be one of:
//
//   - "NONE": sends output to the equivalent of /dev/null. ie "null output logger"
//   - "SYSLOG": sends output to `syslog(3)`
//   - "STDOUT": sends output to the calling process' `STDOUT` stream
//   - "STDERR": sends output to the calling process' `STDERR` stream
//   - file path: sends output to the named file.
//
// The prefix appears at the beginning of each generated log line.
// The flag argument defines the logging properties such as timestamps,
// file & line numbers.
func NewLogger(name string, prio Priority, prefix string, flag int) (Logger, error) {
	flag = defaultFlag(flag)
	switch strings.ToUpper(name) {
	case "NONE":
		return newNullLogger(prefix, prio), nil

	case "SYSLOG":
		return NewSyslog(prio, prefix, flag)

	case "STDOUT":
		return New(os.Stdout, prio, prefix, flag)

	case "STDERR":
		return New(os.Stderr, prio, prefix, flag)

	default:
		return NewFilelog(name, prio, "", flag)
	}
}

// Create a new Sub-Logger with a different prefix and priority.
// This is useful when different components in a large program want
// their own log-prefix (for easier debugging)
func (l *xLogger) New(prefix string, prio Priority) Logger {
	if prio <= 0 {
		prio = l.prio
	}

	nl := &xLogger{
		prio: prio,
		flag: l.flag | lSublog,
		out:  l.out,

		// We use the same start time for relative-timestamps; the output
		// destination is the same regardless of whether a Logger instance
		// is the parent instance or one of the descendants.
		start: l.start,
		ch:    l.ch,
	}

	if len(prefix) > 0 {
		if (l.flag & lPrefix) != 0 {
			oldpref := barePrefix(l.prefix)
			nl.prefix = fmt.Sprintf("[%s.%s] ", oldpref, prefix)
		} else {
			nl.prefix = fmt.Sprintf("[%s] ", prefix)
		}
	}

	return nl
}

// Close the logger and wait for I/O to complete
func (l *xLogger) Close() error {
	if 0 != (l.flag & lSublog) {
		return nil
	}

	if !l.ch.closed.Swap(true) {
		close(l.ch.logch)
		l.ch.wg.Wait()

		// Log when we close the logger and include the caller info
		l.dprintf(1, LOG_INFO, "xLogger at level %s closed.", l.prio.String())

		if (l.flag & lClose) != 0 {
			if fd, ok := l.out.(io.WriteCloser); ok {
				return fd.Close()
			}
		}
	}
	return nil
}

// Enable log rotation to happen every day at 'hh:mm:ss' (24-hour
// representation); keep upto 'max' previous logs. Rotated logs are
// gzip-compressed.
func (l *xLogger) EnableRotation(hh, mm, ss int, max int) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if (l.flag & lClose) == 0 {
		return fmt.Errorf("%s: logger is not file backed", l.prefix)
	}

	if hh < 0 || hh > 23 || mm < 0 || mm > 59 || ss < 0 || ss > 59 {
		return fmt.Errorf("invalid rotation config %d:%d.%d", hh, mm, ss)
	}

	n := time.Now().UTC()

	// This is the time for next file-rotation
	x := time.Date(n.Year(), n.Month(), n.Day(), hh, mm, ss, 0, n.Location())

	// For debugging log-rotate logic
	//x  = n.Add(2 * time.Minute)

	// If we ended up in "yesterday", then set the reminder
	// for the "next day"
	if x.Before(n) {
		x = x.Add(24 * time.Hour)
	}

	if max <= 0 {
		max = _MAX_LOGFILES
	}

	l.Info("logger: Enabled daily log-rotation (keep %d days); first rotation at %s",
		max, x.Format(time.RFC822Z))

	l.flag |= lRotate
	l.rot_n = max
	d := x.Sub(n)
	time.AfterFunc(d, l.qtimer)
	return nil
}

// Enqueue a log-write to happen asynchronously
func (l *xLogger) Output(calldepth int, prio Priority, s string, v ...interface{}) {
	if calldepth > 0 {
		calldepth += 1
	}

	t := l.ofmt(calldepth, prio, s, v...)
	l.qwrite(t)
}

// Dump stack backtrace for 'depth' levels
// Backtrace is of the form "file:line [func name]".
// NB: The absolute pathname of the file is used in the backtrace;
// regardless of the logger flags requesting shortfile.
func (l *xLogger) Backtrace(depth int) {
	s := backTrace(depth+1, l.flag)
	l.qwrite([]byte(s))
}

// Predicate that returns true if we can log at level prio
func (l *xLogger) Loggable(prio Priority) bool {
	return l.prio > LOG_NONE && prio >= l.prio
}

// Printf calls l.Output to print to the logger.
// Arguments are handled in the manner of fmt.Printf.
func (l *xLogger) Printf(format string, v ...interface{}) {
	l.Output(0, LOG_INFO, format, v...)
}

// Panicf is equivalent to l.Printf() followed by a call to panic().
func (l *xLogger) Panic(format string, v ...interface{}) {
	bt := backTrace(_PANIC_BACKTRACES, l.flag)
	s := fmt.Sprintf(format, v...)
	l.Output(2, LOG_EMERG, "%s:\n%s", s, bt)
	l.Close()
	panic(s)
}

// Fatalf is equivalent to l.Printf() followed by a call to os.Exit(1).
func (l *xLogger) Fatal(format string, v ...interface{}) {
	l.Panic(format, v...)
}

// Crit prints logs at level CRIT
func (l *xLogger) Crit(format string, v ...interface{}) {
	if l.Loggable(LOG_CRIT) {
		l.Output(2, LOG_CRIT, format, v...)
	}
}

// Err prints logs at level ERR
func (l *xLogger) Error(format string, v ...interface{}) {
	if l.Loggable(LOG_ERR) {
		l.Output(2, LOG_ERR, format, v...)
	}
}

// Warn prints logs at level WARNING
func (l *xLogger) Warn(format string, v ...interface{}) {
	if l.Loggable(LOG_WARN) {
		l.Output(0, LOG_WARN, format, v...)
	}
}

// Info prints logs at level INFO
func (l *xLogger) Info(format string, v ...interface{}) {
	if l.Loggable(LOG_INFO) {
		l.Output(0, LOG_INFO, format, v...)
	}
}

// Debug prints logs at level INFO
func (l *xLogger) Debug(format string, v ...interface{}) {
	if l.Loggable(LOG_DEBUG) {
		l.Output(2, LOG_DEBUG, format, v...)
	}
}

// Manipulate properties of loggers

// Return priority of this logger
func (l *xLogger) Prio() Priority {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.prio
}

// Flags returns the output flags for the logger.
func (l *xLogger) Flags() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.flag
}

// Prefix returns the output prefix for the logger.
func (l *xLogger) Prefix() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.prefix
}

// -- Internal functions --

func (l *xLogger) formatHeader(out []byte, t time.Time) []byte {
	if (l.flag & Lreltime) == 0 {
		return timestamp(out, t, l.flag)
	}

	// if this is the first time, do the full time stamp so we have a
	// baseline reference
	if ok := l.relstart.Swap(true); !ok {
		return timestamp(out, t, l.flag|Ldate|Ltime)
	}
	d := t.Sub(l.start)
	return fmt.Appendf(out, "+%s", d.String())
}

// Output formats the output for a logging event.  The string s contains
// the text to print after the prefix specified by the flags of the
// Logger.  A newline is appended if the last character of s is not
// already a newline.  Calldepth is used to recover the PC and is
// provided for generality, although at the moment on all pre-defined
// paths it will be 2.
func (l *xLogger) ofmt(calldepth int, prio Priority, s string, v ...interface{}) []byte {
	b := l.getBuf()

	if len(s) == 0 {
		return b
	}

	// Put the timestamp and priority only if we are NOT syslog
	if (l.flag & lSyslog) == 0 {
		now := time.Now().UTC()
		b = fmt.Appendf(b, "<%d>:", prio)
		b = l.formatHeader(b, now)
		b = append(b, ' ')
	}

	if (l.flag & lPrefix) != 0 {
		b = append(b, l.prefix...)
	}

	if calldepth > 0 && (l.flag&Lfileloc) > 0 {
		var ok bool
		_, file, line, ok := runtime.Caller(calldepth)
		if !ok {
			file = "???"
			line = 0
		}

		// if caller requested short names, trim it
		if (l.flag & Lfullpath) == 0 {
			file = path.Base(file)
		}
		b = fmt.Appendf(b, "(%s:%d) ", file, line)
	}

	b = fmt.Appendf(b, s, v...)
	if len(b) > 0 && b[len(b)-1] != '\n' {
		b = append(b, '\n')
	}

	return b
}

// printf style logger that write directly to the underlying writer without going
// through the queue
func (l *xLogger) dprintf(depth int, pr Priority, s string, args ...interface{}) {
	if depth > 0 {
		depth += 1
	}
	x := l.ofmt(depth, pr, s, args...)
	l.out.Write(x)

	// don't forget to return the buffer to the pool
	l.putBuf(x)
}

// type of event that goes into the qrunner channel
type qevt int

const (
	_QEV_LOG   = iota // event type is to log a message
	_QEV_TIMER        // event signals timer expiry (log rotation)
)

// qev records the action to be taken by the qrunner goroutine
type qev struct {
	ty  qevt
	buf []byte
}

// Enqueue a write to be flushed by qrunner()
// Senders are responsible for closing the channel - but only once.
func (l *xLogger) qwrite(b []byte) {
	if !l.ch.closed.Load() {
		l.ch.logch <- qev{_QEV_LOG, b}
	}
}

// Enqueue a timer expirty to be handled by qrunner()
func (l *xLogger) qtimer() {
	if !l.ch.closed.Load() {
		l.ch.logch <- qev{_QEV_TIMER, nil}
	}
}

// Go routine to do async log writes
func (l *xLogger) qrunner() {
	defer l.ch.wg.Done()

	for e := range l.ch.logch {
		switch e.ty {
		case _QEV_LOG:
			l.out.Write(e.buf)
			l.putBuf(e.buf)

		case _QEV_TIMER:
			if 0 != (l.flag & lRotate) {
				l.rotateLog()

				// reset the counter so the first log message has full time stamp.
				l.relstart.Store(false)

				l.dprintf(0, LOG_INFO, "Log rotation complete. Next rotate in +24 hours.")
				time.AfterFunc(24*time.Hour, l.qtimer)
			}
		default:
			l.dprintf(0, LOG_ERR, "logger: unknown event type %d in qrunner", e.ty)
		}
	}
}

func (l *xLogger) getBuf() []byte {
	b := l.ch.pool.Get()
	return b.([]byte)
}

func (l *xLogger) putBuf(b []byte) {
	l.ch.pool.Put(b[:0])
}

// Rotate current file out
func (l *xLogger) rotateLog() {
	var gfd *gzip.Writer
	var wfd *os.File
	var err error
	var errstr string
	var gz, gztmp string

	fd, ok := l.out.(*os.File)
	if !ok {
		panic("logger: rotatelog wants a file - but seems to be corrupted")
	}

	errf := func(err error, s string, args ...interface{}) string {
		s = fmt.Sprintf("logger %s: logrotate: %s", l.prefix, s)
		s = fmt.Sprintf(s, args...)
		s = fmt.Sprintf("%s: %s", s, err)
		return s
	}

	if err = fd.Sync(); err != nil {
		errstr = errf(err, "%s flush", l.name)
		goto fail
	}

	if _, err = fd.Seek(0, 0); err != nil {
		errstr = errf(err, "%s seek0 to start rotation", l.name)
		goto fail
	}

	// First rotate the older files
	if err = rotatefile(l.name, l.rot_n); err != nil {
		errstr = errf(err, "rotate")
		goto fail
	}

	// Now, compress the current file and store it
	gz = fmt.Sprintf("%s.0.gz", l.name)
	gztmp = fmt.Sprintf("%s.%x", l.name, rand64())

	if wfd, err = os.OpenFile(gztmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644); err != nil {
		errstr = errf(err, "% create", gztmp)
		goto fail
	}

	if gfd, err = gzip.NewWriterLevel(wfd, 9); err != nil {
		errstr = errf(err, "%s gzip", gztmp)
		goto fail1
	}

	if _, err = io.Copy(gfd, fd); err != nil {
		errstr = errf(err, "%s gzip copy", gztmp)
		goto fail1
	}

	if err = gfd.Close(); err != nil {
		errstr = errf(err, "%s gzip close", gztmp)
		goto fail1
	}

	if err = wfd.Close(); err != nil {
		errstr = errf(err, "%s close", gztmp)
		goto fail2
	}

	if err = os.Rename(gztmp, gz); err != nil {
		errstr = errf(err, "%s to %s rename", gztmp, gz)
		goto fail2
	}

	if err = fd.Truncate(0); err != nil {
		errstr = errf(err, "%s truncate", l.name)
		goto fail
	}

	if _, err = fd.Seek(0, 0); err != nil {
		errstr = errf(err, "%s seek0", l.name)
		goto fail
	}

	return

fail1:
	wfd.Close()

fail2:
	os.Remove(gztmp)

	// When all else fails - start to log to stderr - hopefully daemons started by
	// supervisory regimes will redirect the log messages to syslog or some other place.
fail:
	fd.Close()
	l.out = os.Stderr
	l.Error(errstr)
	l.Error("switching to STDERR for future logs ..")
	l.flag &= ^lClose
	return
}

// Cheap integer to fixed-width decimal ASCII.  Give a negative width to avoid zero-padding.
func itoa(out []byte, i int, wid int) []byte {
	var u uint = uint(i)
	var b [32]byte

	bp := len(b) - 1
	for u >= 10 || wid > 1 {
		wid--
		q := u / 10
		b[bp] = byte('0' + u - q*10)
		bp--
		u = q
	}
	// u < 10
	b[bp] = byte('0' + u)
	return append(out, b[bp:]...)
}

// make a printable timestamp out of 't' using the flags 'fl'
func timestamp(out []byte, t time.Time, fl int) []byte {
	if fl&(Ldate|Ltime|Lmicroseconds) == 0 {
		return out
	}

	date := false
	if fl&Ldate != 0 {
		year, month, day := t.Date()

		out = itoa(out, year, 4)
		out = append(out, '/')
		out = itoa(out, int(month), 2)
		out = append(out, '/')
		out = itoa(out, day, 2)
		date = true
	}

	if fl&(Ltime|Lmicroseconds) != 0 {
		hour, min, sec := t.Clock()

		// this is now the microsec offset within the second
		microsecs := t.Nanosecond() / 1000

		if date {
			out = append(out, ' ')
		}

		out = itoa(out, hour, 2)
		out = append(out, ':')
		out = itoa(out, min, 2)
		out = append(out, ':')
		out = itoa(out, sec, 2)
		out = append(out, '.')

		if fl&Lmicroseconds != 0 {
			out = itoa(out, microsecs, 6)
		} else {
			out = itoa(out, microsecs/1000, 3)
		}
	}
	return out
}

// Rotate files of the form fn.NN where 0 <= NN < max
// Delete the oldest file (NN == max-1)
func rotatefile(fn string, max int) error {
	old := fmt.Sprintf("%s.%d.gz", fn, max-1)
	if err := os.Remove(old); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("%s rm: %w", old, err)
	}

	// Now, we iterate from max-1 to 0
	for i := max - 1; i > 0; i -= 1 {
		older := old
		old = fmt.Sprintf("%s.%d.gz", fn, i-1)
		err, ok := exists(old)
		if err != nil {
			return fmt.Errorf("%s rm?: %w", old, err)
		} else if !ok {
			continue
		}

		if err = os.Rename(old, older); err != nil {
			return fmt.Errorf("%s to %s rename: %w", old, older, err)
		}
	}
	return nil
}

// Predicate - returns true if file 'fn' exists; false otherwise
func exists(fn string) (error, bool) {
	fi, err := os.Stat(fn)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false
		}
		return err, false
	}

	if fi.Mode().IsRegular() {
		return nil, true
	}

	return fmt.Errorf("%s not a regular file", fn), false
}

// 64 bit random integer
func rand64() uint64 {
	var b [8]byte
	rand.Read(b[:])
	return binary.BigEndian.Uint64(b[:])
}

// fetch backtrace info to 'depth' callers
func backTrace(depth, flag int) string {
	var wr strings.Builder
	var pcv [64]uintptr

	// runtime.Callers() requires a pre-created array.
	n := runtime.Callers(3, pcv[:])
	if n == 0 {
		wr.WriteString("no backtrace frames!")
		return wr.String()
	}

	if depth == 0 {
		depth = n
	}

	if n > depth {
		n = depth
	}

	frames := runtime.CallersFrames(pcv[:n])

	wr.WriteString("--backtrace:\n")

	for {
		var s string

		n -= 1
		f, more := frames.Next()
		file := f.File
		if (flag & Lfullpath) == 0 {
			file = path.Base(file)
		}

		if fn := f.Func; fn != nil {
			off := f.PC - fn.Entry()
			s = fmt.Sprintf("\t%2d: %q:%d [%s +%#x]\n", n, file, f.Line, fn.Name(), off)
		} else {
			s = fmt.Sprintf("\t%2d: %q:%d [unknown addr %#x]\n", n, file, f.Line, f.PC)
		}
		wr.WriteString(s)

		if !more {
			break
		}
	}
	wr.WriteString("--end backtrace\n")

	return wr.String()
}

// null writer
type nullWriter struct{}

func (n *nullWriter) Write(b []byte) (int, error) {
	return len(b), nil
}

// vim: ft=go:sw=8:ts=8:noexpandtab:tw=98:
