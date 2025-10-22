// json_utils.go
package meego

import (
	"fmt"
	"github.com/json-iterator/go"
	"reflect"
)

// BindJSON 绑定 JSON 请求体到结构体
func (c *Context) BindJSON(v interface{}) error {
	if len(c.Request.Body) == 0 {
		return fmt.Errorf("empty request body")
	}

	json := jsoniter.ConfigCompatibleWithStandardLibrary
	if err := json.Unmarshal(c.Request.Body, v); err != nil {
		return fmt.Errorf("invalid JSON: %v", err)
	}

	return nil
}

// ValidateStruct 验证结构体
func ValidateStruct(s interface{}) error {
	val := reflect.ValueOf(s)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}

	if val.Kind() != reflect.Struct {
		return fmt.Errorf("expected struct, got %T", s)
	}

	typ := val.Type()
	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)
		fieldType := typ.Field(i)

		// 检查 required 标签
		if fieldType.Tag.Get("required") == "true" {
			if isZero(field) {
				return fmt.Errorf("field %s is required", fieldType.Name)
			}
		}
	}

	return nil
}

func isZero(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.String:
		return v.String() == ""
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.Bool:
		return !v.Bool()
	case reflect.Ptr, reflect.Interface:
		return v.IsNil()
	case reflect.Slice, reflect.Map, reflect.Array:
		return v.Len() == 0
	default:
		return false
	}
}
