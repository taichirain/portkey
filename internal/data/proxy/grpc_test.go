package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"

	pluginPkg "github.com/taichirain/portkey/internal/data/plugin"
	"go.uber.org/zap"
	"google.golang.org/grpc/metadata"
)

func TestGRPCProxy_IsGRPCRequest(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	registry := newTestPluginRegistry(t)
	builder := newTestPluginChainBuilder(t, registry)
	proxy := NewGRPCProxy(logger, registry, builder)

	tests := []struct {
		name           string
		contentType    string
		expectedResult bool
	}{
		{
			name:           "标准 gRPC Content-Type",
			contentType:    "application/grpc",
			expectedResult: true,
		},
		{
			name:           "gRPC + Proto Content-Type",
			contentType:    "application/grpc+proto",
			expectedResult: true,
		},
		{
			name:           "gRPC + JSON Content-Type",
			contentType:    "application/grpc+json",
			expectedResult: true,
		},
		{
			name:           "普通 HTTP Content-Type",
			contentType:    "application/json",
			expectedResult: false,
		},
		{
			name:           "空 Content-Type",
			contentType:    "",
			expectedResult: false,
		},
		{
			name:           "类似但不匹配的 Content-Type",
			contentType:    "application/grpc-web",
			expectedResult: false,
		},
		{
			name:           "类似但不匹配的 Content-Type 2",
			contentType:    "application/grpc+",
			expectedResult: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/grpc/service/method", nil)
			if tt.contentType != "" {
				req.Header.Set("Content-Type", tt.contentType)
			}

			result := proxy.IsGRPCRequest(req)
			if result != tt.expectedResult {
				t.Errorf("IsGRPCRequest() 期望 %v，实际 %v", tt.expectedResult, result)
			}
		})
	}
}

func TestGRPCProxy_extractGRPCMetadata(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	registry := newTestPluginRegistry(t)
	builder := newTestPluginChainBuilder(t, registry)
	proxy := NewGRPCProxy(logger, registry, builder)

	tests := []struct {
		name           string
		headers        http.Header
		expectedKeys   []string
		excludedKeys   []string
	}{
		{
			name: "标准 HTTP 头转元数据",
			headers: http.Header{
				"X-Custom-Header": []string{"value1"},
				"Authorization":   []string{"Bearer token"},
			},
			expectedKeys: []string{"x-custom-header", "authorization"},
			excludedKeys: []string{},
		},
		{
			name: "排除 Content-Type 和 Content-Length",
			headers: http.Header{
				"Content-Type":   []string{"application/grpc"},
				"Content-Length": []string{"100"},
				"X-User-ID":      []string{"123"},
			},
			expectedKeys: []string{"x-user-id"},
			excludedKeys: []string{"content-type", "content-length"},
		},
		{
			name: "排除 grpc- 前缀的头",
			headers: http.Header{
				"grpc-encoding":     []string{"gzip"},
				"grpc-accept-encoding": []string{"gzip, deflate"},
				"X-Request-ID":      []string{"req-123"},
			},
			expectedKeys: []string{"x-request-id"},
			excludedKeys: []string{"grpc-encoding", "grpc-accept-encoding"},
		},
		{
			name: "排除冒号开头的头（HTTP/2 伪头）",
			headers: http.Header{
				":method": []string{"POST"},
				":path":   []string{"/grpc/service"},
				"X-Trace": []string{"trace-123"},
			},
			expectedKeys: []string{"x-trace"},
			excludedKeys: []string{":method", ":path"},
		},
		{
			name: "大写头名转小写",
			headers: http.Header{
				"X-Camel-Case": []string{"value"},
				"ALLCAPS":      []string{"value"},
			},
			expectedKeys: []string{"x-camel-case", "allcaps"},
			excludedKeys: []string{},
		},
		{
			name: "多值头",
			headers: http.Header{
				"X-Multi-Value": []string{"value1", "value2", "value3"},
			},
			expectedKeys: []string{"x-multi-value"},
			excludedKeys: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/grpc/service/method", nil)
			for key, values := range tt.headers {
				for _, value := range values {
					req.Header.Add(key, value)
				}
			}

			md := proxy.extractGRPCMetadata(req)

			for _, expectedKey := range tt.expectedKeys {
				if _, exists := md[expectedKey]; !exists {
					t.Errorf("期望元数据包含键 '%s'，但未找到", expectedKey)
				}
			}

			for _, excludedKey := range tt.excludedKeys {
				if _, exists := md[excludedKey]; exists {
					t.Errorf("期望元数据不包含键 '%s'，但找到了", excludedKey)
				}
			}
		})
	}
}

func TestGRPCProxy_encodeUint32(t *testing.T) {
	tests := []struct {
		name     string
		value    uint32
		expected []byte
	}{
		{
			name:     "零值",
			value:    0,
			expected: []byte{0x00, 0x00, 0x00, 0x00},
		},
		{
			name:     "小值",
			value:    1,
			expected: []byte{0x00, 0x00, 0x00, 0x01},
		},
		{
			name:     "中等值",
			value:    256,
			expected: []byte{0x00, 0x00, 0x01, 0x00},
		},
		{
			name:     "大端序验证",
			value:    0x12345678,
			expected: []byte{0x12, 0x34, 0x56, 0x78},
		},
		{
			name:     "最大值",
			value:    0xFFFFFFFF,
			expected: []byte{0xFF, 0xFF, 0xFF, 0xFF},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := encodeUint32(tt.value)
			if len(result) != 4 {
				t.Errorf("encodeUint32() 期望返回 4 字节，实际 %d 字节", len(result))
			}
			for i := range tt.expected {
				if result[i] != tt.expected[i] {
					t.Errorf("encodeUint32() 字节 %d 期望 0x%02X，实际 0x%02X", i, tt.expected[i], result[i])
				}
			}
		})
	}
}

func TestGRPCProxy_rawCodec(t *testing.T) {
	codec := &rawCodec{}

	t.Run("Marshal 成功", func(t *testing.T) {
		data := []byte{0x01, 0x02, 0x03, 0x04}
		result, err := codec.Marshal(data)
		if err != nil {
			t.Errorf("rawCodec.Marshal() 期望无错误，实际 %v", err)
		}
		if len(result) != len(data) {
			t.Errorf("rawCodec.Marshal() 期望 %d 字节，实际 %d 字节", len(data), len(result))
		}
		for i := range data {
			if result[i] != data[i] {
				t.Errorf("rawCodec.Marshal() 字节 %d 期望 0x%02X，实际 0x%02X", i, data[i], result[i])
			}
		}
	})

	t.Run("Marshal 失败 - 非 []byte 类型", func(t *testing.T) {
		_, err := codec.Marshal("not bytes")
		if err == nil {
			t.Error("rawCodec.Marshal() 期望对非 []byte 类型返回错误")
		}
	})

	t.Run("Unmarshal 成功", func(t *testing.T) {
		data := []byte{0x01, 0x02, 0x03, 0x04}
		var result []byte
		err := codec.Unmarshal(data, &result)
		if err != nil {
			t.Errorf("rawCodec.Unmarshal() 期望无错误，实际 %v", err)
		}
		if len(result) != len(data) {
			t.Errorf("rawCodec.Unmarshal() 期望 %d 字节，实际 %d 字节", len(data), len(result))
		}
		for i := range data {
			if result[i] != data[i] {
				t.Errorf("rawCodec.Unmarshal() 字节 %d 期望 0x%02X，实际 0x%02X", i, data[i], result[i])
			}
		}
	})

	t.Run("Unmarshal 失败 - 非 *[]byte 类型", func(t *testing.T) {
		data := []byte{0x01, 0x02, 0x03, 0x04}
		var result string
		err := codec.Unmarshal(data, &result)
		if err == nil {
			t.Error("rawCodec.Unmarshal() 期望对非 *[]byte 类型返回错误")
		}
	})

	t.Run("Name 方法", func(t *testing.T) {
		name := codec.Name()
		if name != "raw" {
			t.Errorf("rawCodec.Name() 期望 'raw'，实际 '%s'", name)
		}
	})
}

func TestGRPCProxy_Initialization(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	registry := newTestPluginRegistry(t)
	builder := newTestPluginChainBuilder(t, registry)
	proxy := NewGRPCProxy(logger, registry, builder)

	if proxy == nil {
		t.Error("期望 NewGRPCProxy 返回非空值")
	}
}

func TestGRPCProxy_UpdateSnapshot(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	registry := newTestPluginRegistry(t)
	builder := newTestPluginChainBuilder(t, registry)
	proxy := NewGRPCProxy(logger, registry, builder)

	req := httptest.NewRequest("POST", "/grpc/service/method", nil)
	req.Header.Set("Content-Type", "application/grpc")

	result := proxy.IsGRPCRequest(req)
	if !result {
		t.Error("期望 IsGRPCRequest 返回 true")
	}
}

func TestGRPCProxy_extractGRPCMetadata_Empty(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	registry := newTestPluginRegistry(t)
	builder := newTestPluginChainBuilder(t, registry)
	proxy := NewGRPCProxy(logger, registry, builder)

	req := httptest.NewRequest("POST", "/grpc/service/method", nil)
	req.Header.Set("Content-Type", "application/grpc")

	md := proxy.extractGRPCMetadata(req)
	if len(md) != 0 {
		t.Errorf("期望空元数据，实际有 %d 个键", len(md))
	}
}

func TestGRPCProxy_extractGRPCMetadata_MultiValue(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	registry := newTestPluginRegistry(t)
	builder := newTestPluginChainBuilder(t, registry)
	proxy := NewGRPCProxy(logger, registry, builder)

	req := httptest.NewRequest("POST", "/grpc/service/method", nil)
	req.Header.Set("Content-Type", "application/grpc")
	req.Header.Add("X-Multi-Value", "value1")
	req.Header.Add("X-Multi-Value", "value2")
	req.Header.Add("X-Multi-Value", "value3")

	md := proxy.extractGRPCMetadata(req)
	values := md.Get("x-multi-value")
	if len(values) != 3 {
		t.Errorf("期望 3 个值，实际 %d 个", len(values))
	}
	expectedValues := []string{"value1", "value2", "value3"}
	for i, expected := range expectedValues {
		if values[i] != expected {
			t.Errorf("值 %d 期望 '%s'，实际 '%s'", i, expected, values[i])
		}
	}
}

func TestGRPCProxy_metadata_MD(t *testing.T) {
	md := metadata.MD{}
	md.Set("key1", "value1")
	md.Append("key2", "value2a")
	md.Append("key2", "value2b")

	if len(md) != 2 {
		t.Errorf("期望 2 个键，实际 %d 个", len(md))
	}

	key1Values := md.Get("key1")
	if len(key1Values) != 1 || key1Values[0] != "value1" {
		t.Errorf("key1 期望 ['value1']，实际 %v", key1Values)
	}

	key2Values := md.Get("key2")
	if len(key2Values) != 2 || key2Values[0] != "value2a" || key2Values[1] != "value2b" {
		t.Errorf("key2 期望 ['value2a', 'value2b']，实际 %v", key2Values)
	}
}

func TestGRPCProxy_extractGRPCMetadata_FullLowerCase(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	registry := pluginPkg.NewPluginRegistry()
	builder := pluginPkg.NewPluginChainBuilder(registry)
	proxy := NewGRPCProxy(logger, registry, builder)

	tests := []struct {
		name           string
		originalKey    string
		expectedKey    string
	}{
		{
			name:        "X-Custom-Header 完全小写",
			originalKey: "X-Custom-Header",
			expectedKey: "x-custom-header",
		},
		{
			name:        "ALL-CAPS-HEADER 完全小写",
			originalKey: "ALL-CAPS-HEADER",
			expectedKey: "all-caps-header",
		},
		{
			name:        "x-already-lower 保持不变",
			originalKey: "x-already-lower",
			expectedKey: "x-already-lower",
		},
		{
			name:        "MixEd-CaSe 完全小写",
			originalKey: "MixEd-CaSe",
			expectedKey: "mixed-case",
		},
		{
			name:        "X-Request-ID 完全小写",
			originalKey: "X-Request-ID",
			expectedKey: "x-request-id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/grpc/service/method", nil)
			req.Header.Set(tt.originalKey, "test-value")

			md := proxy.extractGRPCMetadata(req)

			if _, exists := md[tt.expectedKey]; !exists {
				t.Errorf("期望元数据包含键 '%s'，但未找到。元数据中的键: %v", tt.expectedKey, md)
			}

			if tt.originalKey != tt.expectedKey {
				if _, exists := md[tt.originalKey]; exists {
					t.Errorf("期望元数据不包含原始键 '%s'，但找到了", tt.originalKey)
				}

				partialLowerKey := string(tt.originalKey[0]+32) + tt.originalKey[1:]
				if _, exists := md[partialLowerKey]; exists && partialLowerKey != tt.expectedKey {
					t.Errorf("期望元数据不包含部分小写键 '%s'，但找到了。应该是 '%s'", partialLowerKey, tt.expectedKey)
				}
			}
		})
	}
}

func TestGRPCProxy_EmptyPath_NoPanic(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	registry := pluginPkg.NewPluginRegistry()
	builder := pluginPkg.NewPluginChainBuilder(registry)
	proxy := NewGRPCProxy(logger, registry, builder)

	req := httptest.NewRequest("POST", "/", nil)
	req.Header.Set("Content-Type", "application/grpc")

	result := proxy.IsGRPCRequest(req)
	if !result {
		t.Error("期望 IsGRPCRequest 返回 true")
	}

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("路径为空时不应该 panic，但发生了: %v", r)
		}
	}()

	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{
			name:     "空路径",
			path:     "",
			expected: "",
		},
		{
			name:     "只有斜杠的路径",
			path:     "/",
			expected: "",
		},
		{
			name:     "标准路径",
			path:     "/grpc/service/method",
			expected: "grpc/service/method",
		},
		{
			name:     "无前导斜杠的路径",
			path:     "grpc/service/method",
			expected: "grpc/service/method",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fullMethodName := tt.path
			if len(fullMethodName) > 0 && fullMethodName[0] == '/' {
				fullMethodName = fullMethodName[1:]
			}
			if fullMethodName != tt.expected {
				t.Errorf("期望 '%s'，实际 '%s'", tt.expected, fullMethodName)
			}
		})
	}
}
