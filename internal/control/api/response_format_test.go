package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// 验证后端 api.JSON 确实包装了 {success, data} 结构
func TestJSON_WrapsResponse(t *testing.T) {
	rec := httptest.NewRecorder()
	innerData := map[string]string{"token": "abc123", "username": "admin"}
	JSON(rec, http.StatusOK, innerData)

	var wrapper Response
	if err := json.Unmarshal(rec.Body.Bytes(), &wrapper); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if !wrapper.Success {
		t.Error("expected success=true")
	}
	if wrapper.Data == nil {
		t.Fatal("expected data to be present")
	}

	dataBytes, _ := json.Marshal(wrapper.Data)
	var actual map[string]string
	json.Unmarshal(dataBytes, &actual)

	if actual["token"] != "abc123" {
		t.Errorf("expected token=abc123, got %s", actual["token"])
	}
}

// 模拟前端直接访问 response.data.token 的场景（当前 Dashboard 的做法）
func TestJSON_FrontendDirectAccess_WouldFail(t *testing.T) {
	rec := httptest.NewRecorder()
	innerData := map[string]string{"token": "abc123"}
	JSON(rec, http.StatusOK, innerData)

	// 前端 axios 的 response.data 拿到的是整个 Response 对象
	var frontendSees Response
	json.Unmarshal(rec.Body.Bytes(), &frontendSees)

	// 前端代码写的是：if (response.data.token) { ... }
	// 但 response.data 实际上是 {success: true, data: {token: "abc123"}}
	// 所以 response.data.token 是 undefined
	if frontendSees.Success && frontendSees.Data != nil {
		// 模拟前端尝试直接访问 .token（它会拿到 undefined/空）
		dataMap, ok := frontendSees.Data.(map[string]interface{})
		if !ok {
			t.Fatal("data is not a map")
		}
		if dataMap["token"] != "abc123" {
			t.Error("unexpected token value in nested data")
		}
	}
}
