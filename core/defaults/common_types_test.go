package defaults

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"math/big"
	"net"
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type streamDefault string

func (v *streamDefault) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	var raw string
	if err := json.UnmarshalDecode(dec, &raw); err != nil {
		return err
	}
	*v = streamDefault("decoded:" + raw)
	return nil
}

func TestAdditionalCommonTypes(t *testing.T) {
	var target struct {
		MAC      net.HardwareAddr `default:"02:00:5e:10:00:00"`
		TCP      *net.TCPAddr     `default:"[fe80::1%eth0]:8080"`
		UDP      net.UDPAddr      `default:"127.0.0.1:5353"`
		Query    url.Values       `default:"name=a&name=b&path=%2Fapi"`
		Integer  big.Int          `default:"123456789012345678901234567890"`
		Rational big.Rat          `default:"22/7"`
		Float    big.Float        `default:"1.25"`
		Stream   streamDefault    `default:"\"value\""`
	}
	require.NoError(t, Apply(&target))
	require.Equal(t, "02:00:5e:10:00:00", target.MAC.String())
	require.Equal(t, "[fe80::1%eth0]:8080", target.TCP.String())
	require.Equal(t, "127.0.0.1:5353", target.UDP.String())
	require.Equal(t, []string{"a", "b"}, target.Query["name"])
	require.Equal(t, "/api", target.Query.Get("path"))
	require.Equal(t, "123456789012345678901234567890", target.Integer.String())
	require.Equal(t, "22/7", target.Rational.String())
	require.Equal(t, "1.25", target.Float.Text('f', 2))
	require.Equal(t, streamDefault("decoded:value"), target.Stream)
}

func TestCommonTypesInJSONCollections(t *testing.T) {
	var target struct {
		Durations []time.Duration    `default:"[\"100ms\",\"2s\"]"`
		URLs      map[string]url.URL `default:"{\"api\":\"https://example.com/api\"}"`
		Addresses [2]net.TCPAddr     `default:"[\"127.0.0.1:80\",\"[::1]:443\"]"`
		Nested    struct {
			Timeout time.Duration `json:"timeout"`
			Query   url.Values    `json:"query"`
		} `default:"{\"timeout\":\"3s\",\"query\":\"a=1&a=2\"}"`
	}
	require.NoError(t, Apply(&target))
	require.Equal(t, []time.Duration{100 * time.Millisecond, 2 * time.Second}, target.Durations)
	require.Equal(t, "example.com", target.URLs["api"].Host)
	require.Equal(t, "[::1]:443", target.Addresses[1].String())
	require.Equal(t, 3*time.Second, target.Nested.Timeout)
	require.Equal(t, []string{"1", "2"}, target.Nested.Query["a"])
}

func TestInvalidCommonTypesAreAtomic(t *testing.T) {
	for _, tc := range []struct {
		name   string
		target any
	}{
		{"MAC", &struct {
			Value net.HardwareAddr `default:"invalid"`
		}{}},
		{"TCP hostname", &struct {
			Value net.TCPAddr `default:"localhost:80"`
		}{}},
		{"UDP port", &struct {
			Value net.UDPAddr `default:"127.0.0.1:65536"`
		}{}},
		{"query", &struct {
			Value url.Values `default:"a=%zz"`
		}{}},
		{"collection", &struct {
			Value []time.Duration `default:"[\"1s\",\"invalid\"]"`
		}{}},
		{"unknown member", &struct {
			Value struct {
				Timeout time.Duration `json:"timeout"`
			} `default:"{\"timeout\":\"1s\",\"extra\":1}"`
		}{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snapshot, err := deepClone(reflect.ValueOf(tc.target), defaultMaxDepth)
			require.NoError(t, err)
			before := snapshot.Interface()
			require.Error(t, Apply(tc.target))
			require.Equal(t, before, tc.target)
		})
	}
}
