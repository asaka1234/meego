package main

import (
	"os"
	"os/signal"
	"syscall"
	"testing"
)

func Test2(t *testing.T) {
	// 创建服务器实例
	app := New()

	// 全局中间件
	app.Use(CORS())
	app.Use(Logger())

	// 静态文件服务（简化版）
	app.GET("/static/*filepath", func(c *Context) {
		c.JSON(200, JSON{
			"message": "Static file serving would go here",
			"file":    c.Request.URL.Path,
		})
	})

	// RESTful API 路由
	app.GET("/api/users", getUsers)
	app.GET("/api/users/:id", getUser)
	app.POST("/api/users", createUser)
	app.PUT("/api/users/:id", updateUser)
	app.DELETE("/api/users/:id", deleteUser)

	// 启动服务器
	println("Server starting on :8080...")
	app.Run(":8080")

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGKILL, syscall.SIGHUP)
	<-quit
}

// 处理器函数
func getUsers(c *Context) {
	users := []User{
		{ID: 1, Name: "Alice", Email: "alice@example.com"},
		{ID: 2, Name: "Bob", Email: "bob@example.com"},
	}
	c.JSON(200, JSON{"users": users})
}

func getUser(c *Context) {
	// 实际应该从数据库查询
	user := User{ID: 1, Name: "Alice", Email: "alice@example.com"}
	c.JSON(200, JSON{"user": user})
}

func createUser(c *Context) {
	var user User
	if err := c.BindJSON(&user); err != nil {
		c.JSON(400, JSON{"error": err.Error()})
		return
	}

	// 实际应该保存到数据库
	user.ID = 3
	c.JSON(201, JSON{"user": user})
}

func updateUser(c *Context) {
	var user User
	if err := c.BindJSON(&user); err != nil {
		c.JSON(400, JSON{"error": err.Error()})
		return
	}

	c.JSON(200, JSON{"user": user, "message": "User updated"})
}

func deleteUser(c *Context) {
	c.JSON(200, JSON{"message": "User deleted"})
}
