package meego

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
)

// 全局对象池
var requestPool = sync.Pool{
	New: func() interface{} {
		return &HTTPRequest{
			Headers: make(map[string]string, 16), // 预分配容量
		}
	},
}

// HTTPRequest 表示解析后的 HTTP 请求
type HTTPRequest struct {
	Method  string
	URL     *url.URL
	Proto   string
	Headers map[string]string
	Body    []byte
	Host    string
	// 添加原始 URL 字符串用于查询参数解析
	RawURL string
	// 内部重用字段
	contentLength int
}

// 重置方法用于对象池
func (r *HTTPRequest) reset() {
	r.Method = ""
	r.URL = nil
	r.Proto = ""
	r.Host = ""
	r.RawURL = ""
	r.contentLength = 0

	// 重用 Body slice，但重置长度
	r.Body = r.Body[:0]

	// 清空 Headers 但保留 map 容量
	for k := range r.Headers {
		delete(r.Headers, k)
	}
}

// 便捷方法
func (r *HTTPRequest) Query() string {
	if r.URL != nil {
		return r.URL.RawQuery
	}
	return ""
}

func (r *HTTPRequest) Path() string {
	if r.URL != nil {
		return r.URL.Path
	}
	return ""
}

// 批量获取查询参数（避免多次解析）
func (r *HTTPRequest) QueryParams() url.Values {
	if r.URL != nil {
		return r.URL.Query()
	}
	return url.Values{}
}

// GetHeader returns value from request headers.
func (c *HTTPRequest) GetHeader(key string) string {
	if _, ok := c.Headers[key]; ok {
		return c.Headers[key]
	}
	return ""
}

func (c *HTTPRequest) ContentType() string {
	return filterFlags(c.GetHeader("Content-Type"))
}

func filterFlags(content string) string {
	for i, char := range content {
		if char == ' ' || char == ';' {
			return content[:i]
		}
	}
	return content
}

// 获取内容长度（缓存版本）
func (c *HTTPRequest) ContentLength() int {
	if c.contentLength > 0 {
		return c.contentLength
	}

	if clStr := c.GetHeader("Content-Length"); clStr != "" {
		if cl, err := strconv.Atoi(clStr); err == nil {
			c.contentLength = cl
			return cl
		}
	}
	return 0
}

// 释放请求对象
func ReleaseRequest(req *HTTPRequest) {
	if req != nil {
		requestPool.Put(req)
	}
}

//---------------------------------------

// 完整的 HTTP 解析器
type HTTPParser struct {
	reader      *bufio.Reader
	lineBuffer  []byte // 重用行缓冲区
	chunkBuffer []byte // 重用分块缓冲区
}

func NewHTTPParser(conn net.Conn) *HTTPParser {
	return &HTTPParser{
		reader:      bufio.NewReader(conn),
		lineBuffer:  make([]byte, 0, 4096), // 预分配 4KB
		chunkBuffer: make([]byte, 0, 8192), // 预分配 8KB
	}
}

func (p *HTTPParser) ParseRequest() (*HTTPRequest, error) {
	req := requestPool.Get().(*HTTPRequest)

	if err := p.parseRequestInto(req); err != nil {
		requestPool.Put(req)
		return nil, err
	}

	return req, nil
}

// ParseRequestInto 解析到现有对象
func (p *HTTPParser) ParseRequestInto(req *HTTPRequest) error {
	req.reset()
	return p.parseRequestInto(req)
}

// 核心解析方法
func (p *HTTPParser) parseRequestInto(req *HTTPRequest) error {
	// 解析请求行
	if err := p.parseRequestLineFast(req); err != nil {
		return err
	}

	// 解析头部
	if err := p.parseHeadersFast(req); err != nil {
		return err
	}

	// 解析请求体
	if err := p.parseBodyFast(req); err != nil {
		return err
	}

	return nil
}

