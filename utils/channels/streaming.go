package channels

import (
	"context"
	"errors"
	"runtime"
	"runtime/debug"
	"strconv"
)

func BrokenSinkError() error {
	_, file, line, ok := runtime.Caller(1)
	if ok {
		return errors.New("unexpected: broken sink at " + file + ":" + strconv.Itoa(line))
	} else {
		return errors.New("unexpected: broken sink at " + file + ":" + string(debug.Stack()))
	}
}

type StreamingChan[T any] <-chan ChanElem[T]

type ChanElem[T any] struct {
	data T
	err  error
}

func (e *ChanElem[T]) Data() T {
	return e.data
}

func (e *ChanElem[T]) Err() error {
	return e.err
}

func (e *ChanElem[T]) SendTo(ch chan<- ChanElem[T], context context.Context) bool {
	select {
	case ch <- *e:
		return true
	case <-context.Done():
		return false
	}
}

func WrapChanData[T any](data T) ChanElem[T] {
	return ChanElem[T]{data: data}
}

func WrapChanErr[T any](err error) ChanElem[T] {
	return ChanElem[T]{err: err}
}

func SingleElemChannel[T any](data T) StreamingChan[T] {
	result := make(chan ChanElem[T])
	go func() {
		result <- WrapChanData(data)
		close(result)
	}()
	return result
}

// SingleElemChannelContext returns an unbuffered, producer-owned stream whose
// only send is abandoned when the operation context is canceled.
func SingleElemChannelContext[T any](ctx context.Context, data T) StreamingChan[T] {
	requireOperationContext(ctx)
	result := make(chan ChanElem[T])
	go func() {
		defer close(result)
		_ = sendWithContext(result, WrapChanData(data), ctx, ctx)
	}()
	return result
}

func SingleElemChannelErr[T any](data T, err error) StreamingChan[T] {
	result := make(chan ChanElem[T])
	go func() {
		if err != nil {
			result <- WrapChanErr[T](err)
		} else {
			result <- WrapChanData(data)
		}
		close(result)
	}()
	return result
}

// SingleElemChannelErrContext returns either one value or one terminal error.
// Cancellation suppresses a send that has not completed yet.
func SingleElemChannelErrContext[T any](ctx context.Context, data T, err error) StreamingChan[T] {
	requireOperationContext(ctx)
	result := make(chan ChanElem[T])
	go func() {
		defer close(result)
		if err != nil {
			_ = sendWithContext(result, WrapChanErr[T](err), ctx, ctx)
			return
		}
		_ = sendWithContext(result, WrapChanData(data), ctx, ctx)
	}()
	return result
}

func SliceToChannel[T any](data []T) StreamingChan[T] {
	result := make(chan ChanElem[T])
	go func() {
		for _, datum := range data {
			result <- WrapChanData(datum)
		}
		close(result)
	}()
	return result
}

// SliceToChannelContext streams data in source order until completion or
// cancellation. The returned unbuffered channel is always closed by its producer.
func SliceToChannelContext[T any](ctx context.Context, data []T) StreamingChan[T] {
	requireOperationContext(ctx)
	result := make(chan ChanElem[T])
	go func() {
		defer close(result)
		for _, datum := range data {
			if !sendWithContext(result, WrapChanData(datum), ctx, ctx) {
				return
			}
		}
	}()
	return result
}

func CreateChannel[T any](generator func(sink func(data T, context context.Context) bool) error) StreamingChan[T] {
	result := make(chan ChanElem[T])
	go func() {
		defer close(result)
		err := generator(
			func(data T, context context.Context) bool {
				elem := WrapChanData(data)
				return elem.SendTo(result, context)
			},
		)
		if err != nil {
			result <- WrapChanErr[T](err)
		}
	}()
	return result
}

// CreateChannelContext creates an unbuffered context-owned stream. The
// generator must return after sink reports false. A non-nil generator error is
// sent at most once while the operation context remains active.
func CreateChannelContext[T any](ctx context.Context, generator func(sink func(data T, sendContext context.Context) bool) error) StreamingChan[T] {
	requireOperationContext(ctx)
	result := make(chan ChanElem[T])
	go func() {
		defer close(result)
		if ctx.Err() != nil {
			return
		}
		err := generator(func(data T, sendContext context.Context) bool {
			return sendWithContext(result, WrapChanData(data), ctx, sendContext)
		})
		if err != nil {
			_ = sendWithContext(result, WrapChanErr[T](err), ctx, ctx)
		}
	}()
	return result
}

func CreateChannelBuffered[T any](bufSize int, generator func(sink func(data []T, context context.Context) bool) error) StreamingChan[T] {
	result := make(chan ChanElem[T], bufSize)
	go func() {
		defer close(result)
		err := generator(
			func(data []T, context context.Context) bool {
				for _, datum := range data {
					elem := WrapChanData(datum)
					sent := elem.SendTo(result, context)
					if !sent {
						return false
					}
				}
				return true
			},
		)
		if err != nil {
			result <- WrapChanErr[T](err)
		}
	}()
	return result
}

