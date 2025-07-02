package main

import (
	"context"
	"github.com/sedmess/go-ctx-base/utils/channels"
	"github.com/sedmess/go-ctx/ctx"
	"github.com/sedmess/go-ctx/ctx/ctx_testing"
	"github.com/sedmess/go-ctx/ctx/logger"
	"os"
	"testing"
)

type messageServiceStub struct {
	l logger.Logger `ctx:""`
}

func (s *messageServiceStub) SaveMessage(string, string, string) error {
	s.l.Info("SaveMessage stub")
	return nil
}

func (s *messageServiceStub) GetMessages(string, int64) channels.StreamingChan[Message] {
	s.l.Info("GetMessage stub")
	return channels.CreateChannel(func(sink func(data Message, context context.Context) bool) error {
		return nil
	})
}

func TestMain(m *testing.M) {
	os.Exit(ctx_testing.CreateTestingApplication(Packages...).
		WithParameter("DB_SQLITE_PATH", "file::memory:?cache=shared").
		WithParameter("HTTP_LISTEN", "127.0.0.1:57650").
		WithParameter("HTTP_AUTH_TOKENS", "token,token1,token2").
		WithTestingService(ctx_testing.Instead[MessageService](&messageServiceStub{})).
		Run(m.Run))
}

func Test_MessageController(t *testing.T) {
	messageController, ok := ctx.GetTypedService[*messageController]()
	if !ok || messageController == nil {
		t.FailNow()
	}
	messages := messageController.messageService.GetMessages("", 0)
	slice, err := messages.CollectToSlice()
	if err != nil {
		t.FailNow()
	}
	if len(slice) > 0 {
		t.FailNow()
	}
}
