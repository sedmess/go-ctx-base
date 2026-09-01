# Utility Packages Documentation

This document details the utility components of the Go Contextualized Application Framework.

## Table of Contents
- [Slice Utilities](#slice-utilities)
- [Value Helpers](#value-helpers)
- [Streaming Channels](#streaming-channels)
- [Concurrent Execution](#concurrent-execution)
- [Implementation Details](#key-implementation-details)

## Slice Utilities (`slices/mappers.go`)

Provides functional-style operations for slices:

```go
// Transform slice elements
func Map[P, Q any]([]P, func(P) Q) []Q

// Flatten nested slices
func FlatMap[P, Q any]([]P, func(P) []Q) []Q

// Convert slice to map with unique keys
func ToMapUnique[V any, K comparable]([]V, func(V) K) map[K]V
```

**Exceptional**: `ToMapUnique` handles duplicate keys by overwriting with last occurrence

## Value Helpers (`values/utils.go`)

Utilities for working with values and optional types:

```go
// Create a heap-allocated copy
func Copy[T any](T) *T

// Optional type with default handling
type Optional[T] struct{...}
func (o Optional[T]) OrDefault(T) T
func IfError[T any](T, error) Optional[T]
```

**Exceptional**: `IfError` converts error returns to Optional type

## Streaming Channels (`channels/streaming.go`)

`StreamingChan[T]` remains a named receive-only channel of data-or-error elements. Legacy
constructors and transforms keep their original ordering, capacity, and backpressure behavior:

```go
func SingleElemChannel[T any](data T) StreamingChan[T]
func SingleElemChannelErr[T any](data T, err error) StreamingChan[T]
func SliceToChannel[T any](data []T) StreamingChan[T]

func CreateChannel[T any](
    generator func(sink func(T, context.Context) bool) error,
) StreamingChan[T]

func CreateChannelBuffered[T any](
    size int,
    generator func(sink func([]T, context.Context) bool) error,
) StreamingChan[T]

func Map[P, Q any](StreamingChan[P], func(P) Q) StreamingChan[Q]
func FlatMap[P, Q any](StreamingChan[P], func(P) StreamingChan[Q]) StreamingChan[Q]
func FlapMap[P, Q any](StreamingChan[P], func(P) StreamingChan[Q]) StreamingChan[Q]

func (StreamingChan[T]) Map[Q any](func(T) Q) StreamingChan[Q]
func (StreamingChan[T]) FlatMap[Q any](func(T) StreamingChan[Q]) StreamingChan[Q]
```

Go 1.27 generic methods retain the mapper's concrete result type without an `any` conversion.
Direct calls infer `Q`; method values or expressions without assignment context may specify it,
for example `stream.Map[string]`. Generic methods do not satisfy an interface containing the old
non-generic signature. The correctly spelled package `FlatMap` is canonical; historical `FlapMap`
remains as a deprecated compatibility alias. Legacy streams cannot detect that a receive-only
channel has been abandoned, so callers must drain them through closure.

Use the additive context-owned surface whenever a consumer may stop early:

```go
func SingleElemChannelContext[T any](context.Context, T) StreamingChan[T]
func SingleElemChannelErrContext[T any](context.Context, T, error) StreamingChan[T]
func SliceToChannelContext[T any](context.Context, []T) StreamingChan[T]
func CreateChannelContext[T any](
    context.Context,
    func(sink func(T, context.Context) bool) error,
) StreamingChan[T]
func CreateChannelBufferedContext[T any](
    context.Context,
    int,
    func(sink func([]T, context.Context) bool) error,
) StreamingChan[T]
func MapContext[P, Q any](context.Context, StreamingChan[P], func(P) Q) StreamingChan[Q]
func FlatMapContext[P, Q any](context.Context, StreamingChan[P], func(P) StreamingChan[Q]) StreamingChan[Q]
func (StreamingChan[T]) ForEachChanElemContext(context.Context, func(T) error) error
func (StreamingChan[T]) CollectToSliceContext(context.Context) ([]T, error)
```

Operation contexts must be non-nil. The producer owns and closes its output exactly once. Values
remain ordered, buffered constructors retain the requested finite capacity, and both the shared
operation context and each per-send context interrupt blocked sends. A generator error produces
at most one terminal error while the consumer can still receive it. A callback error is returned
as the consumer result; cancellation returns the context error and collection may also return the
values received before cancellation.

To abandon a context-owned chain, cancel its shared context before stopping receipt. Use that same
context for every owned constructor, transform, database stream, iteration, and collection stage.

## Concurrent Execution (`concurrent/executors.go`)

Pool-based concurrency control:

```go
// Semaphore with acquire/release
type Semaphore chan bool
func NewSemaphore(int) Semaphore

// Managed execution pool
type ExecutionPool struct{...}
func NewPool(int) *ExecutionPool
func (p *ExecutionPool) Execute(func())
func (p *ExecutionPool) AwaitAll() int
```

**Key Features**:
- Fixed worker pool size
- Wait group synchronization
- Atomic execution counting

## Key Implementation Details

1. **Channel Error Handling**:
   - Automatic error wrapping with `ChanElem`
   - Shared operation-context cancellation for additive stream APIs
   - Broken sink detection with stack traces

2. **Concurrency Safety**:
   - Semaphore-based worker limiting
   - Atomic counters for completed tasks
   - Tested with 100k task concurrency (see `executors_test.go`)

3. **Memory Management**:
   - Optional value type avoids nil pointers
   - Copy helper for explicit heap allocation
   - Batch channel buffers reduce GC pressure

See [main README](readme.md) for framework architecture and configuration details.
