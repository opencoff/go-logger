package logger

import (
	"bytes"
	"fmt"
	re "regexp"
	"testing"
	"time"
)

const (
	_Rprio      = `<(?<prio>[0-9]+)>:`
	_Rdate      = `(?<date>[0-9][0-9][0-9][0-9]/[0-9][0-9]/[0-9][0-9])`
	_Rtime      = `(?<time>[0-9][0-9]:[0-9][0-9]:[0-9][0-9].(?<frac>[0-9]+))`
	_Rline      = `(?<line>[0-9][0-9]*)`
	_Rlongfile  = `\((?<fpath>.*/[A-Za-z0-9_\-]+\.go):` + _Rline + `\)`
	_Rshortfile = `\((?<fname>[A-Za-z0-9_\-]+\.go):` + _Rline + `\)`
	_Rreltime   = `\+(?<reltime>[0-9]+.?[0-9]+)[^\s]+`
	_Rspace     = `\s+`
	_Rprefix    = `\[(?<pref>[\w-]+)\] `
	_Rlogmsg    = `(?<msg>.+)`
)

type expGroups uint

type testcase struct {
	flag   int    // logger flags
	prefix string // log prefix
	msg    string // message to log
	pat    string // regex pattern to match the output
}

var tests = []testcase{
	{0, "foo", "hello world", _Rprio + _Rdate + _Rspace + _Rtime + _Rspace + _Rprefix + _Rlogmsg},
	{Ldate, "foo", "date", _Rprio + _Rdate + _Rspace + _Rprefix + _Rlogmsg},
	{Ltime, "foo", "time", _Rprio + _Rtime + _Rspace + _Rprefix + _Rlogmsg},
	{Ltime | Lmicroseconds, "foo", "time+us", _Rprio + _Rtime + _Rspace + _Rprefix + _Rlogmsg},
	{Ldate | Ltime | Lfileloc, "foo", "file trace", _Rprio + _Rdate + _Rspace + _Rtime + _Rspace + _Rprefix + _Rlogmsg},
	{Lreltime, "foo", "reltime", _Rprio + _Rreltime + _Rspace + _Rprefix + _Rlogmsg},
}

func makeSubMap(rx *re.Regexp, s string) (map[string]string, error) {
	m := make(map[string]string)

	mx := rx.FindAllStringSubmatch(s, -1)
	if len(mx) != 1 {
		return nil, fmt.Errorf("rx-mat exp 1; saw %d", len(mx))
	}
	subs := mx[0]
	names := rx.SubexpNames()
	for i := 1; i < len(subs); i++ {
		k := names[i]
		m[k] = subs[i]
	}
	return m, nil
}

func doTest(t *testing.T, tc *testcase) {
	assert := newAsserter(t, tc.msg)
	rx, err := re.Compile(tc.pat)
	assert(err == nil, "re-compile: %s: %s", tc.pat, err)

	var wr bytes.Buffer
	ll, err := New(&wr, LOG_INFO, tc.prefix, tc.flag)
	assert(err == nil, "can't create log: %s", err)

	ll.Info(tc.msg)
	ll.Close()

	// skip the first line of logging; it's informational
	_, err = wr.ReadString('\n')
	assert(err == nil, "read hdr string: %s", err)

	out, err := wr.ReadString('\n')
	assert(err == nil, "read string: %s", err)

	m, err := makeSubMap(rx, out)
	assert(err == nil, "<%s>: %s", out, err)

	for k, v := range m {
		switch k {
		case "prio":
			want := fmt.Sprintf("%d", LOG_INFO)
			assert(want == m["prio"], "match: prio: exp %s, saw %s", want, m["prio"])

		case "date":
			if tc.flag == 0 || tc.flag&Ldate > 0 {
				assert(len(v) > 0, "match: date: exp value; saw nil")
			} else {
				assert(len(v) == 0, "match: date: saw %s, exp empty", v)
			}

		case "time":
			if tc.flag == 0 || tc.flag&Ltime > 0 {
				frac := m["frac"]
				assert(len(v) > 0, "match: time: exp value; saw nil")
				assert(len(frac) >= 3, "match: time: frac explen min 3, saw %d", len(frac))
				if tc.flag&Lmicroseconds > 0 {
					assert(len(frac) == 6, "match: time us frac: explen 6, saw %d", len(frac))
				}
			} else {
				assert(len(v) == 0, "match: time: saw %s, exp empty", v)
			}

		case "pref":
			assert(v == tc.prefix, "match: pref exp '%s', saw '%s'", tc.prefix, v)

		case "msg":
			assert(v == tc.msg, "match: msg exp '%s', saw '%s'", tc.msg, v)

		case "reltime":
			if tc.flag&Lreltime > 0 {
				assert(len(v) > 0, "match: reltime: exp non empty ts")
			} else {
				assert(len(v) == 0, "match: reltime: saw %s, exp empty", v)
			}

		case "fname":
			if tc.flag&Lfileloc > 0 {
				line := m["line"]
				assert(len(v) > 0, "match: fname: exp value; saw nil")
				assert(len(line) > 0, "match: fname; exp line; saw nil")
			} else {
				assert(len(v) == 0, "match: fname: saw %s, exp empty", v)
			}

		case "fpath":
			if tc.flag&Lfileloc > 0 {
				line := m["line"]
				assert(len(v) > 0, "match: fpath: exp value; saw nil")
				assert(len(line) > 0, "match: fpath; exp line; saw nil")
			} else {
				assert(len(v) == 0, "match: fpath: saw %s, exp empty", v)
			}

		}
	}
}

func TestLog(t *testing.T) {
	for i := range tests {
		tc := &tests[i]
		doTest(t, tc)
	}
}

func TestConcurrent(t *testing.T) {
	assert := newAsserter(t, "")
	var wr bytes.Buffer

	ll, err := New(&wr, LOG_INFO, "", 0)
	assert(err == nil, "can't make logger: %s", err)

	go func() {
		for i := 0; i < 5000; i++ {
			go func(i int, l *Logger) {
				for j := 0; j < 100; j++ {
					ll.Info("go-%d: Log message %d", i, j)
					time.Sleep(5 * time.Microsecond)
				}
			}(i, ll)
		}
	}()

	// abruptly close
	ll.Close()
}
