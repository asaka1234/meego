package meego

import (
	"context"
	"fmt"
	jsoniter "github.com/json-iterator/go"
	"github.com/panjf2000/ants/v2"
	"io"
	"net"
	"strings"
	"sync"
	"time"
)

//---------------------------

// HandlerFunc 处理器函数类型
type HandlerFunc func(*Context)

// MiddlewareFunc 中间件函数类型
type MiddlewareFunc func(HandlerFunc) HandlerFunc

// JSON 响应结构
type JSON map[string]interface{}

//--------------------------------------------

// 全局对象池
var (
	contextPool        sync.Pool
	responseWriterPool sync.Pool
)

func init() {
	contextPool = sync.Pool{
		New: func() interface{} {
			return &Context{
				Values: make(map[string]interface{}, 8),
			}
		},
	}
	responseWriterPool = sync.Pool{
		New: func() interface{} {
			return &ResponseWriter{
				header: make(map[string]string, 16),
				json:   jsoniter.ConfigCompatibleWithStandardLibrary,
			}
		},
	}
}

// HTTPServer HTTP服务器 - 优化版本
type HTTPServer struct {
	addr        string
	router      *Router
	middlewares []MiddlewareFunc

	readTimeout  time.Duration
	writeTimeout time.Duration

	pool *ants.Pool
	// 性能优化字段
	mu         sync.RWMutex
	serverCtx  context.Context
	cancelFunc context.CancelFunc
}

// New 创建新的 HTTPServer 实例
func New() *HTTPServer {
	// 创建协程池，大小根据需求调整
	pool, err := ants.NewPool(5000, ants.WithExpiryDuration(30*time.Second))
	if err != nil {
		panic(err)
	}

	// 创建可取消的上下文
	ctx, cancel := context.WithCancel(context.Background())

	return &HTTPServer{
		router:       NewRouter(),
		middlewares:  []MiddlewareFunc{},
		pool:         pool,
		readTimeout:  10 * time.Second,
		writeTimeout: 10 * time.Second,
		serverCtx:    ctx,
		cancelFunc:   cancel,
	}
}

// Default 创建带有默认中间件的 HTTPServer
func Default() *HTTPServer {
	server := New()
	server.Use(Logger())
	server.Use(Recovery())
	return server
}

// Run 启动服务器
func (s *HTTPServer) Run(addr ...string) error {
	if len(addr) > 0 {
		s.addr = addr[0]
	}
	if s.addr == "" {
		s.addr = ":8080"
	}
	return s.Start()
}

// Listen 启动服务器
func (s *HTTPServer) Listen(addr string) error {
	s.addr = addr
	return s.Start()
}

// ListenAndServe 兼容 net/http 风格
func (s *HTTPServer) ListenAndServe(addr string) error {
	return s.Listen(addr)
}

// 创建新的 HTTPServer
func NewHTTPServer(addr string) *HTTPServer {
	server := New()
	server.addr = addr
	return server
}

// 优化的启动方法
func (s *HTTPServer) Start() error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	defer ln.Close()

	fmt.Printf("HTTPServer started on %s\n", s.addr)

	// 主接受循环
	for {
		select {
		case <-s.serverCtx.Done():
			fmt.Println("Server received shutdown signal")
			return nil
		default:
			fmt.Printf("=Start default==============\n")
			conn, err := ln.Accept()
			if err != nil {
				if ne, ok := err.(net.Error); ok && ne.Temporary() {
					time.Sleep(5 * time.Millisecond)
					continue
				}
				// 检查是否因为上下文取消导致的错误
				select {
				case <-s.serverCtx.Done():
					return nil
				default:
					return err
				}
			}

			// 优化连接参数
			if tc, ok := conn.(*net.TCPConn); ok {
				tc.SetNoDelay(true)
				tc.SetKeepAlive(true)
				tc.SetKeepAlivePeriod(3 * time.Minute)
			}

			// 使用协程池处理连接
			err = s.pool.Submit(func() {
				s.handleConnectionFast(conn)
			})
			if err != nil {
				// 协程池已满，直接关闭连接
				fmt.Printf("Pool is full, rejecting connection: %v\n", err)
				conn.Close()
			}
		}
	}
}

