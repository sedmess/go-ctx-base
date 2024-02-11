package db

import (
	"context"
	"errors"
	"github.com/sedmess/go-ctx-base/utils/channels"
	"github.com/sedmess/go-ctx/logger"
	"gorm.io/gorm"
)

const DefaultFetchSize = 1000

type Paginator interface {
	Scope() func(db *gorm.DB) *gorm.DB
	OffsetResult(result *gorm.DB)
	HasNext() bool
	Limit() int
	Offset() int
}

type paginator struct {
	fetchSize int
	offset    int
	hasNext   bool
	scopeFn   func(db *gorm.DB) *gorm.DB
}

func NewPaginator(fetchSize int) Paginator {
	p := &paginator{fetchSize: fetchSize, offset: 0, hasNext: true}
	p.scopeFn = func(db *gorm.DB) *gorm.DB {
		return db.Limit(p.fetchSize).Offset(p.offset)
	}
	return p
}

func (p *paginator) Scope() func(db *gorm.DB) *gorm.DB {
	return p.scopeFn
}

func (p *paginator) OffsetResult(result *gorm.DB) {
	if result.RowsAffected == 0 {
		p.hasNext = false
	} else {
		p.offset += int(result.RowsAffected)
	}
}

func (p *paginator) HasNext() bool {
	return p.hasNext
}

func (p *paginator) Limit() int {
	return p.fetchSize
}

func (p *paginator) Offset() int {
	return p.offset
}

func SessionStream[T any](connection Connection, fetchSize int, selectFn func(session *gorm.DB) *gorm.DB) channels.StreamingChan[T] {
	return SessionContextStream[T](context.Background(), connection, fetchSize, selectFn)
}

func SessionContextStream[T any](ctx context.Context, connection Connection, fetchSize int, selectFn func(session *gorm.DB) *gorm.DB) channels.StreamingChan[T] {
	paginator := NewPaginator(fetchSize)
	return channels.CreateChannelBuffered[T](fetchSize, func(sink func(data []T, context context.Context) bool) error {
		return connection.SessionContext(ctx, func(session *Session) error {
			for paginator.HasNext() {
				var list []T
				result := selectFn(session.Scopes(paginator.Scope())).Find(&list)
				if result.Error != nil {
					return result.Error
				}
				paginator.OffsetResult(result)
				if !sink(list, session) {
					logger.Error("DB", "breaking TX by timeout")
					return errors.New("transaction timeout expired")
				}
			}
			return nil
		})
	})
}

func SessionReturning[T any](connection Connection, sessionFn func(session *Session) (T, error)) (T, error) {
	var value T
	err := connection.Session(func(session *Session) error {
		var err error
		value, err = sessionFn(session)
		return err
	})
	return value, err
}

func SessionContextReturning[T any](context context.Context, connection Connection, sessionFn func(session *Session) (T, error)) (T, error) {
	var value T
	err := connection.SessionContext(context, func(session *Session) error {
		var err error
		value, err = sessionFn(session)
		return err
	})
	return value, err
}

func TxReturning[T any](session *Session, txFn func(session *Session) (T, error)) (T, error) {
	var value T
	err := session.Tx(func(session *Session) error {
		var err error
		value, err = txFn(session)
		return err
	})
	return value, err
}

func SessionTxReturning[T any](connection Connection, txFn func(session *Session) (T, error)) (T, error) {
	return SessionReturning(connection, func(session *Session) (T, error) {
		return TxReturning(session, func(session *Session) (T, error) {
			return txFn(session)
		})
	})
}

func SessionContextTxReturning[T any](context context.Context, connection Connection, txFn func(session *Session) (T, error)) (T, error) {
	return SessionContextReturning(context, connection, func(session *Session) (T, error) {
		return TxReturning(session, func(session *Session) (T, error) {
			return txFn(session)
		})
	})
}

func IsErrNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound)
}
