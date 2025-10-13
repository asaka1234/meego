package meego

import (
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
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

//---------------------------

// HandlerFunc 处理器函数类型
type HandlerFunc func(*Context)

// MiddlewareFunc 中间件函数类型
type MiddlewareFunc func(HandlerFunc) HandlerFunc

// HTTPServer HTTP服务器
type HTTPServer struct {
	addr        string
	router      *Router
	middlewares []MiddlewareFunc
}

// New 创建新的 HTTPServer 实例（类似 gin.New()）
func New() *HTTPServer {
	return &HTTPServer{
		router:      NewRouter(),
		middlewares: []MiddlewareFunc{},
	}
}

// Default 创建带有默认中间件的 HTTPServer（类似 gin.Default()）
func Default() *HTTPServer {
	server := New()
	server.Use(Logger())
	server.Use(Recovery())
	return server
}

// Run 启动服务器（类似 gin.Run()）
func (s *HTTPServer) Run(addr ...string) error {
	if len(addr) > 0 {
		s.addr = addr[0]
	}
	if s.addr == "" {
		s.addr = ":8080" // 默认端口
	}

	return s.Start()
}

// Listen 启动服务器（类似 gin.Listen()）
func (s *HTTPServer) Listen(addr string) error {
	s.addr = addr
	return s.Start()
}

// ListenAndServe 兼容 net/http 风格
func (s *HTTPServer) ListenAndServe(addr string) error {
	return s.Listen(addr)
}

//--------------------------------------------------

// ResponseWriter 响应写入器
type ResponseWriter struct {
	conn   net.Conn
	header map[string]string
	status int
}

// JSON 响应结构
type JSON map[string]interface{}

// 创建新的 HTTPServer
func NewHTTPServer(addr string) *HTTPServer {
	return &HTTPServer{
		addr:        addr,
		router:      NewRouter(),
		middlewares: []MiddlewareFunc{},
	}
}

// 启动服务器
func (s *HTTPServer) Start() error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	defer ln.Close()

	fmt.Printf("HTTPServer started on %s\n", s.addr)

	for {
		conn, err := ln.Accept()
		if err != nil {
			fmt.Printf("Accept error: %v\n", err)
			continue
		}
		go s.handleConnection(conn)
	}
}

// 处理连接
func (s *HTTPServer) handleConnection(conn net.Conn) {
	defer conn.Close()

	// 解析 HTTP 请求
	parser := NewHTTPParser(conn)
	req, err := parser.ParseRequest()
	if err != nil {
		fmt.Printf("Parse error: %v\n", err)
		s.sendError(conn, 400, "Bad Request")
		return
	}

	// 查找路由并解析路径参数
	handler, params := s.findRouteHandler(req.Method, req.URL.Path)
	if handler == nil {
		s.sendError(conn, 404, "Not Found")
		return
	}

	// 创建上下文
	ctx := &Context{
		Conn:    conn,
		Request: req,
		Writer:  NewResponseWriter(conn),
		Values:  make(map[string]interface{}),
		params:  params,
		Index:   -1,
	}
	
	// 执行处理链
	ctx.handlers = []HandlerFunc{handler}
	ctx.Next()
}

// findRouteHandler 更新查找路由方法
func (s *HTTPServer) findRouteHandler(method, path string) (HandlerFunc, map[string]string) {
	handler, params := s.router.FindRoute(method, path)
	if handler == nil {
		return nil, nil
	}

	// 应用中间件
	for i := len(s.middlewares) - 1; i >= 0; i-- {
		finalHandler := handler
		handler = s.middlewares[i](finalHandler)
	}

	return handler, params
}

// 发送错误响应
func (s *HTTPServer) sendError(conn net.Conn, code int, message string) {
	writer := NewResponseWriter(conn)
	writer.Status(code).JSON(JSON{
		"error": message,
		"code":  code,
	})
}

