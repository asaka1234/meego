package meego

// HTTPResponse 表示 HTTP 响应
type HTTPResponse struct {
	StatusCode int
	StatusText string
	Headers    map[string]string
	Body       []byte
}

// GetHeader returns value from request headers.
func (c *HTTPResponse) GetHeader(key string) string {
	if _, ok := c.Headers[key]; ok {
		return c.Headers[key]
	}
	return ""
}
