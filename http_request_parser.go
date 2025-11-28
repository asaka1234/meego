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
			Headers: make(map[string]string, 16),
		}
	},
}

type HTTPRequest struct {
	Method  string
	URL     *url.URL
	Proto   string
	Headers map[string]string
	Body    []byte
	Host    string
	RawURL  string

	contentLength int
	parsed        bool
}

func (r *HTTPRequest) reset() {
	r.Method = ""
	r.URL = nil
	r.Proto = ""
	r.Host = ""
	r.RawURL = ""
	r.contentLength = 0
	r.parsed = false
	r.Body = r.Body[:0]

	for k := range r.Headers {
		delete(r.Headers, k)
	}
}

// 获取内容长度（带缓存）
func (r *HTTPRequest) ContentLength() int {
	if r.contentLength > 0 {
		return r.contentLength
	}

	if clStr := r.GetHeader("Content-Length"); clStr != "" {
		if cl, err := strconv.Atoi(clStr); err == nil && cl >= 0 {
			r.contentLength = cl
			return cl
		}
	}
	return 0
}

func (r *HTTPRequest) GetHeader(key string) string {
	return r.Headers[key]
}

func (r *HTTPRequest) ContentType() string {
	return filterFlags(r.GetHeader("Content-Type"))
}

func filterFlags(content string) string {
	for i, char := range content {
		if char == ' ' || char == ';' {
			return content[:i]
		}
	}
	return content
}

func ReleaseRequest(req *HTTPRequest) {
	if req != nil {
		req.reset()
		requestPool.Put(req)
	}
}

// HTTPParser 修复版本
type HTTPParser struct {
	reader      *bufio.Reader
	lineBuffer  []byte
	chunkBuffer []byte
}

func NewHTTPParser(conn net.Conn) *HTTPParser {
	return &HTTPParser{
		reader:      bufio.NewReader(conn),
		lineBuffer:  make([]byte, 0, 4096),
		chunkBuffer: make([]byte, 0, 8192),
	}
}

func (p *HTTPParser) ParseRequest() (*HTTPRequest, error) {
	req := requestPool.Get().(*HTTPRequest)

	if err := p.parseRequestInto(req); err != nil {
		ReleaseRequest(req) // 使用 ReleaseRequest 确保正确释放
		return nil, err
	}

	req.parsed = true
	return req, nil
}

func (p *HTTPParser) parseRequestInto(req *HTTPRequest) error {
	// 解析请求行
	if err := p.parseRequestLineFast(req); err != nil {
		return fmt.Errorf("request line error: %v", err)
	}

	// 解析头部
	if err := p.parseHeadersFast(req); err != nil {
		return fmt.Errorf("headers error: %v", err)
	}

	// 解析请求体
	if err := p.parseBodyFast(req); err != nil {
		return fmt.Errorf("body error: %v", err)
	}

	return nil
}

func (p *HTTPParser) parseRequestLineFast(req *HTTPRequest) error {
	line, err := p.readLineFast()
	if err != nil {
		return err
	}

	fmt.Printf("DEBUG Request line: %q\n", string(line))

	// 更健壮的请求行解析
	parts := bytes.Split(line, []byte{' '})
	if len(parts) < 2 {
		return fmt.Errorf("malformed request line: %q", string(line))
	}

	req.Method = string(parts[0])

	// 验证方法
	if !isValidMethod(req.Method) {
		return fmt.Errorf("invalid HTTP method: %s", req.Method)
	}

	req.RawURL = string(parts[1])

	// 处理协议
	if len(parts) >= 3 {
		req.Proto = string(parts[2])
	} else {
		req.Proto = "HTTP/1.1" // 默认
	}

	// 解析 URL - 更宽松的处理
	if req.RawURL == "" {
		req.RawURL = "/" // 默认路径
	}

	// 如果 URL 不包含协议，确保它以 / 开头
	if !strings.Contains(req.RawURL, "://") && req.RawURL[0] != '/' {
		req.RawURL = "/" + req.RawURL
	}

	parsedURL, err := url.Parse(req.RawURL)
	if err != nil {
		// 尝试 URL 编码处理
		encodedURL := url.QueryEscape(req.RawURL)
		if parsedURL, err = url.Parse(encodedURL); err != nil {
			return fmt.Errorf("failed to parse URL %q: %v", req.RawURL, err)
		}
	}
	req.URL = parsedURL

	fmt.Printf("DEBUG Parsed: %s %s %s\n", req.Method, req.RawURL, req.Proto)
	return nil
}

