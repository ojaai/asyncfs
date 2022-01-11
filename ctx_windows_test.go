// +build windows

package asyncfs

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func steps() []int {
	return []int{0x0}
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
	if err := c.initCtx(sz); err != nil {
		t.Fatal(err.Error())
	}
	c.sz = sz
	c.currentCnt = 0
	c.align = 512
	c.asyncMode = mode
	c.newThread = func(int) bool {
		return false
	}
}
