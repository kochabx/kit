package defaults

import (
	"net"
	"net/netip"
	"net/url"
	"reflect"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyScalarsAndPreservesNonZeroValues(t *testing.T) {
	type config struct {
		Name    string        `default:"service"`
		Port    uint16        `default:"8080"`
		Ratio   float32       `default:"1.5"`
		Enabled bool          `default:"true"`
		Timeout time.Duration `default:"3s"`
		Complex complex64     `default:"1+2i"`
	}
	target := config{Name: "custom"}

	require.NoError(t, Apply(&target))
	assert.Equal(t, "custom", target.Name)
	assert.Equal(t, uint16(8080), target.Port)
	assert.Equal(t, float32(1.5), target.Ratio)
	assert.True(t, target.Enabled)
	assert.Equal(t, 3*time.Second, target.Timeout)
	assert.Equal(t, complex64(1+2i), target.Complex)
}

func TestApplyCommonTypes(t *testing.T) {
	type config struct {
		Endpoint url.URL        `default:"https://example.com/api?q=1"`
		IP       net.IP         `default:"192.0.2.1"`
		Network  net.IPNet      `default:"192.0.2.0/24"`
		Addr     netip.Addr     `default:"2001:db8::1"`
		Prefix   netip.Prefix   `default:"2001:db8::/32"`
		When     time.Time      `default:"2026-08-24T10:30:00Z"`
		Zone     *time.Location `default:"Asia/Shanghai"`
		Pattern  regexp.Regexp  `default:"^[a-z]+$"`
		Raw      []byte         `default:"hello"`
		Secret   []byte         `default:"base64:aGVsbG8="`
	}
	var target config

	require.NoError(t, Apply(&target))
	assert.Equal(t, "https", target.Endpoint.Scheme)
	assert.True(t, target.IP.Equal(net.ParseIP("192.0.2.1")))
	assert.Equal(t, "192.0.2.0/24", target.Network.String())
	assert.Equal(t, netip.MustParseAddr("2001:db8::1"), target.Addr)
	assert.Equal(t, netip.MustParsePrefix("2001:db8::/32"), target.Prefix)
	assert.Equal(t, 2026, target.When.Year())
	assert.Equal(t, "Asia/Shanghai", target.Zone.String())
	assert.True(t, target.Pattern.MatchString("abc"))
	assert.Equal(t, []byte("hello"), target.Raw)
	assert.Equal(t, []byte("hello"), target.Secret)
}

func TestApplyJSONCompositeTypes(t *testing.T) {
	type nested struct {
		Host string `json:"host"`
		Port int    `json:"port"`
	}
	type config struct {
		Names  []string       `default:"[\"a,b\",\"c\"]"`
		Ports  [2]int         `default:"[80,443]"`
		Labels map[string]int `default:"{\"primary\":1}"`
		Server nested         `default:"{\"host\":\"localhost\",\"port\":8080}"`
	}
	var target config

	require.NoError(t, Apply(&target))
	assert.Equal(t, []string{"a,b", "c"}, target.Names)
	assert.Equal(t, [2]int{80, 443}, target.Ports)
	assert.Equal(t, map[string]int{"primary": 1}, target.Labels)
	assert.Equal(t, nested{Host: "localhost", Port: 8080}, target.Server)
}

func TestApplyNestedPointersCollectionsAndCycles(t *testing.T) {
	type node struct {
		Name string `default:"node"`
		Next *node
	}
	type config struct {
		Root  *node
		Nodes []node
		ByID  map[string]node
	}
	target := config{
		Nodes: []node{{}},
		ByID:  map[string]node{"one": {}},
	}
	target.Root = &node{}
	target.Root.Next = target.Root

	require.NoError(t, Apply(&target))
	assert.Equal(t, "node", target.Root.Name)
	assert.Same(t, target.Root, target.Root.Next)
	assert.Equal(t, "node", target.Nodes[0].Name)
	assert.Equal(t, "node", target.ByID["one"].Name)
}

func TestApplyIsAtomicOnError(t *testing.T) {
	type config struct {
		Host string `default:"localhost"`
		Port int8   `default:"300"`
	}
	target := config{}

	err := Apply(&target)
	assert.ErrorIs(t, err, ErrInvalidTagValue)
	assert.Equal(t, config{}, target)
	var fieldErr *FieldError
	require.ErrorAs(t, err, &fieldErr)
	assert.Equal(t, "Port", fieldErr.Path)
	assert.NotContains(t, err.Error(), "300")
}

func TestApplyDistinguishesEmptyAndMissingTags(t *testing.T) {
	type config struct {
		Present *string  `default:""`
		Skipped string   `default:"-"`
		Items   []string `default:"[]"`
	}
	var target config

	require.NoError(t, Apply(&target))
	require.NotNil(t, target.Present)
	assert.Empty(t, *target.Present)
	assert.Empty(t, target.Skipped)
	assert.NotNil(t, target.Items)
	assert.Empty(t, target.Items)
}

func TestApplyRejectsMalformedJSONAndUnknownFields(t *testing.T) {
	type nested struct {
		Name string `json:"name"`
	}
	type config struct {
		BadMap map[string]int `default:"{broken}"`
	}
	var target config
	assert.ErrorIs(t, Apply(&target), ErrInvalidTagValue)

	type unknownConfig struct {
		Value nested `default:"{\"unknown\":1}"`
	}
	assert.ErrorIs(t, Apply(&unknownConfig{}), ErrInvalidTagValue)
}

func TestApplierOptionsAndCustomDecoder(t *testing.T) {
	type config struct {
		Value string `fallback:"value"`
		Skip  string `fallback:"skip" ignore:"true"`
	}
	applier, err := New(
		WithTag("fallback"),
		WithFieldFilter(func(field reflect.StructField) bool {
			return field.Tag.Get("ignore") != "true"
		}),
		WithDecoder(DecoderFunc(func(value reflect.Value, raw string) error {
			value.SetString("decoded:" + raw)
			return nil
		})),
	)
	require.NoError(t, err)
	var target config
	require.NoError(t, applier.Apply(&target))
	assert.Equal(t, "decoded:value", target.Value)
	assert.Empty(t, target.Skip)
}

func TestInvalidOptionsAndTargets(t *testing.T) {
	_, err := New(WithTag(""))
	assert.ErrorIs(t, err, ErrInvalidOption)
	_, err = New(WithMaxDepth(0))
	assert.ErrorIs(t, err, ErrInvalidOption)
	_, err = New(nil)
	assert.ErrorIs(t, err, ErrInvalidOption)

	assert.ErrorIs(t, Apply(struct{}{}), ErrTargetMustBePointer)
	assert.ErrorIs(t, Apply((*struct{})(nil)), ErrTargetIsNil)
	assert.ErrorIs(t, Apply(new(int)), ErrTargetMustBePointer)
}

func TestMetadataCacheIsConcurrentForRecursiveTypes(t *testing.T) {
	applier, err := New()
	require.NoError(t, err)
	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var target recursiveLeft
			require.NoError(t, applier.Apply(&target))
			require.NotNil(t, target.Right)
			assert.Equal(t, "ok", target.Right.Value)
		}()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("concurrent metadata lookup deadlocked")
	}
}

