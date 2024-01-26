package concurrent

import (
	. "github.com/onsi/gomega"
	"sync/atomic"
	"testing"
)

func TestExecutionPool(t *testing.T) {
	RegisterTestingT(t)

	pCnt := atomic.Int32{}
	tCnt := atomic.Int32{}

	pool := NewPool(10)

	for i := 0; i < 100000; i++ {
		pool.Execute(func() {
			cCnt := pCnt.Add(1)
			Expect(cCnt <= 10).Should(BeTrue())
			pCnt.Add(-1)
			tCnt.Add(1)
		})
	}

	cnt := pool.AwaitAll()

	Expect(cnt).Should(Equal(100000))
	Expect(int(tCnt.Load())).Should(Equal(100000))
}
