package httpx

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/url"
)

// Body 描述一次请求的有效载荷。
//
// Encode 必须可以被安全地多次调用 (用于重试)，返回的字节切片不应在外部被修改。
// 当 data 为 nil 时表示没有 body；contentType 为空时不会自动设置 Content-Type。
type Body interface {
	Encode() (data []byte, contentType string, err error)
}

// bodyFunc 把函数适配为 Body 接口。
type bodyFunc func() ([]byte, string, error)

func (f bodyFunc) Encode() ([]byte, string, error) { return f() }

// NoBody 表示不携带请求体。等价于传入 nil Body。
func NoBody() Body { return bodyFunc(func() ([]byte, string, error) { return nil, "", nil }) }

// JSON 使用 application/json 编码 v 作为请求体。
func JSON(v any) Body {
	return bodyFunc(func() ([]byte, string, error) {
		data, err := json.Marshal(v)
		if err != nil {
			return nil, "", fmt.Errorf("httpx: marshal json body: %w", err)
		}
		return data, ContentTypeJSON, nil
	})
}

// XML 使用 application/xml 编码 v 作为请求体。
func XML(v any) Body {
	return bodyFunc(func() ([]byte, string, error) {
		data, err := xml.Marshal(v)
		if err != nil {
			return nil, "", fmt.Errorf("httpx: marshal xml body: %w", err)
		}
		return data, ContentTypeXML, nil
	})
}

// Form 使用 application/x-www-form-urlencoded 编码 values 作为请求体。
func Form(values url.Values) Body {
	return bodyFunc(func() ([]byte, string, error) {
		return []byte(values.Encode()), ContentTypeForm, nil
	})
}

// FormMap 是 Form 的便捷版本，接收 map[string]string。
func FormMap(values map[string]string) Body {
	v := make(url.Values, len(values))
	for k, val := range values {
		v.Set(k, val)
	}
	return Form(v)
}

// Raw 以给定 contentType 直接发送原始字节。contentType 为空时不会设置 Content-Type 头。
func Raw(contentType string, data []byte) Body {
	return bodyFunc(func() ([]byte, string, error) { return data, contentType, nil })
}

// Text 以 text/plain 发送字符串。
func Text(s string) Body { return Raw(ContentTypeText, []byte(s)) }

// ReadAll 从 io.Reader 一次性读出全部字节，然后以 contentType 发送。
//
// 该函数会立即消费 reader，因此调用后请勿再读取。如果 reader 实现了 io.Closer，会被关闭。
func ReadAll(contentType string, r io.Reader) Body {
	return bodyFunc(func() ([]byte, string, error) {
		if r == nil {
			return nil, contentType, nil
		}
		data, err := io.ReadAll(r)
		if c, ok := r.(io.Closer); ok {
			_ = c.Close()
		}
		if err != nil {
			return nil, "", fmt.Errorf("httpx: read body: %w", err)
		}
		return data, contentType, nil
	})
}
