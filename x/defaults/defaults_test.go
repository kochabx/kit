package defaults

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/netip"
	"net/url"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

type customText string

func (c *customText) UnmarshalText(b []byte) error {
	*c = customText("text:" + string(b))
	return nil
}

type setterNested struct {
	Host string `default:"localhost"`
	Port int    `default:"8080"`
	Addr string
}

func (s *setterNested) SetDefaults() {
	if s.Addr == "" {
		s.Addr = s.Host + ":8080"
	}
}

func TestAllElevenStdlibTypesBytesAndCollections(t *testing.T) {
	type cfg struct {
		Timeout  time.Duration    `default:"5s"`
		At       time.Time        `default:"2026-11-24T10:30:00Z"`
		Zone     *time.Location   `default:"Asia/Shanghai"`
		URL      url.URL          `default:"https://example.com/api?k=v"`
		Query    url.Values       `default:"a=1&a=2&b=x"`
		IP       net.IP           `default:"192.0.2.10"`
		CIDR     net.IPNet        `default:"192.0.2.0/24"`
		MAC      net.HardwareAddr `default:"00:11:22:33:44:55"`
		TCP      net.TCPAddr      `default:"127.0.0.1:8080"`
		UDP      net.UDPAddr      `default:"127.0.0.1:9090"`
		Pattern  *regexp.Regexp   `default:"^[a-z]+$"`
		Raw      []byte           `default:"raw-bytes"`
		B64      []byte           `default:"base64:aGVsbG8="`
		Hex      []byte           `default:"hex:6b6974"`
		Addr     netip.Addr       `default:"2001:db8::1"`
		Custom   customText       `default:"ok"`
		Interval []time.Duration  `default:"[\"1s\",\"2m\"]"`
		Zones    []*time.Location `default:"[\"UTC\",\"Asia/Shanghai\"]"`
	}

	got, err := New[cfg]()
	if err != nil {
		t.Fatalf("New[cfg]() failed: %v", err)
	}
	if got.Timeout != 5*time.Second || got.At.Year() != 2026 || got.Zone.String() != "Asia/Shanghai" {
		t.Fatalf("time defaults mismatch: %+v", got)
	}
	if got.URL.Host != "example.com" || len(got.Query["a"]) != 2 || got.Query["a"][1] != "2" {
		t.Fatalf("url defaults mismatch: %+v %+v", got.URL, got.Query)
	}
	if !got.IP.Equal(net.ParseIP("192.0.2.10")) || got.CIDR.String() != "192.0.2.0/24" || got.MAC.String() != "00:11:22:33:44:55" {
		t.Fatalf("net defaults mismatch: %+v", got)
	}
	if got.TCP.Port != 8080 || got.UDP.Port != 9090 || !got.Pattern.MatchString("abc") {
		t.Fatalf("addr/regexp defaults mismatch: %+v", got)
	}
	if string(got.Raw) != "raw-bytes" || string(got.B64) != "hello" || string(got.Hex) != "kit" {
		t.Fatalf("bytes defaults mismatch: %q %q %q", got.Raw, got.B64, got.Hex)
	}
	if got.Addr != netip.MustParseAddr("2001:db8::1") || got.Custom != "text:ok" {
		t.Fatalf("netip/customText defaults mismatch: %v %v", got.Addr, got.Custom)
	}
	if !slices.Equal(got.Interval, []time.Duration{time.Second, 2 * time.Minute}) || len(got.Zones) != 2 || got.Zones[1].String() != "Asia/Shanghai" {
		t.Fatalf("JSON-embedded stdlib slices mismatch: %+v %+v", got.Interval, got.Zones)
	}
}

func TestNestedJSONNilSliceFilterAndPointerSemantics(t *testing.T) {
	type item struct {
		Name    string `json:"name" default:"unnamed"`
		Retries int    `json:"retries" default:"3"`
		Secret  string `default:"ignored"`
	}
	type holder struct {
		Nested   item   `default:"{\"name\":\"worker\"}"`
		Items    []item `default:"[{\"name\":\"job\"}]"`
		Optional *[]item
		Skipped  *item
		Static   string `default:"keep"`
		Hidden   string `default:"secret-value"`
	}

	h := holder{}
	err := Apply(&h, WithFieldFilter(func(f reflect.StructField) bool {
		return f.Name != "Hidden" && f.Name != "Secret" && f.Name != "Skipped"
	}))
	if err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
	// Nested JSON struct and slice subfields receive defaults.
	if h.Nested.Name != "worker" || h.Nested.Retries != 3 {
		t.Fatalf("nested JSON defaults missing: %+v", h.Nested)
	}
	if len(h.Items) != 1 || h.Items[0].Name != "job" || h.Items[0].Retries != 3 {
		t.Fatalf("slice JSON defaults missing: %+v", h.Items)
	}
	// Optional *[]item and filtered *item remain nil.
	if h.Optional != nil {
		t.Fatalf("nil *[]item must not be allocated: %v", h.Optional)
	}
	if h.Skipped != nil || h.Hidden != "" || h.Nested.Secret != "" {
		t.Fatalf("FieldFilter must prevent allocation/mutation: %+v", h)
	}

	// Value time.Location and regexp.Regexp are rejected.
	type badLoc struct {
		Loc time.Location `default:"UTC"`
	}
	if err := Apply(&badLoc{}); !errors.Is(err, ErrUnsupportedType) {
		t.Fatalf("value time.Location must fail with ErrUnsupportedType, got %v", err)
	}
	type badRegexp struct {
		Re regexp.Regexp `default:"^a+$"`
	}
	if err := Apply(&badRegexp{}); !errors.Is(err, ErrUnsupportedType) {
		t.Fatalf("value regexp.Regexp must fail with ErrUnsupportedType, got %v", err)
	}
}

