// http_router.go
package meego

import (
	"strings"
	"sync"
)

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
	mu     sync.RWMutex
	routes map[string][]*Route // method -> routes

	// 缓存优化 - 使用独立的锁
	cacheMu    sync.RWMutex
	routeCache map[string]struct {
		handler HandlerFunc
		params  map[string]string
	}
	cacheSize int
}

func NewRouter() *Router {
	return &Router{
		routes: make(map[string][]*Route),
		routeCache: make(map[string]struct {
			handler HandlerFunc
			params  map[string]string
		}, 1024),
		cacheSize: 1024,
	}
}

// AddRoute 添加路由，支持路径参数
func (r *Router) AddRoute(method, path string, handler HandlerFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()

	route := &Route{
		method:  method,
		path:    path,
		handler: handler,
	}

	// 解析路径参数
	route.parsePath()

	r.routes[method] = append(r.routes[method], route)

	// 清空缓存 - 使用独立的锁
	r.clearCache()
}

// FindRoute 查找路由并解析参数 - 优化版本
func (r *Router) FindRoute(method, path string) (HandlerFunc, map[string]string) {
	// 首先尝试缓存
	cacheKey := method + ":" + path
	if result, found := r.getFromCache(cacheKey); found {
		return result.handler, result.params
	}

	r.mu.RLock()
	routes, exists := r.routes[method]
	r.mu.RUnlock()

	if !exists {
		return nil, nil
	}

	pathSegments := splitPathFast(path)

	for _, route := range routes {
		if params := route.matchFast(pathSegments); params != nil {
			// 缓存结果
			r.putToCache(cacheKey, route.handler, params)
			return route.handler, params
		}
	}

	return nil, nil
}

// 缓存操作 - 使用独立的锁
func (r *Router) getFromCache(key string) (struct {
	handler HandlerFunc
	params  map[string]string
}, bool) {
	r.cacheMu.RLock()
	defer r.cacheMu.RUnlock()
	result, exists := r.routeCache[key]
	return result, exists
}

func (r *Router) putToCache(key string, handler HandlerFunc, params map[string]string) {
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()

	// 简单的缓存淘汰策略
	if len(r.routeCache) >= r.cacheSize {
		// 清空缓存 - 这里不需要获取锁，因为已经在写锁保护中
		r.routeCache = make(map[string]struct {
			handler HandlerFunc
			params  map[string]string
		}, r.cacheSize)
	}

	r.routeCache[key] = struct {
		handler HandlerFunc
		params  map[string]string
	}{handler: handler, params: params}
}

func (r *Router) clearCache() {
	// 使用独立的缓存锁，避免死锁
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()

	// 重用现有的 map 而不是创建新的，避免 GC 压力
	if len(r.routeCache) > 0 {
		for k := range r.routeCache {
			delete(r.routeCache, k)
		}
	}
}

// parsePath 解析路径，提取参数名 - 优化版本
func (r *Route) parsePath() {
	path := strings.Trim(r.path, "/")
	if path == "" {
		r.segments = []string{""}
		return
	}

	segments := splitPathFast(path)
	r.segments = make([]string, len(segments))
	r.paramNames = make([]string, 0, 2)

	for i, segment := range segments {
		if len(segment) > 1 && segment[0] == ':' {
			paramName := segment[1:]
			r.segments[i] = ":"
			r.paramNames = append(r.paramNames, paramName)
		} else {
			r.segments[i] = segment
		}
	}
}

// matchFast 快速匹配路径并提取参数
func (r *Route) matchFast(pathSegments []string) map[string]string {
	if len(r.segments) != len(pathSegments) {
		return nil
	}

	params := make(map[string]string, len(r.paramNames))
	paramIndex := 0

	for i, routeSeg := range r.segments {
		pathSeg := pathSegments[i]

		if routeSeg == ":" {
			if paramIndex < len(r.paramNames) {
				paramName := r.paramNames[paramIndex]
				params[paramName] = pathSeg
				paramIndex++
			} else {
				return nil
			}
		} else if routeSeg != pathSeg {
			return nil
		}
	}

	if paramIndex != len(r.paramNames) {
		return nil
	}

	return params
}

// splitPathFast 快速分割路径
func splitPathFast(path string) []string {
	if path == "" || path == "/" {
		return []string{""}
	}

	start, end := 0, len(path)
	if path[0] == '/' {
		start = 1
	}
	if path[end-1] == '/' {
		end = end - 1
	}

	if start >= end {
		return []string{""}
	}

	path = path[start:end]

	segmentCount := 1
	for i := 0; i < len(path); i++ {
		if path[i] == '/' {
			segmentCount++
		}
	}

	segments := make([]string, segmentCount)
	current := 0
	last := 0

	for i := 0; i < len(path); i++ {
		if path[i] == '/' {
			segments[current] = path[last:i]
			current++
			last = i + 1
		}
	}

	if last < len(path) {
		segments[current] = path[last:]
	}

	return segments
}

// 批量添加路由方法
func (r *Router) AddRoutes(method string, routes map[string]HandlerFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for path, handler := range routes {
		route := &Route{
			method:  method,
			path:    path,
			handler: handler,
		}
		route.parsePath()
		r.routes[method] = append(r.routes[method], route)
	}

	// 清空缓存
	r.clearCache()
}

// 获取所有路由（用于调试）
func (r *Router) GetRoutes() map[string][]string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string][]string)
	for method, routes := range r.routes {
		paths := make([]string, len(routes))
		for i, route := range routes {
			paths[i] = route.path
		}
		result[method] = paths
	}
	return result
}
