package middleware

import (
	"bytes"
	"encoding/json"
	"html"
	"io"

	"github.com/gin-gonic/gin"
)

type XssConfig struct{}

func Xss() gin.HandlerFunc {
	return XssWithConfig(XssConfig{})
}

func XssWithConfig(config XssConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Escape all the query parameters
		query := c.Request.URL.Query()
		for key, values := range query {
			for i, value := range values {
				query[key][i] = html.EscapeString(value)
			}
		}
		c.Request.URL.RawQuery = query.Encode()

		// Escape all the form values
		c.Request.ParseForm()
		for key, values := range c.Request.PostForm {
			for i, value := range values {
				c.Request.PostForm[key][i] = html.EscapeString(value)
			}
		}

		// Escape all the header values
		for key, values := range c.Request.Header {
			for i, value := range values {
				c.Request.Header[key][i] = html.EscapeString(value)
			}
		}

		// Escape all the JSON values
		if c.Request.Header.Get("Content-Type") == "application/json" {
			bodyBytes, err := io.ReadAll(c.Request.Body)
			if err == nil {
				var bodyMap map[string]any
				if err := json.Unmarshal(bodyBytes, &bodyMap); err == nil {
					for key, value := range bodyMap {
						if strValue, ok := value.(string); ok {
							bodyMap[key] = html.EscapeString(strValue)
						}
					}
					newBodyBytes, _ := json.Marshal(bodyMap)
					c.Request.Body = io.NopCloser(bytes.NewBuffer(newBodyBytes))
				}
			}
		}

		c.Next()
	}
}
