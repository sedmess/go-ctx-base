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

Reactive-style streaming primitives:

```go
// Create unbuffered stream
func CreateChannel[T](func(sink func(T, context.Context) bool)) StreamingChan[T]

// Create buffered batch stream
func CreateChannelBuffered[T](int, func(func([]T, context.Context) bool)) StreamingChan[T]

// Transformations
func Map[P, Q](StreamingChan[P], func(P) Q) StreamingChan[Q]
func FlatMap[P, Q](StreamingChan[P], func(P) StreamingChan[Q]) StreamingChan[Q]
```

**Exceptional**:
- Context-aware channel sending
- Automatic error propagation
- Batch buffering support

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
   - Context cancellation propagation
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
