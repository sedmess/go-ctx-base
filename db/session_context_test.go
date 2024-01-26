package db

import (
	"context"
	"errors"
	gm "github.com/onsi/gomega"
	"os"
	"testing"
	"time"
)

func Test_SessionContext(t *testing.T) {
	gm.RegisterTestingT(t)

	_ = os.Setenv("BASE_TEST_DB_SQLITE_PATH", "file:test:?mode=memory&cache=shared")

	conn := NewConnection("base_test", "base_test", false, false)
	conn.Init()

	resultCh := make(chan error)

	go func() {
		timeout, cancelFunc := context.WithTimeout(context.Background(), time.Millisecond)
		defer cancelFunc()

		resultCh <- conn.SessionContext(timeout, func(session *Session) error {
			return session.Exec("WITH RECURSIVE r(i) AS (\n  VALUES(0)\n  UNION ALL\n  SELECT i FROM r\n  LIMIT 1000000000\n)\nSELECT i FROM r WHERE i = 1;").Error
		})
	}()

	var result error
	select {
	case err := <-resultCh:
		result = err
	case <-time.After(time.Second):
		result = errors.New("test timeout")
	}

	gm.Expect(result).ShouldNot(gm.BeNil())
	gm.Expect(result.Error()).Should(gm.Equal("interrupted (9)"))
}