func TestUndoRollbackPreservesSyncPrimitivesAndTLS(t *testing.T) {
	type sub struct {
		Port int8 `default:"300"` // overflows int8
	}
	type server struct {
		mu     sync.Mutex
		TLS    *tls.Config
		Pool   *x509.CertPool
		Host   string `default:"localhost"`
		Nested *sub
		Sub    setterNested
		TagMap map[string]sub
	}

	tlsCfg := &tls.Config{ServerName: "secure.internal"}
	pool := x509.NewCertPool()
	s := server{
		TLS:    tlsCfg,
		Pool:   pool,
		TagMap: map[string]sub{"a": {}},
	}

	err := Apply(&s)
	if _, ok := errors.AsType[*FieldError](err); !ok || !errors.Is(err, ErrInvalidTagValue) {
		t.Fatalf("expected *FieldError/ErrInvalidTagValue, got %v", err)
	}
	// Error message must never leak the raw tag value "300".
	if strings.Contains(err.Error(), "300") {
		t.Fatalf("FieldError leaked raw tag value: %s", err.Error())
	}
	// Atomic Undo Log must restore every field and never clone/replace TLS, Pool, or Mutex.
	if s.Host != "" || s.Nested != nil || s.TLS != tlsCfg || s.Pool != pool || s.Sub.Port != 0 {
		t.Fatalf("state was not atomically rolled back: host=%q nested=%v subPort=%d", s.Host, s.Nested, s.Sub.Port)
	}
	// Mutex remains usable untouched.
	s.mu.Lock()
	s.mu.Unlock()
}

func TestOptionsCycleAndSetter(t *testing.T) {
	type node struct {
		Name string `default:"node"`
		Next *node
	}
	root := &node{}
	root.Next = root
	if err := Apply(root); err != nil || root.Name != "node" || root.Next != root {
		t.Fatalf("cycle traversal failed: %+v (%v)", root, err)
	}

	type dateCfg struct {
		Date time.Time `default:"2026/11/24"`
	}
	dCfg, err := New[dateCfg](WithTimeLayouts("2006/01/02"))
	if err != nil || dCfg.Date.Year() != 2026 || dCfg.Date.Day() != 24 {
		t.Fatalf("WithTimeLayouts failed: %+v (%v)", dCfg, err)
	}

	type customCfg struct {
		Wrapped string `cfg:"hello"`
		Skip    string `cfg:"-"`
		Nested  setterNested
	}
	a, err := NewApplier(
		WithTag("cfg"),
		WithDecoder(reflect.TypeFor[string](), func(raw string) (any, error) {
			return "custom:" + raw, nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	got, err := a.New[customCfg]()
	if err != nil {
		t.Fatalf("Applier.New failed: %v", err)
	}
	if got.Wrapped != "custom:hello" || got.Skip != "" {
		t.Fatalf("custom options failed: %+v", got)
	}

	nested, err := New[setterNested]()
	if err != nil || nested.Addr != "localhost:8080" {
		t.Fatalf("Setter failed: %+v (%v)", nested, err)
	}

	// Setter inside map values, interface values, and overlapping subslices.
	type container struct {
		ByName  map[string]setterNested
		Any     any
		SubHead []setterNested
		SubAll  []setterNested
	}
	backing := make([]setterNested, 2)
	c := container{
		ByName:  map[string]setterNested{"primary": {}},
		Any:     setterNested{},
		SubHead: backing[:1],
		SubAll:  backing[:2],
	}
	if err := Apply(&c); err != nil {
		t.Fatalf("Apply container failed: %v", err)
	}
	if c.ByName["primary"].Addr != "localhost:8080" {
		t.Fatalf("Setter in map dropped: %+v", c.ByName["primary"])
	}
	if c.Any.(setterNested).Addr != "localhost:8080" {
		t.Fatalf("Setter in interface dropped: %+v", c.Any)
	}
	if c.SubAll[1].Addr != "localhost:8080" {
		t.Fatalf("overlapping subslice element[1] skipped: %+v", c.SubAll[1])
	}
}

type panickingMutatingSetter struct {
	mu      sync.Mutex
	Tagged  string `default:"tagged-default"`
	Mutated string
}

func (p *panickingMutatingSetter) SetDefaults() {
	p.Mutated = "mutated-before-panic"
	panic("setter failed")
}

func TestSetterPanicRollsBackTagsAndPreservesSyncPrimitives(t *testing.T) {
	target := panickingMutatingSetter{}
	err := Apply(&target)
	if !errors.Is(err, ErrApplyPanic) {
		t.Fatalf("expected ErrApplyPanic, got %v", err)
	}
	if target.Tagged != "" {
		t.Fatalf("tag-applied field was not rolled back: %q", target.Tagged)
	}
	// Mutex remains usable untouched.
	target.mu.Lock()
	target.mu.Unlock()
}
