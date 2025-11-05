package meego

import (
	"fmt"
	jsoniter "github.com/json-iterator/go"
	"net"
	"strconv"
	"strings"
)

// ResponseWriter 响应写入器
type ResponseWriter struct {
	conn   net.Conn
	header map[string]string
	status int
	json   jsoniter.API //序列化/反序列化
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
