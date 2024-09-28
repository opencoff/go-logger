// stdwrapper.go - wrapper around my logger to make it compatible
// with stdlib log.Logger.
//
// Changes Copyright 2012, Sudhi Herle <sudhi -at- herle.net>
// This code is licensed under the same terms as the golang core.

package logger

import (
	stdlog "log"
)

func fl2std(flag int) int {
	fl := stdlog.LUTC
	if 0 != (flag & Ldate) {
		fl |= stdlog.Ldate
	}
	if 0 != (flag & Ltime) {
		fl |= stdlog.Ltime
	}
	if 0 != (flag & Lmicroseconds) {
		fl |= stdlog.Lmicroseconds
	}
	if 0 != (flag & Lfileloc) {
		if 0 != (flag & Lfullpath) {
			fl |= stdlog.Llongfile
		} else {
			fl |= stdlog.Lshortfile
		}
	}

	return fl
}

// Return an instance of self that satisfies stdlib logger
func (l *xLogger) StdLogger() *stdlog.Logger {
	var g *stdlog.Logger

	if g = l.stdlogger.Load(); g == nil {
		// here first argument 'l' is the io.Writer; we provide its
		// interface implementation below.
		g = stdlog.New(l, l.prefix, fl2std(l.flag))

		if !l.stdlogger.CompareAndSwap(nil, g) {
			g = l.stdlogger.Load()
		}
	}
	return g
}

// We only provide an ioWriter implementation for stdlogger
func (l *xLogger) Write(b []byte) (int, error) {
	l.qwrite(b)
	return len(b), nil
}

// provide implementations for the nul logger as well

func (e *emptyLogger) StdLogger() *stdlog.Logger {
	return stdlog.New(e, e.prefix, fl2std(0))
}

func (e *emptyLogger) Write(b []byte) (int, error) {
	return len(b), nil
}

// vim: ft=go:sw=8:ts=8:noexpandtab:tw=98:
