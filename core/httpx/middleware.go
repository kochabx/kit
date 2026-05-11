package httpx

import "net/http"

// RoundTripFunc 把函数适配为 http.RoundTripper。
type RoundTripFunc func(*http.Request) (*http.Response, error)

// RoundTrip 实现 http.RoundTripper。
func (f RoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// Middleware 用于在请求被发送之前 / 响应返回之后做拦截。
//
// 典型用法：
//
//	logMW := func(next httpx.RoundTripFunc) httpx.RoundTripFunc {
//	    return func(req *http.Request) (*http.Response, error) {
//	        start := time.Now()
//	        resp, err := next(req)
//	        log.Printf("%s %s -> %v %v", req.Method, req.URL, statusOf(resp), time.Since(start))
//	        return resp, err
//	    }
//	}
type Middleware func(next RoundTripFunc) RoundTripFunc

// chain 把一组中间件按声明顺序包装到 base 之外，先声明的位于最外层。
func chain(base http.RoundTripper, mws []Middleware) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	final := RoundTripFunc(base.RoundTrip)
	// 反向迭代，使第一个中间件位于最外层。
	for i := len(mws) - 1; i >= 0; i-- {
		final = mws[i](final)
	}
	return final
}
