// middleware.go
package main

import (
	"fmt"
	"github.com/rs/zerolog/log"
	"strings"
	"time"
)

// Logger 日志中间件
func Logger() MiddlewareFunc {
	return func(next HandlerFunc) HandlerFunc {
		return func(c *Context) {
			start := time.Now()

			// 调用下一个处理器
			next(c)

			// 记录日志
			duration := time.Since(start)
			cc := fmt.Sprintf("[%s] %s %s - %d - %v\n",
				time.Now().Format("2006-01-02 15:04:05"),
				c.Request.Method,
				c.Request.URL.Path,
				c.Writer.status,
				duration,
			)
			log.Info().Msg(cc)
		}
	}
}

// Recovery 恢复中间件
func Recovery() MiddlewareFunc {
	return func(next HandlerFunc) HandlerFunc {
		return func(c *Context) {
			defer func() {
				if err := recover(); err != nil {
					fmt.Printf("Panic recovered: %v\n", err)
					c.Writer.Status(500).JSON(JSON{
						"error": "Internal Server Error",
						"code":  500,
					})
				}
			}()
			next(c)
		}
	}
}

// CORS 跨域中间件
func CORS() MiddlewareFunc {
	return func(next HandlerFunc) HandlerFunc {
		return func(c *Context) {
			c.Writer.SetHeader("Access-Control-Allow-Origin", "*")
			c.Writer.SetHeader("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			c.Writer.SetHeader("Access-Control-Allow-Headers", "Content-Type, Authorization")

			if c.Request.Method == "OPTIONS" {
				c.Writer.Status(200).String("")
				return
			}

			next(c)
		}
	}
}

// Auth 认证中间件
func Auth() MiddlewareFunc {
	return func(next HandlerFunc) HandlerFunc {
		return func(c *Context) {
			token := c.Request.Headers["Authorization"]
			if token == "" {
				c.Writer.Status(401).JSON(JSON{
					"error": "Unauthorized",
					"code":  401,
				})
				return
			}

			// 简单的 token 验证
			if !strings.HasPrefix(token, "Bearer ") {
				c.Writer.Status(401).JSON(JSON{
					"error": "Invalid token format",
					"code":  401,
				})
				return
			}

			// 在实际应用中，这里应该验证 token 的有效性
			c.Set("user", "authenticated_user")
			next(c)
		}
	}
}

// Timeout 超时中间件
func Timeout(timeout time.Duration) MiddlewareFunc {
	return func(next HandlerFunc) HandlerFunc {
		return func(c *Context) {
			done := make(chan bool, 1)

			go func() {
				next(c)
				done <- true
			}()

			select {
			case <-done:
				return
			case <-time.After(timeout):
				c.Writer.Status(503).JSON(JSON{
					"error": "Request timeout",
					"code":  503,
				})
			}
		}
	}
}
