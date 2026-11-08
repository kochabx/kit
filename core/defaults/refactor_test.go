package defaults

import (
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCloneDistinctSliceViews(t *testing.T) {
	type item struct {
		Name string `default:"default"`
	}
	backing := make([]item, 3, 4)
	target := struct{ Short, Long, Limited []item }{backing[:1], backing[:3], backing[:1:1]}
	require.NoError(t, Apply(&target))
	require.Len(t, target.Short, 1)
	require.Len(t, target.Long, 3)
	require.Len(t, target.Limited, 1)
	require.Equal(t, 4, cap(target.Short))
	require.Equal(t, 1, cap(target.Limited))
	for _, value := range target.Long {
		require.Equal(t, "default", value.Name)
	}
	for _, value := range backing {
		require.Empty(t, value.Name)
	}
}

func TestCloneDepthLimitIsAtomic(t *testing.T) {
	type node struct {
		Next *node
		Name string `default:"node"`
	}
	root := &node{}
	tail := root
	for range 10000 {
		tail.Next = &node{}
		tail = tail.Next
	}
	target := struct{ Root *node }{root}
	err := Apply(&target, WithMaxDepth(8))
	require.ErrorIs(t, err, ErrMaxDepthExceeded)
	require.NotErrorIs(t, err, ErrApplyPanic)
	require.Same(t, root, target.Root)
	require.Empty(t, root.Name)
}

func TestCloneCyclesRemainBounded(t *testing.T) {
	type node struct {
		Next *node
		Name string `default:"node"`
	}
	root := &node{}
	root.Next = root
	target := struct{ Root *node }{root}
	require.NoError(t, Apply(&target, WithMaxDepth(4)))
	require.Same(t, target.Root, target.Root.Next)
	require.NotSame(t, root, target.Root)
	require.Equal(t, "node", target.Root.Name)
	require.Empty(t, root.Name)
}

func TestSkippedDefaultsDoNotAllocateAncestors(t *testing.T) {
	type child struct {
		Name string `default:"child"`
	}
	type parent struct {
		Child *child `default:"-"`
	}
	var target struct{ Parent *parent }
	require.NoError(t, Apply(&target))
	require.Nil(t, target.Parent)
	require.False(t, typeHasDefaults(reflect.TypeFor[parent](), "default", make(map[reflect.Type]bool)))
}

func TestSharedJSONDecoderOptions(t *testing.T) {
	applier, err := New(WithTimeLayouts("02/01/2006"))
	require.NoError(t, err)
	type config struct {
		Dates  []time.Time     `default:"[\"08/11/2026\"]"`
		Delays []time.Duration `default:"[\"1s\"]"`
	}
	errs := make(chan error, 16)
	var group sync.WaitGroup
	for range 16 {
		group.Go(func() {
			var target config
			if err := applier.Apply(&target); err != nil {
				errs <- err
				return
			}
			if len(target.Dates) != 1 || target.Dates[0].Year() != 2026 || len(target.Delays) != 1 || target.Delays[0] != time.Second {
				errs <- errors.New("incorrect decoded defaults")
			}
		})
	}
	group.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
}
