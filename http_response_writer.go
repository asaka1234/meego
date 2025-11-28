package meego

import (
	"fmt"
	jsoniter "github.com/json-iterator/go"
	"net"
	"strconv"
	"strings"
	"sync"
)

// ResponseWriter 响应写入器
type ResponseWriter struct {
	conn   net.Conn
	header map[string]string
	status int
	json   jsoniter.API //序列化/反序列化

	// 缓冲优化
	buffer strings.Builder
	mu     sync.Mutex
}

// ResponseWriter 方法
func NewResponseWriter(conn net.Conn) *ResponseWriter {
	return &ResponseWriter{
		conn:   conn,
		header: make(map[string]string),
		status: 200,
		json:   jsoniter.ConfigCompatibleWithStandardLibrary,
	}
}

// 快速初始化
func (w *ResponseWriter) fastInit(conn net.Conn) {
	w.conn = conn
	w.status = 200
	w.buffer.Reset()

	// 清空 header 但保留容量
	for k := range w.header {
		delete(w.header, k)
	}
}

// 重置方法用于对象池
func (w *ResponseWriter) reset() {
	w.conn = nil
	w.status = 200
	w.buffer.Reset()

	if w.header != nil {
		for k := range w.header {
			delete(w.header, k)
		}
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
	jsonData, err := w.json.Marshal(data)
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
	w.mu.Lock()
	defer w.mu.Unlock()

	// 重用 buffer
	w.buffer.Reset()

	// 构建状态行
	statusText := getStatusText(w.status)
	w.buffer.WriteString(fmt.Sprintf("HTTP/1.1 %d %s\r\n", w.status, statusText))

	// 设置默认头部
	if w.header["Content-Length"] == "" {
		w.header["Content-Length"] = strconv.Itoa(len(body))
	}
	// 设置 Connection: close
	w.header["Connection"] = "close"

	// 写入头部
	for key, value := range w.header {
		w.buffer.WriteString(fmt.Sprintf("%s: %s\r\n", key, value))
	}
	w.buffer.WriteString("\r\n")

	// 批量写入
	headers := w.buffer.String()
	if len(body) > 0 {
		// 使用 net.Buffers 减少系统调用
		buffers := net.Buffers{[]byte(headers), body}
		_, err := buffers.WriteTo(w.conn)
		return err
	} else {
		_, err := w.conn.Write([]byte(headers))
		return err
	}
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
