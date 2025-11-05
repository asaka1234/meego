package meego

import (
	"net"
	"net/url"
	"strconv"
)

// Context 请求上下文
type Context struct {
	Conn    net.Conn
	Request *HTTPRequest
	Writer  *ResponseWriter

	// 路径参数
	params map[string]string

	// 中间件数据
	Values   map[string]interface{}
	Index    int
	handlers []HandlerFunc
}

// 快速初始化
func (c *Context) fastInit(conn net.Conn, req *HTTPRequest, writer *ResponseWriter, params map[string]string, handler HandlerFunc) {
	c.Conn = conn
	c.Request = req
	c.Writer = writer
	c.params = params
	c.Index = -1

	// 重用 handlers 切片
	if cap(c.handlers) == 0 {
		c.handlers = make([]HandlerFunc, 0, 4)
	}
	c.handlers = c.handlers[:0]
	c.handlers = append(c.handlers, handler)

	// 清空 Values 但保留容量
	for k := range c.Values {
		delete(c.Values, k)
	}
}

// Context 方法
func (c *Context) Next() {
	c.Index++
	if c.Index < len(c.handlers) {
		c.handlers[c.Index](c)
	}
}

func (c *Context) JSON(code int, data interface{}) {
	c.Writer.Status(code).JSON(data)
}

func (c *Context) String(code int, text string) {
	c.Writer.Status(code).String(text)
}

func (c *Context) HTML(code int, html string) {
	c.Writer.Status(code).HTML(html)
}

func (c *Context) Set(key string, value interface{}) {
	c.Values[key] = value
}

func (c *Context) Get(key string) interface{} {
	return c.Values[key]
}

// Param 获取路径参数（类似 gin.Param）
func (c *Context) Param(key string) string {
	if c.params == nil {
		return ""
	}
	return c.params[key]
}

// Params 获取所有路径参数
func (c *Context) Params() map[string]string {
	return c.params
}

// SetParams 设置路径参数（内部使用）
func (c *Context) SetParams(params map[string]string) {
	c.params = params
}

// Query 获取查询字符串参数（类似 gin.Query）
func (c *Context) Query(key string) string {
	if c.Request.URL == nil {
		return ""
	}
	return c.Request.URL.Query().Get(key)
}

// QueryDefault 获取查询参数，如果不存在返回默认值
func (c *Context) QueryDefault(key, defaultValue string) string {
	if value := c.Query(key); value != "" {
		return value
	}
	return defaultValue
}

// QueryInt 获取整数类型的查询参数
func (c *Context) QueryInt(key string) (int, error) {
	value := c.Query(key)
	if value == "" {
		return 0, ErrQueryParamNotFound
	}
	return strconv.Atoi(value)
}

// QueryIntDefault 获取整数查询参数，如果不存在或解析失败返回默认值
func (c *Context) QueryIntDefault(key string, defaultValue int) int {
	if value, err := c.QueryInt(key); err == nil {
		return value
	}
	return defaultValue
}

// QueryBool 获取布尔类型的查询参数
func (c *Context) QueryBool(key string) (bool, error) {
	value := c.Query(key)
	if value == "" {
		return false, ErrQueryParamNotFound
	}
	return strconv.ParseBool(value)
}

// QueryBoolDefault 获取布尔查询参数，如果不存在或解析失败返回默认值
func (c *Context) QueryBoolDefault(key string, defaultValue bool) bool {
	if value, err := c.QueryBool(key); err == nil {
		return value
	}
	return defaultValue
}

// QueryFloat 获取浮点数类型的查询参数
func (c *Context) QueryFloat(key string) (float64, error) {
	value := c.Query(key)
	if value == "" {
		return 0, ErrQueryParamNotFound
	}
	return strconv.ParseFloat(value, 64)
}

// QueryFloatDefault 获取浮点数查询参数，如果不存在或解析失败返回默认值
func (c *Context) QueryFloatDefault(key string, defaultValue float64) float64 {
	if value, err := c.QueryFloat(key); err == nil {
		return value
	}
	return defaultValue
}

// QueryArray 获取数组类型的查询参数（同名参数）
func (c *Context) QueryArray(key string) []string {
	if c.Request.URL == nil {
		return nil
	}
	return c.Request.URL.Query()[key]
}

// QueryMap 获取映射类型的查询参数（key:value 格式）
func (c *Context) QueryMap(key string) map[string]string {
	if c.Request.URL == nil {
		return nil
	}

	query := c.Request.URL.Query()
	result := make(map[string]string)

	for k, values := range query {
		if len(values) > 0 {
			result[k] = values[0]
		}
	}

	return result
}

// GetQuery 获取查询参数，并返回是否存在
func (c *Context) GetQuery(key string) (string, bool) {
	value := c.Query(key)
	return value, value != ""
}

// GetQueryArray 获取查询参数数组，并返回是否存在
func (c *Context) GetQueryArray(key string) ([]string, bool) {
	values := c.QueryArray(key)
	return values, len(values) > 0
}

// GetQueryInt 获取整数查询参数，并返回是否存在和错误
func (c *Context) GetQueryInt(key string) (int, bool, error) {
	value, exists := c.GetQuery(key)
	if !exists {
		return 0, false, nil
	}
	intValue, err := strconv.Atoi(value)
	return intValue, true, err
}

// GetAllQuery 获取所有查询参数
func (c *Context) GetAllQuery() url.Values {
	if c.Request.URL == nil {
		return url.Values{}
	}
	return c.Request.URL.Query()
}

func (c *Context) ClientIP() string {
	// 简化实现，实际应该从 X-Forwarded-For 等头部获取
	return c.Conn.RemoteAddr().String()
}

//-------------------------------------------

// 错误定义
var ErrQueryParamNotFound = &QueryError{Message: "query parameter not found"}

type QueryError struct {
	Message string
}

func (e *QueryError) Error() string {
	return e.Message
}
