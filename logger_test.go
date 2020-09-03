package logger

import (
	"bytes"
	"testing"
	"time"
)

func TestConcurrent(t *testing.T) {
	assert := newAsserter(t)
	var wr bytes.Buffer

	ll, err := New(&wr, LOG_INFO, "", 0)
	assert(err == nil, "can't make logger: %s", err)

	go func() {
		for i := 0; i < 50000; i++ {
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
	ll.Close()
}
