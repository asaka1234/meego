// quick_start.go
package meego

// 快速启动函数
func Start(addr ...string) *HTTPServer {
	server := Default()
	address := ":8080"
	if len(addr) > 0 {
		address = addr[0]
	}

	go func() {
		if err := server.Run(address); err != nil {
			panic(err)
		}
	}()

	return server
}

// 创建基础服务器
func Create() *HTTPServer {
	return New()
}

// 创建带默认中间件的服务器
func CreateWithDefaults() *HTTPServer {
	return Default()
}
