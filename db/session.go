package db

import (
	"context"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Session struct {
	context.Context
	*gorm.DB
	inTx bool
}

func newSession(parentContext context.Context, db *gorm.DB) *Session {
	return &Session{Context: parentContext, DB: db.WithContext(parentContext), inTx: false}
}

func (s *Session) Tx(txFunc func(session *Session) error) error {
	var err error
	if s.inTx {
		err = txFunc(&Session{Context: s.Context, DB: s.DB, inTx: true})
	} else {
		err = s.DB.Transaction(func(tx *gorm.DB) error {
			return txFunc(&Session{Context: s.Context, DB: tx, inTx: true})
		})
	}
	return err
}

func (s *Session) LockForUpdate() *gorm.DB {
	return s.Clauses(clause.Locking{Strength: "UPDATE"})
}