// 优化的连接处理方法
func (s *HTTPServer) handleConnectionFast(conn net.Conn) {
	// 对于短连接，可以禁用 Nagle 算法以减少延迟
	if tc, ok := conn.(*net.TCPConn); ok {
		tc.SetNoDelay(true)
		// 不需要设置 KeepAlive，因为马上就会关闭
	}

	remoteAddr := conn.RemoteAddr().String()
	fmt.Printf("DEBUG [%s] Connection established\n", remoteAddr)

	defer func() {
		conn.Close()
		fmt.Printf("DEBUG [%s] Connection closed\n", remoteAddr)

		if r := recover(); r != nil {
			// 静默处理 panic
			fmt.Printf("PANIC in handleConnectionFast: %v\n", r)
		}
		//conn.Close()
	}()

	// 为每个连接创建新的解析器
	parser := NewHTTPParser(conn)

	// 只处理一个请求，然后立即关闭连接
	conn.SetReadDeadline(time.Now().Add(s.readTimeout))

	fmt.Printf("DEBUG [%s] Waiting for request...\n", remoteAddr)
	// 使用对象池获取请求
	req, err := parser.ParseRequest()
	if err != nil {
		s.handleParseError(conn, remoteAddr, err)
		return // 直接返回，defer 会关闭连接
	}

	fmt.Printf("DEBUG [%s] Processing: %s %s\n", remoteAddr, req.Method, req.RawURL)
	s.processRequestFast(conn, req)
	ReleaseRequest(req)

	// 处理完一个请求就直接结束，连接会在 defer 中关闭
	fmt.Printf("DEBUG [%s] Request processed, closing connection\n", remoteAddr)
}

func (s *HTTPServer) handleParseError(conn net.Conn, remoteAddr string, err error) {
	switch {
	case err == io.EOF:
		fmt.Printf("DEBUG [%s] Client closed connection\n", remoteAddr)
	case isTimeoutError(err):
		fmt.Printf("DEBUG [%s] Read timeout (no data sent)\n", remoteAddr)
	default:
		fmt.Printf("DEBUG [%s] Parse error: %v\n", remoteAddr, err)
		if isParseError(err) {
			s.sendErrorFast(conn, 400, "Bad Request")
		}
	}
	// 错误处理完后，连接会在 defer 中自动关闭
}

// 优化的请求处理方法
func (s *HTTPServer) processRequestFast(conn net.Conn, req *HTTPRequest) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("PANIC in processRequestFast: %v\n", r)
			s.sendErrorFast(conn, 500, "Internal Server Error")
		}
		// 确保连接关闭
		conn.Close()
	}()
	// 设置写入超时
	conn.SetWriteDeadline(time.Now().Add(s.writeTimeout))

	// 快速路由查找
	handler, params := s.findRouteHandler(req.Method, req.URL.Path)
	if handler == nil {
		fmt.Printf("No handler found for %s %s\n", req.Method, req.URL.Path)
		s.sendErrorFast(conn, 404, "Not Found")
		return
	}

	// 从对象池获取上下文和响应写入器
	var ctx *Context
	var writer *ResponseWriter

	// 确保在函数返回时释放对象
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("Recovered from panic in request processing: %v\n", r)
		}

		// 重置并放回对象池
		if ctx != nil {
			ctx.reset()
			contextPool.Put(ctx)
		}
		if writer != nil {
			writer.reset()
			responseWriterPool.Put(writer)
		}
	}()

	// 安全地从对象池获取
	ctxObj := contextPool.Get()
	writerObj := responseWriterPool.Get()

	var ok bool
	ctx, ok = ctxObj.(*Context)
	if !ok || ctx == nil {
		fmt.Printf("Failed to get context from pool\n")
		s.sendErrorFast(conn, 500, "Internal Server Error")
		return
	}

	writer, ok = writerObj.(*ResponseWriter)
	if !ok || writer == nil {
		fmt.Printf("Failed to get writer from pool\n")
		s.sendErrorFast(conn, 500, "Internal Server Error")
		// 如果writer获取失败，但ctx已获取，需要放回
		if ctx != nil {
			ctx.reset()
			contextPool.Put(ctx)
		}
		return
	}

	// 快速初始化
	ctx.fastInit(conn, req, writer, params, handler)
	writer.fastInit(conn)
	// 强制短连接
	writer.SetHeader("Connection", "close")

	// 执行处理链
	ctx.Next()
}

// 优化的路由查找方法
func (s *HTTPServer) findRouteHandler(method, path string) (HandlerFunc, map[string]string) {
	handler, params := s.router.FindRoute(method, path)
	if handler == nil {
		return nil, nil
	}

	// 应用中间件（优化：避免在每次请求时创建闭包）
	s.mu.RLock()
	middlewares := s.middlewares
	s.mu.RUnlock()

	if len(middlewares) > 0 {
		wrappedHandler := handler
		for i := len(middlewares) - 1; i >= 0; i-- {
			wrappedHandler = middlewares[i](wrappedHandler)
		}
		return wrappedHandler, params
	}

	return handler, params
}