func (p *HTTPParser) parseRequestLineFast(req *HTTPRequest) error {
	line, err := p.readLineFast()
	if err != nil {
		return err
	}

	// 手动分割，避免 strings.Split 分配
	// 格式: METHOD URL PROTO
	firstSpace := bytes.IndexByte(line, ' ')
	if firstSpace == -1 {
		return fmt.Errorf("invalid request line")
	}

	secondSpace := bytes.IndexByte(line[firstSpace+1:], ' ')
	if secondSpace == -1 {
		return fmt.Errorf("invalid request line")
	}
	secondSpace += firstSpace + 1

	req.Method = string(line[:firstSpace])
	req.RawURL = string(line[firstSpace+1 : secondSpace])
	req.Proto = string(line[secondSpace+1:])

	// 解析 URL
	parsedURL, err := url.Parse(req.RawURL)
	if err != nil {
		return err
	}
	req.URL = parsedURL

	return nil
}

// 快速读取行 - 重用缓冲区
func (p *HTTPParser) readLineFast() ([]byte, error) {
	p.lineBuffer = p.lineBuffer[:0]

	for {
		line, isPrefix, err := p.reader.ReadLine()
		if err != nil {
			return nil, err
		}

		p.lineBuffer = append(p.lineBuffer, line...)

		if !isPrefix {
			break
		}
	}

	return p.lineBuffer, nil
}

func (p *HTTPParser) parseHeadersFast(req *HTTPRequest) error {
	for {
		line, err := p.readLineFast()
		if err != nil {
			return err
		}

		// 空行表示头部结束
		if len(line) == 0 {
			break
		}

		idx := bytes.IndexByte(line, ':')
		if idx == -1 {
			continue // 跳过无效头部
		}

		// 使用字节操作避免字符串分配
		key := strings.TrimSpace(string(line[:idx]))
		value := strings.TrimSpace(string(line[idx+1:]))

		req.Headers[key] = value

		// 特殊处理 Host 头
		if key == "Host" {
			req.Host = value
		}
	}
	return nil
}

func (p *HTTPParser) parseBodyFast(req *HTTPRequest) error {
	// 检查 Content-Length
	if clStr := req.GetHeader("Content-Length"); clStr != "" {
		contentLength, err := strconv.Atoi(clStr)
		if err != nil {
			return err
		}

		if contentLength > 0 {
			// 重用 Body slice
			if cap(req.Body) < contentLength {
				req.Body = make([]byte, contentLength)
			} else {
				req.Body = req.Body[:contentLength]
			}

			_, err := io.ReadFull(p.reader, req.Body)
			if err != nil {
				return err
			}
			req.contentLength = contentLength
		}
		return nil
	}

	// 处理 Transfer-Encoding: chunked
	if te := req.GetHeader("Transfer-Encoding"); te != "" && strings.Contains(te, "chunked") {
		return p.parseChunkedBodyFast(req)
	}

	return nil
}

func (p *HTTPParser) parseChunkedBodyFast(req *HTTPRequest) error {
	// 重用 chunkBuffer
	p.chunkBuffer = p.chunkBuffer[:0]

	for {
		// 读取块大小
		line, err := p.readLineFast()
		if err != nil {
			return err
		}

		chunkSize, err := strconv.ParseInt(string(line), 16, 64)
		if err != nil {
			return err
		}

		if chunkSize == 0 {
			// 读取尾部头部
			for {
				line, err := p.readLineFast()
				if err != nil {
					return err
				}
				if len(line) == 0 {
					break
				}
			}
			break
		}

		// 确保 chunkBuffer 有足够容量
		if cap(p.chunkBuffer) < len(p.chunkBuffer)+int(chunkSize) {
			// 需要扩容
			newBuffer := make([]byte, len(p.chunkBuffer), (len(p.chunkBuffer)+int(chunkSize))*2)
			copy(newBuffer, p.chunkBuffer)
			p.chunkBuffer = newBuffer
		}

		oldLen := len(p.chunkBuffer)
		p.chunkBuffer = p.chunkBuffer[:oldLen+int(chunkSize)]

		// 读取块数据
		_, err = io.ReadFull(p.reader, p.chunkBuffer[oldLen:])
		if err != nil {
			return err
		}

		// 读取 CRLF
		if _, err := p.reader.Discard(2); err != nil {
			return err
		}
	}

	// 设置 body
	req.Body = append(req.Body[:0], p.chunkBuffer...)
	req.contentLength = len(req.Body)

	return nil
}
