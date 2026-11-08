package defaults

import (
	"encoding/json/v2"
	"errors"
	"net"
	"net/netip"
	"net/url"
	"reflect"
	"regexp"
	"strings"
	"time"
)

func newBuiltinDecoder(layouts []string) *builtinDecoder {
	d := &builtinDecoder{parsers: make(map[reflect.Type]func(reflect.Value, string) error)}
	parsers := json.JoinUnmarshalers(
		registerText(d, func(raw string) (time.Duration, error) { return time.ParseDuration(strings.TrimSpace(raw)) }),
		registerText(d, func(raw string) (time.Time, error) {
			for _, layout := range layouts {
				if value, err := time.Parse(layout, strings.TrimSpace(raw)); err == nil {
					return value, nil
				}
			}
			return time.Time{}, errors.New("time does not match configured layouts")
		}),
		registerText(d, func(raw string) (time.Location, error) {
			value, err := time.LoadLocation(strings.TrimSpace(raw))
			if err != nil {
				return time.Location{}, err
			}
			return *value, nil
		}),
		registerText(d, func(raw string) (url.URL, error) {
			value, err := url.Parse(strings.TrimSpace(raw))
			if err != nil {
				return url.URL{}, err
			}
			return *value, nil
		}),
		registerText(d, func(raw string) (net.IP, error) {
			value := net.ParseIP(strings.TrimSpace(raw))
			if value == nil {
				return nil, errors.New("invalid IP address")
			}
			return value, nil
		}),
		registerText(d, func(raw string) (net.IPNet, error) {
			ip, value, err := net.ParseCIDR(strings.TrimSpace(raw))
			if err != nil {
				return net.IPNet{}, err
			}
			value.IP = ip
			return *value, nil
		}),
		registerText(d, func(raw string) (regexp.Regexp, error) {
			value, err := regexp.Compile(raw)
			if err != nil {
				return regexp.Regexp{}, err
			}
			return *value, nil
		}),
		registerText(d, func(raw string) (net.HardwareAddr, error) { return net.ParseMAC(strings.TrimSpace(raw)) }),
		registerText(d, func(raw string) (net.TCPAddr, error) {
			value, err := netip.ParseAddrPort(strings.TrimSpace(raw))
			if err != nil {
				return net.TCPAddr{}, err
			}
			return *net.TCPAddrFromAddrPort(value), nil
		}),
		registerText(d, func(raw string) (net.UDPAddr, error) {
			value, err := netip.ParseAddrPort(strings.TrimSpace(raw))
			if err != nil {
				return net.UDPAddr{}, err
			}
			return *net.UDPAddrFromAddrPort(value), nil
		}),
		registerText(d, url.ParseQuery),
	)
	d.jsonOptions = json.JoinOptions(json.RejectUnknownMembers(true), json.WithUnmarshalers(parsers))
	return d
}