// CreateChannelBufferedContext creates a context-owned stream with the exact
// requested finite capacity. Each page is delivered in order, one element at a
// time, and cancellation interrupts blocked value and terminal-error sends.
func CreateChannelBufferedContext[T any](ctx context.Context, bufSize int, generator func(sink func(data []T, sendContext context.Context) bool) error) StreamingChan[T] {
	requireOperationContext(ctx)
	result := make(chan ChanElem[T], bufSize)
	go func() {
		defer close(result)
		if ctx.Err() != nil {
			return
		}
		err := generator(func(data []T, sendContext context.Context) bool {
			for _, datum := range data {
				if !sendWithContext(result, WrapChanData(datum), ctx, sendContext) {
					return false
				}
			}
			return true
		})
		if err != nil {
			_ = sendWithContext(result, WrapChanErr[T](err), ctx, ctx)
		}
	}()
	return result
}

func requireOperationContext(ctx context.Context) {
	if ctx == nil {
		panic("channels: nil operation context")
	}
}

func sendWithContext[T any](ch chan<- ChanElem[T], elem ChanElem[T], operationContext context.Context, sendContext context.Context) bool {
	if sendContext == nil || operationContext.Err() != nil || sendContext.Err() != nil {
		return false
	}
	select {
	case <-operationContext.Done():
		return false
	case <-sendContext.Done():
		return false
	case ch <- elem:
		return true
	}
}

func Map[P any, Q any](ch StreamingChan[P], mapper func(data P) Q) StreamingChan[Q] {
	newCh := CreateChannel(func(sink func(data Q, context context.Context) bool) error {
		return ch.ForEachChanElem(func(data P) error {
			if sink(mapper(data), context.Background()) {
				return nil
			} else {
				return BrokenSinkError()
			}
		})
	})
	return newCh
}

// MapContext maps source values in order and shares ctx across receiving and
// sending so an abandoned chain can be canceled as one operation.
func MapContext[P any, Q any](ctx context.Context, ch StreamingChan[P], mapper func(data P) Q) StreamingChan[Q] {
	requireOperationContext(ctx)
	return CreateChannelContext(ctx, func(sink func(data Q, sendContext context.Context) bool) error {
		return ch.ForEachChanElemContext(ctx, func(data P) error {
			if sink(mapper(data), ctx) {
				return nil
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			return BrokenSinkError()
		})
	})
}

func FlapMap[P any, Q any](ch StreamingChan[P], mapper func(data P) StreamingChan[Q]) StreamingChan[Q] {
	newCh := CreateChannel(func(sink func(data Q, context context.Context) bool) error {
		return ch.ForEachChanElem(func(data P) error {
			c := mapper(data)
			return c.ForEachChanElem(func(data Q) error {
				if sink(data, context.Background()) {
					return nil
				} else {
					return BrokenSinkError()
				}
			})
		})
	})
	return newCh
}

// FlatMapContext expands source values in order while all owned receives and
// sends observe the same operation context. FlapMap remains available for
// compatibility with its historical spelling.
func FlatMapContext[P any, Q any](ctx context.Context, ch StreamingChan[P], mapper func(data P) StreamingChan[Q]) StreamingChan[Q] {
	requireOperationContext(ctx)
	return CreateChannelContext(ctx, func(sink func(data Q, sendContext context.Context) bool) error {
		return ch.ForEachChanElemContext(ctx, func(data P) error {
			return mapper(data).ForEachChanElemContext(ctx, func(mapped Q) error {
				if sink(mapped, ctx) {
					return nil
				}
				if err := ctx.Err(); err != nil {
					return err
				}
				return BrokenSinkError()
			})
		})
	})
}

func (ch StreamingChan[T]) Map(mapper func(data T) any) StreamingChan[any] {
	return Map(ch, mapper)
}

func (ch StreamingChan[T]) FlatMap(mapper func(data T) StreamingChan[any]) StreamingChan[any] {
	return FlapMap(ch, mapper)
}

func (ch StreamingChan[T]) ForEachChanElem(onEach func(data T) error) error {
	var err error
	for elem := range ch {
		if elem.Err() != nil {
			err = elem.Err()
			break
		}
		if e := onEach(elem.Data()); e != nil {
			err = e
			break
		}
	}
	return err
}

// ForEachChanElemContext consumes values until closure, a terminal stream
// error, callback failure, or operation cancellation.
func (ch StreamingChan[T]) ForEachChanElemContext(ctx context.Context, onEach func(data T) error) error {
	requireOperationContext(ctx)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case elem, open := <-ch:
			if !open {
				if err := ctx.Err(); err != nil {
					return err
				}
				return nil
			}
			if elem.Err() != nil {
				return elem.Err()
			}
			if err := onEach(elem.Data()); err != nil {
				return err
			}
		}
	}
}

func (ch StreamingChan[T]) CollectToSlice() ([]T, error) {
	result := make([]T, 0)
	err := ch.ForEachChanElem(func(data T) error {
		result = append(result, data)
		return nil
	})
	return result, err
}

// CollectToSliceContext collects values received before completion or
// cancellation. Partial values are returned together with the terminal error.
func (ch StreamingChan[T]) CollectToSliceContext(ctx context.Context) ([]T, error) {
	result := make([]T, 0)
	err := ch.ForEachChanElemContext(ctx, func(data T) error {
		result = append(result, data)
		return nil
	})
	return result, err
}
