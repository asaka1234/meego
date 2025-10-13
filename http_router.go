package meego

import "strings"

// Route 路由信息
type Route struct {
	method     string
	path       string
	handler    HandlerFunc
	segments   []string // 路径分段
	paramNames []string // 参数名
}

// Router 实现
type Router struct {
	routes map[string][]*Route // method -> routes
	//routes map[string]map[string]HandlerFunc // method -> path -> handler
}

func NewRouter() *Router {
	return &Router{
		routes: make(map[string][]*Route),
	}
}

// AddRoute 添加路由，支持路径参数
func (r *Router) AddRoute(method, path string, handler HandlerFunc) {
	route := &Route{
		method:  method,
		path:    path,
		handler: handler,
	}

	// 解析路径参数
	route.parsePath()

	r.routes[method] = append(r.routes[method], route)
}

// FindRoute 查找路由并解析参数
func (r *Router) FindRoute(method, path string) (HandlerFunc, map[string]string) {
	routes, exists := r.routes[method]
	if !exists {
		return nil, nil
	}

	pathSegments := splitPath(path)

	for _, route := range routes {
		if params := route.match(pathSegments); params != nil {
			return route.handler, params
		}
	}

	return nil, nil
}

// parsePath 解析路径，提取参数名
func (r *Route) parsePath() {
	path := strings.Trim(r.path, "/")
	if path == "" {
		r.segments = []string{""}
		return
	}

	segments := strings.Split(path, "/")
	r.segments = make([]string, len(segments))
	r.paramNames = make([]string, 0)

	for i, segment := range segments {
		if strings.HasPrefix(segment, ":") {
			// 参数段，如 :id
			paramName := strings.TrimPrefix(segment, ":")
			r.segments[i] = ":" // 标记为参数段
			r.paramNames = append(r.paramNames, paramName)
		} else {
			// 固定段
			r.segments[i] = segment
		}
	}
}

// match 匹配路径并提取参数
func (r *Route) match(pathSegments []string) map[string]string {
	if len(r.segments) != len(pathSegments) {
		return nil
	}

	params := make(map[string]string)
	paramIndex := 0

	for i, routeSeg := range r.segments {
		pathSeg := pathSegments[i]

		if routeSeg == ":" {
			// 参数段，提取参数值
			if paramIndex < len(r.paramNames) {
				paramName := r.paramNames[paramIndex]
				params[paramName] = pathSeg
				paramIndex++
			}
		} else if routeSeg != pathSeg {
			// 固定段不匹配
			return nil
		}
	}

	return params
}

// splitPath 分割路径
func splitPath(path string) []string {
	path = strings.Trim(path, "/")
	if path == "" {
		return []string{""}
	}
	return strings.Split(path, "/")
}
