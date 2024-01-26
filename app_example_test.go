package main

import (
	"github.com/sedmess/go-ctx/ctx"
	"github.com/sedmess/go-ctx/ctx/ctx_testing"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	os.Exit(ctx_testing.CreateTestingApplication(Packages...).
		WithParameter("DB_SQLITE_PATH", "file::memory:?cache=shared").
		WithParameter("HTTP_LISTEN", "127.0.0.1:57650").
		WithParameter("HTTP_AUTH_TOKENS", "token,token1,token2").
		Run(m.Run))
}

func Test_MessageController(t *testing.T) {
	messageController := ctx.GetTypedService[*messageController]()
	if messageController == nil {
		t.FailNow()
	}
}