// 优化的错误发送方法
func (s *HTTPServer) sendErrorFast(conn net.Conn, code int, message string) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("Recovered from panic in error sending: %v\n", r)
		}
	}()
	// 从对象池获取响应写入器
	writerObj := responseWriterPool.Get()
	if writerObj == nil {
		// 对象池可能返回nil，需要处理
		fmt.Printf("Failed to get writer from pool for error response\n")
		return
	}
	writer, ok := writerObj.(*ResponseWriter)
	if !ok {
		fmt.Printf("Type assertion failed for writer from pool\n")
		return
	}
	// 关键：必须初始化writer！
	writer.fastInit(conn)

	defer func() {
		if writer != nil {
			writer.reset()
			responseWriterPool.Put(writer)
		}
	}()

	// 强制短连接
	writer.SetHeader("Connection", "close")
	writer.Status(code).JSON(JSON{
		"error": message,
		"code":  code,
	})
}

// 检查连接是否应该关闭
func (s *HTTPServer) shouldClose(req *HTTPRequest) bool {
	connection := req.GetHeader("Connection")
	return connection == "close"
}

// 关闭服务器
func (s *HTTPServer) Shutdown() {
	fmt.Printf("=Shutdown==============\n")

	select {
	case <-s.serverCtx.Done():
		// 已经关闭
		return
	default:
		s.cancelFunc() // 取消上下文
		s.pool.Release()
	}
}

// 添加全局中间件
func (s *HTTPServer) Use(middleware MiddlewareFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.middlewares = append(s.middlewares, middleware)
}

// 注册路由 - 线程安全版本
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

// 配置方法
func (s *HTTPServer) SetTimeout(readTimeout, writeTimeout time.Duration) {
	s.readTimeout = readTimeout
	s.writeTimeout = writeTimeout
}

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

// 预编译中间件链
func (g *RouteGroup) wrapHandler(handler HandlerFunc) HandlerFunc {
	if len(g.middlewares) == 0 {
		return handler
	}

	wrapped := handler
	for i := len(g.middlewares) - 1; i >= 0; i-- {
		wrapped = g.middlewares[i](wrapped)
	}
	return wrapped
}

func (g *RouteGroup) GET(path string, handler HandlerFunc) {
	fullPath := g.prefix + path
	wrappedHandler := g.wrapHandler(handler)
	g.server.GET(fullPath, wrappedHandler)
}

func (g *RouteGroup) POST(path string, handler HandlerFunc) {
	fullPath := g.prefix + path
	wrappedHandler := g.wrapHandler(handler)
	g.server.POST(fullPath, wrappedHandler)
}

// 添加其他方法
func (g *RouteGroup) PUT(path string, handler HandlerFunc) {
	fullPath := g.prefix + path
	wrappedHandler := g.wrapHandler(handler)
	g.server.PUT(fullPath, wrappedHandler)
}

func (g *RouteGroup) DELETE(path string, handler HandlerFunc) {
	fullPath := g.prefix + path
	wrappedHandler := g.wrapHandler(handler)
	g.server.DELETE(fullPath, wrappedHandler)
}

//====

// isTimeoutError 判断是否为超时错误
func isTimeoutError(err error) bool {
	if err == nil {
		return false
	}

	// 检查错误字符串
	errStr := err.Error()
	timeoutIndicators := []string{
		"timeout",
		"i/o timeout",
		"deadline exceeded",
		"timed out",
	}

	for _, indicator := range timeoutIndicators {
		if strings.Contains(strings.ToLower(errStr), indicator) {
			return true
		}
	}

	// 检查 net.Error 接口
	if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
		return true
	}

	return false
}

// isParseError 判断是否为真正的 HTTP 协议解析错误
func isParseError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()

	// 这些是真正的 HTTP 协议解析错误，应该返回 400
	parseErrorIndicators := []string{
		"invalid request line",
		"invalid HTTP method",
		"invalid URL",
		"malformed request line",
		"invalid chunk size",
		"failed to parse",
		"malformed",
		"invalid header",
		"invalid Content-Length",
		"body too large",
	}

	for _, indicator := range parseErrorIndicators {
		if strings.Contains(errStr, indicator) {
			return true
		}
	}

	return false
}

// isConnectionError 判断是否为连接相关错误（不是解析错误）
func isConnectionError(err error) bool {
	if err == nil {
		return false
	}

	if err == io.EOF {
		return true
	}

	errStr := err.Error()
	connectionErrorIndicators := []string{
		"closed",
		"reset",
		"refused",
		"broken pipe",
		"connection abort",
	}

	for _, indicator := range connectionErrorIndicators {
		if strings.Contains(errStr, indicator) {
			return true
		}
	}

	// 超时错误也是连接问题
	if isTimeoutError(err) {
		return true
	}

	return false
}
