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
	var g *stdlog.Logger

	if g = l.stdlogger.Load(); g == nil {
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
		if 0 != (l.flag & Lfileloc) {
			if 0 != (l.flag & Lfullpath) {
				fl |= stdlog.Llongfile
			} else {
				fl |= stdlog.Lshortfile
			}
		}

		// here first argument 'l' is the io.Writer; we provide its
		// interface implementation below.
		g = stdlog.New(l, l.prefix, fl)

		if !l.stdlogger.CompareAndSwap(nil, g) {
			g = l.stdlogger.Load()
		}
	}
	return g
}

// We only provide an ioWriter implementation for stdlogger
func (l *Logger) Write(b []byte) (int, error) {
	l.qwrite(string(b))
	return len(b), nil
}

// vim: ft=go:sw=8:ts=8:noexpandtab:tw=98:
