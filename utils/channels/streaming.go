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

func (ch StreamingChan[T]) CollectToSlice() ([]T, error) {
	result := make([]T, 0)
	err := ch.ForEachChanElem(func(data T) error {
		result = append(result, data)
		return nil
	})
	return result, err
}