// 添加全局中间件
func (s *HTTPServer) Use(middleware MiddlewareFunc) {
	s.middlewares = append(s.middlewares, middleware)
}

// 注册路由
func (s *HTTPServer) GET(path string, handler HandlerFunc) {
	s.router.AddRoute("GET", path, handler)
}

func (s *HTTPServer) POST(path string, handler HandlerFunc) {
	s.router.AddRoute("POST", path, handler)
}

func (s *HTTPServer) PUT(path string, handler HandlerFunc) {
	s.router.AddRoute("PUT", path, handler)
}

func (s *HTTPServer) DELETE(path string, handler HandlerFunc) {
	s.router.AddRoute("DELETE", path, handler)
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

// ResponseWriter 方法
func NewResponseWriter(conn net.Conn) *ResponseWriter {
	return &ResponseWriter{
		conn:   conn,
		header: make(map[string]string),
		status: 200,
	}
}

func (w *ResponseWriter) Header() map[string]string {
	return w.header
}

func (w *ResponseWriter) SetHeader(key, value string) {
	w.header[key] = value
}

func (w *ResponseWriter) Status(code int) *ResponseWriter {
	w.status = code
	return w
}

func (w *ResponseWriter) JSON(data interface{}) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}

	w.SetHeader("Content-Type", "application/json; charset=utf-8")
	return w.writeResponse(jsonData)
}

func (w *ResponseWriter) String(text string) error {
	w.SetHeader("Content-Type", "text/plain; charset=utf-8")
	return w.writeResponse([]byte(text))
}

func (w *ResponseWriter) HTML(html string) error {
	w.SetHeader("Content-Type", "text/html; charset=utf-8")
	return w.writeResponse([]byte(html))
}

func (w *ResponseWriter) writeResponse(body []byte) error {
	// 构建状态行
	statusText := getStatusText(w.status)
	statusLine := fmt.Sprintf("HTTP/1.1 %d %s\r\n", w.status, statusText)

	// 设置默认头部
	if w.header["Content-Length"] == "" {
		w.header["Content-Length"] = strconv.Itoa(len(body))
	}
	if w.header["Connection"] == "" {
		w.header["Connection"] = "close"
	}

	// 构建响应
	var response strings.Builder
	response.WriteString(statusLine)

	for key, value := range w.header {
		response.WriteString(fmt.Sprintf("%s: %s\r\n", key, value))
	}
	response.WriteString("\r\n")

	if len(body) > 0 {
		response.Write(body)
	}

	_, err := w.conn.Write([]byte(response.String()))
	return err
}

// 工具函数
func getStatusText(code int) string {
	statusTexts := map[int]string{
		200: "OK",
		201: "Created",
		400: "Bad Request",
		404: "Not Found",
		500: "Internal Server Error",
	}
	if text, ok := statusTexts[code]; ok {
		return text
	}
	return "Unknown Status"
}

//------------------------------------------

// 路由组支持
func (s *HTTPServer) Group(prefix string, middlewares ...MiddlewareFunc) *RouteGroup {
	return &RouteGroup{
		server:      s,
		prefix:      prefix,
		middlewares: middlewares,
	}
}

type RouteGroup struct {
	server      *HTTPServer
	prefix      string
	middlewares []MiddlewareFunc
}

func (g *RouteGroup) GET(path string, handler HandlerFunc) {
	fullPath := g.prefix + path
	wrappedHandler := handler
	for i := len(g.middlewares) - 1; i >= 0; i-- {
		wrappedHandler = g.middlewares[i](wrappedHandler)
	}
	g.server.GET(fullPath, wrappedHandler)
}

func (g *RouteGroup) POST(path string, handler HandlerFunc) {
	fullPath := g.prefix + path
	wrappedHandler := handler
	for i := len(g.middlewares) - 1; i >= 0; i-- {
		wrappedHandler = g.middlewares[i](wrappedHandler)
	}
	g.server.POST(fullPath, wrappedHandler)
}
