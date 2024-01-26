package concurrent

import (
	"sync"
	"sync/atomic"
)

type Semaphore chan bool

func NewSemaphore(limit int) Semaphore {
	return make(chan bool, limit)
}

func (s Semaphore) Acquire() {
	s <- true
}

func (s Semaphore) Release() {
	<-s
}

type ExecutionPool struct {
	s  Semaphore
	wg sync.WaitGroup
	cn uint32
}

func NewPool(limit int) *ExecutionPool {
	return &ExecutionPool{s: NewSemaphore(limit), wg: sync.WaitGroup{}, cn: uint32(0)}
}

func (p *ExecutionPool) Execute(fn func()) {
	p.s.Acquire()
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		defer atomic.AddUint32(&p.cn, 1)
		defer p.s.Release()
		fn()
	}()
}

func (p *ExecutionPool) AwaitAll() int {
	p.wg.Wait()
	return int(p.cn)
}
