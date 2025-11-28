package meego

import (
	"context"
	"fmt"
	jsoniter "github.com/json-iterator/go"
	"github.com/panjf2000/ants/v2"
	"io"
	"net"
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
	pool, err := ants.NewPool(1000, ants.WithExpiryDuration(10*time.Second))
	if err != nil {
		panic(err)
	}

	// 创建可取消的上下文
	ctx, cancel := context.WithCancel(context.Background())

	return &HTTPServer{
		router:       NewRouter(),
		middlewares:  []MiddlewareFunc{},
		pool:         pool,
		readTimeout:  30 * time.Second,
		writeTimeout: 30 * time.Second,
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
	defer func() {
		if r := recover(); r != nil {
			// 静默处理 panic
			fmt.Printf("PANIC in handleConnectionFast: %v\n", r)
		}
		//conn.Close()
	}()

	// 为每个连接创建新的解析器
	parser := NewHTTPParser(conn)

	// 支持 keep-alive 连接
	for {
		// 检查服务器是否已关闭
		select {
		case <-s.serverCtx.Done():
			// 已经关闭
			fmt.Printf("=Start default==============\n")
			conn.Close()
			return
		default:
		}

		// 设置读取超时
		conn.SetReadDeadline(time.Now().Add(s.readTimeout))

		// 使用对象池获取请求
		req, err := parser.ParseRequest()
		if err != nil {
			fmt.Printf("ParseRequest failed: %v, error type: %T\n", err, err)

			if err != io.EOF {
				fmt.Println("Client closed connection (EOF)")
			}
			// 其他错误，发送错误响应后关闭连接
			s.sendErrorFast(conn, 400, fmt.Sprintf("Bad Request: %v", err))
			break
		}

		// 处理请求
		s.processRequestFast(conn, req)

		// 释放请求对象
		ReleaseRequest(req)

		// 检查是否保持连接
		if s.shouldClose(req) {
			break
		}
	}
	// 循环结束后关闭连接
	conn.Close()
}

// 优化的请求处理方法
func (s *HTTPServer) processRequestFast(conn net.Conn, req *HTTPRequest) {
	// 设置写入超时
	conn.SetWriteDeadline(time.Now().Add(s.writeTimeout))

	// 快速路由查找
	handler, params := s.findRouteHandler(req.Method, req.URL.Path)
	if handler == nil {
		s.sendErrorFast(conn, 404, "Not Found")
		return
	}

	// 从对象池获取上下文和响应写入器
	ctx := contextPool.Get().(*Context)
	writer := responseWriterPool.Get().(*ResponseWriter)

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

	// 快速初始化
	ctx.fastInit(conn, req, writer, params, handler)
	writer.fastInit(conn)

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
	writer := responseWriterPool.Get().(*ResponseWriter)
	defer func() {
		if writer != nil {
			writer.reset()
			responseWriterPool.Put(writer)
		}
	}()

	writer.fastInit(conn)
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
