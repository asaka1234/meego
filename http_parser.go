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
)

// HTTPRequest 表示解析后的 HTTP 请求
type HTTPRequest struct {
	Method  string
	URL     *url.URL
	Proto   string
	Headers map[string]string
	Body    []byte
	Host    string
}

// HTTPResponse 表示 HTTP 响应
type HTTPResponse struct {
	StatusCode int
	StatusText string
	Headers    map[string]string
	Body       []byte
}

//---------------------------------------

// 完整的 HTTP 解析器
type HTTPParser struct {
	reader *bufio.Reader
}

func NewHTTPParser(conn net.Conn) *HTTPParser {
	return &HTTPParser{
		reader: bufio.NewReader(conn),
	}
}

func (p *HTTPParser) ParseRequest() (*HTTPRequest, error) {
	req := &HTTPRequest{
		Headers: make(map[string]string),
	}

	// 解析请求行
	if err := p.parseRequestLine(req); err != nil {
		return nil, err
	}

	// 解析头部
	if err := p.parseHeaders(req); err != nil {
		return nil, err
	}

	// 解析请求体
	if err := p.parseBody(req); err != nil {
		return nil, err
	}

	return req, nil
}

func (p *HTTPParser) parseRequestLine(req *HTTPRequest) error {
	line, err := p.reader.ReadString('\n')
	if err != nil {
		return err
	}

	line = strings.TrimSpace(line)
	parts := strings.Split(line, " ")
	if len(parts) != 3 {
		return fmt.Errorf("invalid request line: %s", line)
	}

	req.Method = parts[0]
	req.URL, err = url.Parse(parts[1])
	if err != nil {
		return err
	}
	req.Proto = parts[2]

	return nil
}

func (p *HTTPParser) parseHeaders(req *HTTPRequest) error {
	for {
		line, err := p.reader.ReadString('\n')
		if err != nil {
			return err
		}

		line = strings.TrimSpace(line)
		if line == "" {
			break
		}

		idx := strings.Index(line, ":")
		if idx == -1 {
			continue
		}

		key := strings.TrimSpace(line[:idx])
		value := strings.TrimSpace(line[idx+1:])
		req.Headers[key] = value

		// 特殊处理 Host 头
		if key == "Host" {
			req.Host = value
		}
	}
	return nil
}

func (p *HTTPParser) parseBody(req *HTTPRequest) error {
	// 检查 Content-Length
	if clStr, ok := req.Headers["Content-Length"]; ok {
		contentLength, err := strconv.Atoi(clStr)
		if err != nil {
			return err
		}

		if contentLength > 0 {
			req.Body = make([]byte, contentLength)
			_, err := io.ReadFull(p.reader, req.Body)
			if err != nil {
				return err
			}
		}
	}

	// 处理 Transfer-Encoding: chunked
	if te, ok := req.Headers["Transfer-Encoding"]; ok && strings.Contains(te, "chunked") {
		return p.parseChunkedBody(req)
	}

	return nil
}

func (p *HTTPParser) parseChunkedBody(req *HTTPRequest) error {
	var body bytes.Buffer

	for {
		// 读取块大小
		line, err := p.reader.ReadString('\n')
		if err != nil {
			return err
		}

		line = strings.TrimSpace(line)
		chunkSize, err := strconv.ParseInt(line, 16, 64)
		if err != nil {
			return err
		}

		if chunkSize == 0 {
			// 读取尾部头部
			for {
				line, err := p.reader.ReadString('\n')
				if err != nil {
					return err
				}
				if line == "\r\n" {
					break
				}
			}
			break
		}

		// 读取块数据
		chunk := make([]byte, chunkSize)
		_, err = io.ReadFull(p.reader, chunk)
		if err != nil {
			return err
		}

		body.Write(chunk)

		// 读取 CRLF
		_, err = p.reader.Discard(2) // \r\n
		if err != nil {
			return err
		}
	}

	req.Body = body.Bytes()
	return nil
}
