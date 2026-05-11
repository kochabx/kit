package httpx

import (
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// RequestOption 配置单次请求。
type RequestOption func(*requestConfig)

// requestConfig 是单次请求的可变配置，由 RequestOption 填充。
type requestConfig struct {
	header http.Header
	query  url.Values
	decode func(*http.Response) error
}

// Header 添加单个请求头。多次调用同一 key 会追加值。
func Header(key, value string) RequestOption {
	return func(c *requestConfig) { c.header.Add(key, value) }
}

// SetHeader 设置单个请求头，覆盖同名值。
func SetHeader(key, value string) RequestOption {
	return func(c *requestConfig) { c.header.Set(key, value) }
}

// Headers 批量设置请求头 (覆盖同名值)。
func Headers(headers map[string]string) RequestOption {
	return func(c *requestConfig) {
		for k, v := range headers {
			c.header.Set(k, v)
		}
	}
}

// Bearer 设置 Authorization: Bearer <token>。
func Bearer(token string) RequestOption {
	return SetHeader("Authorization", "Bearer "+token)
}

// BasicAuth 设置 HTTP Basic 认证头。
func BasicAuth(username, password string) RequestOption {
	creds := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
	return SetHeader("Authorization", "Basic "+creds)
}

// Query 追加单个 query 参数。
func Query(key, value string) RequestOption {
	return func(c *requestConfig) { c.query.Add(key, value) }
}

// SetQuery 设置单个 query 参数 (覆盖同名值)。
func SetQuery(key, value string) RequestOption {
	return func(c *requestConfig) { c.query.Set(key, value) }
}

// Queries 批量追加 query 参数。
func Queries(values url.Values) RequestOption {
	return func(c *requestConfig) {
		for k, vs := range values {
			for _, v := range vs {
				c.query.Add(k, v)
			}
		}
	}
}

// Into 根据响应的 Content-Type 自动解码到 dest：
//   - application/json => json.Unmarshal
//   - application/xml / text/xml => xml.Unmarshal
//   - 当 dest 为 *[]byte / *string 时，直接保存原始响应体
//
// 解码完成后 Body 会被关闭。如果状态码被判定为错误 (HTTPError)，不会进行解码。
func Into(dest any) RequestOption {
	return func(c *requestConfig) {
		c.decode = func(resp *http.Response) error {
			return decodeAuto(resp, dest)
		}
	}
}

// IntoJSON 强制以 JSON 解码到 dest，忽略响应 Content-Type。
func IntoJSON(dest any) RequestOption {
	return func(c *requestConfig) {
		c.decode = func(resp *http.Response) error {
			defer resp.Body.Close()
			return json.NewDecoder(resp.Body).Decode(dest)
		}
	}
}

// IntoXML 强制以 XML 解码到 dest。
func IntoXML(dest any) RequestOption {
	return func(c *requestConfig) {
		c.decode = func(resp *http.Response) error {
			defer resp.Body.Close()
			return xml.NewDecoder(resp.Body).Decode(dest)
		}
	}
}

// IntoBytes 将原始响应体写入 *dest。
func IntoBytes(dest *[]byte) RequestOption {
	return func(c *requestConfig) {
		c.decode = func(resp *http.Response) error {
			defer resp.Body.Close()
			data, err := io.ReadAll(resp.Body)
			if err != nil {
				return err
			}
			*dest = data
			return nil
		}
	}
}

// IntoString 将原始响应体写入 *dest。
func IntoString(dest *string) RequestOption {
	return func(c *requestConfig) {
		c.decode = func(resp *http.Response) error {
			defer resp.Body.Close()
			data, err := io.ReadAll(resp.Body)
			if err != nil {
				return err
			}
			*dest = string(data)
			return nil
		}
	}
}

// Decode 注册一个自定义响应处理函数，调用方负责关闭 resp.Body。
func Decode(fn func(*http.Response) error) RequestOption {
	return func(c *requestConfig) { c.decode = fn }
}

// decodeAuto 根据 Content-Type 自动选择解码方式。
func decodeAuto(resp *http.Response, dest any) error {
	defer resp.Body.Close()

	switch d := dest.(type) {
	case *[]byte:
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		*d = data
		return nil
	case *string:
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		*d = string(data)
		return nil
	}

	ct := resp.Header.Get("Content-Type")
	mediaType := strings.ToLower(strings.TrimSpace(strings.SplitN(ct, ";", 2)[0]))
	switch {
	case mediaType == "" || strings.Contains(mediaType, "json"):
		return json.NewDecoder(resp.Body).Decode(dest)
	case strings.Contains(mediaType, "xml"):
		return xml.NewDecoder(resp.Body).Decode(dest)
	default:
		return fmt.Errorf("httpx: cannot auto-decode Content-Type %q into %T", ct, dest)
	}
}