func isValidMethod(method string) bool {
	switch method {
	case "GET", "POST", "PUT", "DELETE", "HEAD", "OPTIONS", "PATCH", "CONNECT", "TRACE":
		return true
	default:
		// 也允许小写方法
		upperMethod := strings.ToUpper(method)
		switch upperMethod {
		case "GET", "POST", "PUT", "DELETE", "HEAD", "OPTIONS", "PATCH", "CONNECT", "TRACE":
			return true
		}
		return false
	}
}

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

	// 清理 CRLF
	if len(p.lineBuffer) > 0 && p.lineBuffer[len(p.lineBuffer)-1] == '\r' {
		p.lineBuffer = p.lineBuffer[:len(p.lineBuffer)-1]
	}

	return p.lineBuffer, nil
}

func (p *HTTPParser) parseHeadersFast(req *HTTPRequest) error {
	headerCount := 0

	for {
		line, err := p.readLineFast()
		if err != nil {
			if err == io.EOF && headerCount > 0 {
				break // 允许 EOF 作为头部结束
			}
			return err
		}

		// 空行表示头部结束
		if len(line) == 0 {
			break
		}

		idx := bytes.IndexByte(line, ':')
		if idx <= 0 { // 确保 key 不为空
			fmt.Printf("WARNING: Skipping malformed header: %q\n", string(line))
			continue
		}

		key := strings.TrimSpace(string(line[:idx]))
		value := strings.TrimSpace(string(line[idx+1:]))

		// 存储头部
		req.Headers[key] = value

		// 特殊处理
		if key == "Host" {
			req.Host = value
		}

		headerCount++
	}

	fmt.Printf("DEBUG Parsed %d headers\n", headerCount)
	return nil
}

func (p *HTTPParser) parseBodyFast(req *HTTPRequest) error {
	contentLength := req.ContentLength()

	if contentLength > 0 {
		// 检查大小限制
		if contentLength > 10*1024*1024 { // 10MB
			return fmt.Errorf("body too large: %d bytes", contentLength)
		}

		// 分配或重用 body
		if cap(req.Body) < contentLength {
			req.Body = make([]byte, contentLength)
		} else {
			req.Body = req.Body[:contentLength]
		}

		// 读取 body
		if _, err := io.ReadFull(p.reader, req.Body); err != nil {
			return fmt.Errorf("failed to read body: %v", err)
		}

		fmt.Printf("DEBUG Read body: %d bytes\n", contentLength)
	} else if te := req.GetHeader("Transfer-Encoding"); te != "" && strings.Contains(strings.ToLower(te), "chunked") {
		fmt.Println("DEBUG Processing chunked encoding")
		return p.parseChunkedBodyFast(req)
	} else {
		fmt.Println("DEBUG No body to read")
	}

	return nil
}

func (p *HTTPParser) parseChunkedBodyFast(req *HTTPRequest) error {
	p.chunkBuffer = p.chunkBuffer[:0]
	totalRead := 0

	for {
		// 读取块大小行
		line, err := p.readLineFast()
		if err != nil {
			return err
		}

		// 解析块大小
		chunkSize, err := strconv.ParseInt(string(line), 16, 64)
		if err != nil {
			return fmt.Errorf("invalid chunk size: %q", string(line))
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

		totalRead += int(chunkSize)
		if totalRead > 10*1024*1024 {
			return fmt.Errorf("chunked body too large: %d bytes", totalRead)
		}

		// 确保容量
		if cap(p.chunkBuffer) < len(p.chunkBuffer)+int(chunkSize) {
			newCap := (len(p.chunkBuffer) + int(chunkSize)) * 2
			newBuffer := make([]byte, len(p.chunkBuffer), newCap)
			copy(newBuffer, p.chunkBuffer)
			p.chunkBuffer = newBuffer
		}

		oldLen := len(p.chunkBuffer)
		p.chunkBuffer = p.chunkBuffer[:oldLen+int(chunkSize)]

		// 读取块数据
		if _, err := io.ReadFull(p.reader, p.chunkBuffer[oldLen:]); err != nil {
			return err
		}

		// 读取 CRLF
		if _, err := p.reader.Discard(2); err != nil {
			return err
		}
	}

	req.Body = append(req.Body[:0], p.chunkBuffer...)
	req.contentLength = len(req.Body)
	fmt.Printf("DEBUG Chunked body: %d bytes\n", len(req.Body))

	return nil
}
