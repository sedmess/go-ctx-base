package db

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sedmess/go-ctx-base/utils/channels"
	"gorm.io/gorm"
)

type streamRecord struct {
	ID    int `gorm:"primaryKey"`
	Value string
}

type streamConnectionStub struct {
	Connection
	sessionContext func(context.Context, func(*Session) error) error
}

func (s *streamConnectionStub) SessionContext(ctx context.Context, callback func(*Session) error) error {
	return s.sessionContext(ctx, callback)
}

func waitForDatabaseStreamClose[T any](t *testing.T, stream channels.StreamingChan[T]) []channels.ChanElem[T] {
	t.Helper()
	deadline := time.After(2 * time.Second)
	var elements []channels.ChanElem[T]
	for {
		select {
		case element, open := <-stream:
			if !open {
				return elements
			}
			elements = append(elements, element)
		case <-deadline:
			t.Fatal("database stream did not close within two seconds")
		}
	}
}

func TestPaginatorCompatibility(t *testing.T) {
	paginator := NewPaginator(25)
	if paginator.Limit() != 25 || paginator.Offset() != 0 || !paginator.HasNext() {
		t.Fatalf("initial paginator = limit %d, offset %d, next %v", paginator.Limit(), paginator.Offset(), paginator.HasNext())
	}
	paginator.OffsetResult(&gorm.DB{RowsAffected: 7})
	if paginator.Offset() != 7 || !paginator.HasNext() {
		t.Fatalf("advanced paginator = offset %d, next %v", paginator.Offset(), paginator.HasNext())
	}
	paginator.OffsetResult(&gorm.DB{RowsAffected: 0})
	if paginator.HasNext() {
		t.Fatal("zero-row page did not terminate pagination")
	}
}

func TestSessionStreamsPreservePaginationAndCompatibility(t *testing.T) {
	connection := newTestConnection(t, "session-stream-pagination")
	if err := connection.Init(); err != nil {
		t.Fatal(err)
	}
	defer connection.Dispose()
	if err := connection.Session(func(session *Session) error {
		if err := session.AutoMigrate(&streamRecord{}); err != nil {
			return err
		}
		return session.Create([]streamRecord{
			{ID: 1, Value: "one"},
			{ID: 2, Value: "two"},
			{ID: 3, Value: "three"},
			{ID: 4, Value: "four"},
			{ID: 5, Value: "five"},
		}).Error
	}); err != nil {
		t.Fatal(err)
	}

	operationContext, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := SessionContextStream[streamRecord](operationContext, connection, 2, func(session *gorm.DB) *gorm.DB {
		return session.Order("id")
	})
	if capacity := cap(stream); capacity != 2 {
		t.Fatalf("database stream capacity = %d", capacity)
	}
	rows, err := stream.CollectToSliceContext(operationContext)
	if err != nil {
		t.Fatal(err)
	}
	if got := []int{rows[0].ID, rows[1].ID, rows[2].ID, rows[3].ID, rows[4].ID}; !reflect.DeepEqual(got, []int{1, 2, 3, 4, 5}) {
		t.Fatalf("context pagination order = %v", got)
	}

	legacyRows, err := SessionStream[streamRecord](connection, 3, func(session *gorm.DB) *gorm.DB {
		return session.Order("id")
	}).CollectToSlice()
	if err != nil || len(legacyRows) != 5 {
		t.Fatalf("legacy SessionStream = %d rows, %v", len(legacyRows), err)
	}
}

func TestSessionContextStreamCancelsPoolWork(t *testing.T) {
	started := make(chan struct{})
	connection := &streamConnectionStub{sessionContext: func(ctx context.Context, _ func(*Session) error) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}}
	operationContext, cancel := context.WithCancel(context.Background())
	stream := SessionContextStream[int](operationContext, connection, 1, func(session *gorm.DB) *gorm.DB { return session })
	<-started
	cancel()
	if elements := waitForDatabaseStreamClose(t, stream); len(elements) != 0 {
		t.Fatalf("canceled pool work emitted %d elements", len(elements))
	}
}

func TestSessionContextStreamCancelsBlockedPageSend(t *testing.T) {
	connection := newTestConnection(t, "session-stream-page-cancel")
	if err := connection.Init(); err != nil {
		t.Fatal(err)
	}
	defer connection.Dispose()
	if err := connection.Session(func(session *Session) error {
		if err := session.AutoMigrate(&streamRecord{}); err != nil {
			return err
		}
		return session.Create([]streamRecord{{ID: 1}, {ID: 2}, {ID: 3}}).Error
	}); err != nil {
		t.Fatal(err)
	}

	operationContext, cancel := context.WithCancel(context.Background())
	var queries atomic.Int32
	stream := SessionContextStream[streamRecord](operationContext, connection, 1, func(session *gorm.DB) *gorm.DB {
		queries.Add(1)
		return session.Order("id")
	})
	deadline := time.Now().Add(2 * time.Second)
	for queries.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if queries.Load() < 2 {
		t.Fatal("producer did not reach the blocked second page send")
	}
	cancel()
	elements := waitForDatabaseStreamClose(t, stream)
	if len(elements) != 1 || elements[0].Err() != nil || elements[0].Data().ID != 1 {
		t.Fatalf("elements before canceled page send = %#v", elements)
	}
}

func TestSessionContextStreamCancelsBlockedTerminalError(t *testing.T) {
	sentinel := errors.New("synthetic query failure")
	returned := make(chan struct{})
	connection := &streamConnectionStub{sessionContext: func(context.Context, func(*Session) error) error {
		close(returned)
		return sentinel
	}}
	operationContext, cancel := context.WithCancel(context.Background())
	stream := SessionContextStream[int](operationContext, connection, 0, func(session *gorm.DB) *gorm.DB { return session })
	<-returned
	cancel()
	if elements := waitForDatabaseStreamClose(t, stream); len(elements) != 0 {
		t.Fatalf("canceled terminal error emitted %d elements", len(elements))
	}
}
