package meego

import (
	"fmt"
	"net"
)

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
