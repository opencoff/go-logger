// nul.go - Nul Logger instance
//
// Copyright 2009 The Go Authors. All rights reserved.
//
// Changes Copyright 2012, Sudhi Herle <sudhi -at- herle.net>
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package logger

type emptyLogger struct {
	prio   Priority
	prefix string
}

var _ Logger = &emptyLogger{}

func newNullLogger(pref string, prio Priority) *emptyLogger {
	return &emptyLogger{
		prio:   prio,
		prefix: pref,
	}
}

func (e *emptyLogger) New(pref string, prio Priority) Logger {
	return newNullLogger(pref, prio)
}

func (e *emptyLogger) Close() error {
	return nil
}

func (e *emptyLogger) Loggable(p Priority) bool {
	return e.prio > LOG_NONE && p >= e.prio
}

func (e *emptyLogger) Fatal(s string, v ...interface{}) {}
func (e *emptyLogger) Crit(s string, v ...interface{})  {}
func (e *emptyLogger) Error(s string, v ...interface{}) {}
func (e *emptyLogger) Warn(s string, v ...interface{})  {}
func (e *emptyLogger) Info(s string, v ...interface{})  {}
func (e *emptyLogger) Debug(s string, v ...interface{}) {}

func (e *emptyLogger) Prio() Priority {
	return e.prio
}

func (e *emptyLogger) Prefix() string {
	return e.prefix
}
