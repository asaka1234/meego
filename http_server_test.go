package main

// 定义数据模型
type User struct {
	ID    int    `json:"id" required:"true"`
	Name  string `json:"name" required:"true"`
	Email string `json:"email" required:"true"`
}

/*
func TestGetIPS(t *testing.T) {
	//----------------------------------
	// 创建服务器
	server := NewHTTPServer(":8080")
	// 添加全局中间件
	server.Use(Logger())
	server.Use(Recovery())
	server.Use(CORS())
	// 注册路由
	server.GET("/", func(c *Context) {
		c.JSON(200, JSON{
			"message": "Welcome to HTTPServer",
			"version": "1.0.0",
		})
	})

	// 需要认证的路由组
	authGroup := server.Group("/api/protected", Auth())
	{
		authGroup.GET("/profile", func(c *Context) {
			user := c.Get("user")
			c.JSON(200, JSON{
				"message": "Access granted to protected resource",
				"user":    user,
			})
		})

		authGroup.POST("/users", func(c *Context) {
			var user User
			if err := c.BindJSON(&user); err != nil {
				c.JSON(400, JSON{"error": err.Error()})
				return
			}

			if err := ValidateStruct(user); err != nil {
				c.JSON(400, JSON{"error": err.Error()})
				return
			}

			c.JSON(201, JSON{
				"message": "User created successfully",
				"user":    user,
			})
		})
	}

	// 启动服务器
	fmt.Println("Starting HTTPServer with middleware support...")
	if err := server.Start(); err != nil {
		panic(err)
	}
	//----------------------------------
}
*/