type recursiveLeft struct {
	Right *recursiveRight
}

type recursiveRight struct {
	Left  *recursiveLeft
	Value string `default:"ok"`
}

func TestApplyRecursionDepth(t *testing.T) {
	type level3 struct {
		Value string `default:"deep"`
	}
	type level2 struct{ Child level3 }
	type level1 struct{ Child level2 }
	type config struct{ Child level1 }

	var target config
	err := Apply(&target, WithMaxDepth(2))
	assert.ErrorIs(t, err, ErrMaxDepthExceeded)
	assert.Equal(t, config{}, target)
}

func TestApplyPanicIsRecoveredAtomically(t *testing.T) {
	type config struct {
		Value string `default:"value"`
	}
	target := config{}
	err := Apply(&target, WithDecoder(DecoderFunc(func(reflect.Value, string) error {
		panic("boom")
	})))
	assert.ErrorIs(t, err, ErrApplyPanic)
	assert.Equal(t, config{}, target)
}

func BenchmarkApply(b *testing.B) {
	type config struct {
		Host string        `default:"localhost"`
		Port uint16        `default:"8080"`
		TTL  time.Duration `default:"30s"`
	}
	applier, err := New()
	require.NoError(b, err)
	for b.Loop() {
		var target config
		_ = applier.Apply(&target)
	}
}
