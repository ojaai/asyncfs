// +build freebsd darwin

package asyncfs

import (
	"github.com/stretchr/testify/assert"
	"testing"
	"unsafe"
)

func steps() []int {
	return []int{asyncAio}
}

func prepare(t *testing.T, mode int) {
	var sz int

	if mode == 0 { // windows/bsd
		sz = 8
		err := NewCtx(sz, sameThreadLim, nil, nil)
		assert.NoError(t, err)
		return
	}

	sz = 8
	c = new(ctx)
	c.asyncMode = asyncAio
	c.operationsFd = make(map[unsafe.Pointer]opCap, sz)
	c.sz = sz
	c.currentCnt = 0
	c.align = 512
	c.asyncMode = mode
	c.newThread = func(int) bool {
		return false
	}
}
