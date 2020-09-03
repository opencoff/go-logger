// stdwrapper.go - wrapper around my logger to make it compatible
// with stdlib log.Logger.
//
// Changes Copyright 2012, Sudhi Herle <sudhi -at- herle.net>
// This code is licensed under the same terms as the golang core.

package logger

import (
	stdlog "log"
)

// Return an instance of self that satisfies stdlib logger
func (l *Logger) StdLogger() *stdlog.Logger {

	l.mu.Lock()
	defer l.mu.Unlock()

	gl := l.gl
	if gl == nil {
		fl := stdlog.LUTC
		if 0 != (l.flag & Ldate) {
			fl |= stdlog.Ldate
		}
		if 0 != (l.flag & Ltime) {
			fl |= stdlog.Ltime
		}
		if 0 != (l.flag & Lmicroseconds) {
			fl |= stdlog.Lmicroseconds
		}
		if 0 != (l.flag & Llongfile) {
			fl |= stdlog.Llongfile
		}
		if 0 != (l.flag & Lshortfile) {
			fl |= stdlog.Lshortfile
		}

		// here first argument 'l' is the io.Writer; we provide its
		// interface implementation below.
		gl = stdlog.New(l, l.prefix, fl)
		l.gl = gl
	}

	return gl
}

// We only provide an ioWriter implementation for stdlogger
func (l *Logger) Write(b []byte) (int, error) {
	l.qwrite(string(b))
	return len(b), nil
}

// vim: ft=go:sw=8:ts=8:noexpandtab:tw=98:
