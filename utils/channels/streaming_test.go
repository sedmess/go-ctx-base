package channels

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func waitForStreamClose[T any](t *testing.T, stream StreamingChan[T]) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case _, open := <-stream:
			if !open {
				return
			}
		case <-deadline:
			t.Fatal("stream did not close within two seconds")
		}
	}
}

func TestLegacyStreamingCompatibility(t *testing.T) {
	values, err := SliceToChannel([]int{1, 2, 3}).CollectToSlice()
	if err != nil || !reflect.DeepEqual(values, []int{1, 2, 3}) {
		t.Fatalf("slice stream = %v, %v", values, err)
	}
	if nilValues, err := SliceToChannel[int](nil).CollectToSlice(); err != nil || len(nilValues) != 0 {
		t.Fatalf("nil slice stream = %v, %v", nilValues, err)
	}
	if single, err := SingleElemChannel(7).CollectToSlice(); err != nil || !reflect.DeepEqual(single, []int{7}) {
		t.Fatalf("single stream = %v, %v", single, err)
	}

	sentinel := errors.New("terminal")
	errStream := SingleElemChannelErr(0, sentinel)
	if _, err := errStream.CollectToSlice(); !errors.Is(err, sentinel) {
		t.Fatalf("single terminal error = %v", err)
	}

	buffered := CreateChannelBuffered[int](3, func(sink func([]int, context.Context) bool) error {
		if !sink([]int{1, 2, 3}, context.Background()) {
			t.Fatal("legacy buffered sink broke")
		}
		return sentinel
	})
	if capacity := cap(buffered); capacity != 3 {
		t.Fatalf("legacy buffer capacity = %d", capacity)
	}
	terminalCount := 0
	values = nil
	for element := range buffered {
		if element.Err() != nil {
			terminalCount++
			if !errors.Is(element.Err(), sentinel) {
				t.Fatalf("terminal error = %v", element.Err())
			}
			continue
		}
		values = append(values, element.Data())
	}
	if !reflect.DeepEqual(values, []int{1, 2, 3}) || terminalCount != 1 {
		t.Fatalf("legacy buffered values/errors = %v/%d", values, terminalCount)
	}

	flattened, err := FlapMap(SliceToChannel([]int{1, 2}), func(value int) StreamingChan[int] {
		return SliceToChannel([]int{value, value * 10})
	}).CollectToSlice()
	if err != nil || !reflect.DeepEqual(flattened, []int{1, 10, 2, 20}) {
		t.Fatalf("historical FlapMap = %v, %v", flattened, err)
	}
}

func TestLegacyStreamsRetainBackpressure(t *testing.T) {
	sendCompleted := make(chan struct{})
	stream := CreateChannel(func(sink func(int, context.Context) bool) error {
		if !sink(1, context.Background()) {
			return BrokenSinkError()
		}
		close(sendCompleted)
		return nil
	})
	select {
	case <-sendCompleted:
		t.Fatal("unbuffered legacy send completed without a receiver")
	case <-time.After(20 * time.Millisecond):
	}
	if element := <-stream; element.Data() != 1 || element.Err() != nil {
		t.Fatalf("legacy element = %d, %v", element.Data(), element.Err())
	}
	select {
	case <-sendCompleted:
	case <-time.After(2 * time.Second):
		t.Fatal("legacy producer did not resume after receive")
	}
	waitForStreamClose(t, stream)
}

func TestContextStreamsCancelBeforeAndAfterSends(t *testing.T) {
	t.Run("before generator", func(t *testing.T) {
		operationContext, cancel := context.WithCancel(context.Background())
		cancel()
		var called atomic.Bool
		stream := CreateChannelContext(operationContext, func(func(int, context.Context) bool) error {
			called.Store(true)
			return nil
		})
		waitForStreamClose(t, stream)
		if called.Load() {
			t.Fatal("canceled operation invoked its generator")
		}
	})

	t.Run("after partial receive", func(t *testing.T) {
		operationContext, cancel := context.WithCancel(context.Background())
		stream := SliceToChannelContext(operationContext, []int{1, 2, 3})
		if element := <-stream; element.Data() != 1 {
			t.Fatalf("first element = %d", element.Data())
		}
		cancel()
		waitForStreamClose(t, stream)
	})

	t.Run("blocked value send", func(t *testing.T) {
		operationContext, cancel := context.WithCancel(context.Background())
		generatorStarted := make(chan struct{})
		generatorDone := make(chan struct{})
		stream := CreateChannelContext(operationContext, func(sink func(int, context.Context) bool) error {
			defer close(generatorDone)
			close(generatorStarted)
			if sink(1, operationContext) {
				t.Error("blocked send succeeded without a receiver")
			}
			return nil
		})
		<-generatorStarted
		cancel()
		select {
		case <-generatorDone:
		case <-time.After(2 * time.Second):
			t.Fatal("blocked producer ignored cancellation")
		}
		waitForStreamClose(t, stream)
	})

	t.Run("per-send cancellation", func(t *testing.T) {
		operationContext, cancelOperation := context.WithCancel(context.Background())
		defer cancelOperation()
		sendContext, cancelSend := context.WithCancel(context.Background())
		cancelSend()
		stream := CreateChannelContext(operationContext, func(sink func(int, context.Context) bool) error {
			if sink(1, sendContext) {
				t.Error("send with canceled per-send context succeeded")
			}
			return nil
		})
		waitForStreamClose(t, stream)
	})
}

func TestContextTransformsCancelBlockedReceiveAndPreserveOrder(t *testing.T) {
	operationContext, cancel := context.WithCancel(context.Background())
	never := make(chan ChanElem[int])
	mapped := MapContext(operationContext, StreamingChan[int](never), func(value int) int { return value * 2 })
	cancel()
	waitForStreamClose(t, mapped)

	operationContext, cancel = context.WithCancel(context.Background())
	defer cancel()
	source := SliceToChannelContext(operationContext, []int{1, 2, 3})
	flattened := FlatMapContext(operationContext, source, func(value int) StreamingChan[int] {
		return SliceToChannelContext(operationContext, []int{value, value * 10})
	})
	values, err := flattened.CollectToSliceContext(operationContext)
	if err != nil || !reflect.DeepEqual(values, []int{1, 10, 2, 20, 3, 30}) {
		t.Fatalf("context transform = %v, %v", values, err)
	}
}

func TestContextTerminalErrorAndConsumerResults(t *testing.T) {
	operationContext, cancel := context.WithCancel(context.Background())
	defer cancel()
	sentinel := errors.New("generator failed")
	stream := CreateChannelContext(operationContext, func(sink func(int, context.Context) bool) error {
		if !sink(1, operationContext) {
			return operationContext.Err()
		}
		return sentinel
	})
	values, err := stream.CollectToSliceContext(operationContext)
	if !reflect.DeepEqual(values, []int{1}) || !errors.Is(err, sentinel) {
		t.Fatalf("terminal result = %v, %v", values, err)
	}
	waitForStreamClose(t, stream)

	callbackError := errors.New("callback failed")
	err = SliceToChannelContext(operationContext, []int{1, 2}).ForEachChanElemContext(operationContext, func(int) error {
		return callbackError
	})
	if !errors.Is(err, callbackError) {
		t.Fatalf("callback result = %v", err)
	}
}

func TestContextStreamCancellationImmediatelyAfterGeneratorError(t *testing.T) {
	const iterations = 100
	sentinel := errors.New("generator failed before cancellation")
	for iteration := 0; iteration < iterations; iteration++ {
		operationContext, cancel := context.WithCancel(context.Background())
		generatorReturning := make(chan struct{})
		stream := CreateChannelContext(operationContext, func(sink func(int, context.Context) bool) error {
			close(generatorReturning)
			return sentinel
		})

		<-generatorReturning
		cancel()

		values := 0
		terminalErrors := 0
		deadline := time.After(2 * time.Second)
		for stream != nil {
			select {
			case element, open := <-stream:
				if !open {
					stream = nil
					continue
				}
				if element.Err() != nil {
					terminalErrors++
				} else {
					values++
				}
			case <-deadline:
				t.Fatalf("iteration %d did not close after canceling the terminal-error send", iteration)
			}
		}
		if values != 0 || terminalErrors > 1 {
			t.Fatalf("iteration %d emitted values/errors = %d/%d", iteration, values, terminalErrors)
		}
	}
}

func TestContextStreamCancellationStress(t *testing.T) {
	const iterations = 100
	var waitGroup sync.WaitGroup
	for index := 0; index < iterations; index++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			operationContext, cancel := context.WithCancel(context.Background())
			source := SliceToChannelContext(operationContext, []int{1, 2, 3, 4})
			mapped := MapContext(operationContext, source, func(value int) int { return value * 2 })
			<-mapped
			cancel()
			for range mapped {
			}
		}()
	}
	finished := make(chan struct{})
	go func() {
		waitGroup.Wait()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("context stream stress did not terminate")
	}
}

func TestContextStreamsRejectNilOperationContext(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("nil operation context did not panic")
		}
	}()
	_ = SingleElemChannelContext[int](nil, 1)
}

func TestContextBufferedCapacity(t *testing.T) {
	operationContext, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := CreateChannelBufferedContext[int](operationContext, 4, func(sink func([]int, context.Context) bool) error {
		sink([]int{1, 2, 3}, operationContext)
		return nil
	})
	if capacity := cap(stream); capacity != 4 {
		t.Fatalf("context buffer capacity = %d", capacity)
	}
	values, err := stream.CollectToSliceContext(operationContext)
	if err != nil || !reflect.DeepEqual(values, []int{1, 2, 3}) {
		t.Fatalf("context buffered values = %v, %v", values, err)
	}
}
