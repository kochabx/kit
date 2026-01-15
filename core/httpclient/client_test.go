package httpclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type TestResponse struct {
	Message string `json:"message"`
	Status  int    `json:"status"`
}

func TestClient_Request_GET(t *testing.T) {
	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != MethodGet {
			t.Errorf("Expected GET method, got %s", r.Method)
		}

		response := TestResponse{
			Message: "success",
			Status:  200,
		}

		w.Header().Set("Content-Type", ContentTypeJSON)
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Test GET request
	client := New()
	var result TestResponse

	resp, err := client.Request(MethodGet, server.URL, nil, WithResponse(&result))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	if resp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	if result.Message != "success" {
		t.Errorf("Expected message 'success', got '%s'", result.Message)
	}
}

func TestClient_Request_POST_JSON(t *testing.T) {
	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != MethodPost {
			t.Errorf("Expected POST method, got %s", r.Method)
		}

		if r.Header.Get("Content-Type") != ContentTypeJSON {
			t.Errorf("Expected Content-Type %s, got %s", ContentTypeJSON, r.Header.Get("Content-Type"))
		}

		var reqBody TestResponse
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Errorf("Failed to decode request body: %v", err)
		}

		response := TestResponse{
			Message: "received: " + reqBody.Message,
			Status:  201,
		}

		w.WriteHeader(201)
		w.Header().Set("Content-Type", ContentTypeJSON)
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Test POST request with JSON body
	client := New()
	requestBody := TestResponse{
		Message: "test message",
		Status:  100,
	}

	var result TestResponse
	resp, err := client.Request(MethodPost, server.URL, requestBody, WithResponse(&result))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	if resp.StatusCode != 201 {
		t.Errorf("Expected status 201, got %d", resp.StatusCode)
	}

	expectedMessage := "received: test message"
	if result.Message != expectedMessage {
		t.Errorf("Expected message '%s', got '%s'", expectedMessage, result.Message)
	}
}

func TestClient_Request_WithContext(t *testing.T) {
	// Create test server with delay
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		json.NewEncoder(w).Encode(TestResponse{Message: "delayed", Status: 200})
	}))
	defer server.Close()

	// Test with context timeout
	client := New()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	var result TestResponse
	_, err := client.Request(MethodGet, server.URL, nil,
		WithContext(ctx),
		WithResponse(&result),
	)

	if err == nil {
		t.Error("Expected timeout error, got nil")
	}
}

func TestClient_Request_WithHeaders(t *testing.T) {
	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Custom-Header") != "custom-value" {
			t.Errorf("Expected custom header 'custom-value', got '%s'", r.Header.Get("X-Custom-Header"))
		}

		json.NewEncoder(w).Encode(TestResponse{Message: "headers received", Status: 200})
	}))
	defer server.Close()

	// Test with custom headers
	client := New()
	headers := map[string]string{
		"X-Custom-Header": "custom-value",
	}

	var result TestResponse
	_, err := client.Request(MethodGet, server.URL, nil,
		WithHeader(headers),
		WithResponse(&result),
	)

	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	if result.Message != "headers received" {
		t.Errorf("Expected message 'headers received', got '%s'", result.Message)
	}
}

func TestClient_Request_ErrorResponse(t *testing.T) {
	// Create test server that returns error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		w.Write([]byte("Bad Request"))
	}))
	defer server.Close()

	// Test error handling
	client := New()
	_, err := client.Request(MethodGet, server.URL, nil)

	if err == nil {
		t.Error("Expected error for 400 status, got nil")
	}
}

func BenchmarkClient_Request(b *testing.B) {
	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(TestResponse{Message: "benchmark", Status: 200})
	}))
	defer server.Close()

	client := New()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			var result TestResponse
			_, err := client.Request(MethodGet, server.URL, nil, WithResponse(&result))
			if err != nil {
				b.Fatalf("Request failed: %v", err)
			}
		}
	})
}

func TestClient_ConvenienceMethods(t *testing.T) {
	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := map[string]string{
			"method": r.Method,
			"path":   r.URL.Path,
		}

		w.Header().Set("Content-Type", ContentTypeJSON)
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	request := New()

	// Test GET convenience method
	t.Run("GET", func(t *testing.T) {
		var result map[string]string
		resp, err := request.Get(server.URL+"/test", WithResponse(&result))
		if err != nil {
			t.Fatalf("GET request failed: %v", err)
		}
		if resp.StatusCode != 200 {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}
		if result["method"] != "GET" {
			t.Errorf("Expected method GET, got %s", result["method"])
		}
	})

	// Test POST convenience method
	t.Run("POST", func(t *testing.T) {
		var result map[string]string
		body := map[string]string{"test": "data"}
		resp, err := request.Post(server.URL+"/test", body, WithResponse(&result))
		if err != nil {
			t.Fatalf("POST request failed: %v", err)
		}
		if resp.StatusCode != 200 {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}
		if result["method"] != "POST" {
			t.Errorf("Expected method POST, got %s", result["method"])
		}
	})

	// Test PUT convenience method
	t.Run("PUT", func(t *testing.T) {
		var result map[string]string
		body := map[string]string{"test": "data"}
		resp, err := request.Put(server.URL+"/test", body, WithResponse(&result))
		if err != nil {
			t.Fatalf("PUT request failed: %v", err)
		}
		if resp.StatusCode != 200 {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}
		if result["method"] != "PUT" {
			t.Errorf("Expected method PUT, got %s", result["method"])
		}
	})

	// Test DELETE convenience method
	t.Run("DELETE", func(t *testing.T) {
		var result map[string]string
		resp, err := request.Delete(server.URL+"/test", WithResponse(&result))
		if err != nil {
			t.Fatalf("DELETE request failed: %v", err)
		}
		if resp.StatusCode != 200 {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}
		if result["method"] != "DELETE" {
			t.Errorf("Expected method DELETE, got %s", result["method"])
		}
	})

	// Test PATCH convenience method
	t.Run("PATCH", func(t *testing.T) {
		var result map[string]string
		body := map[string]string{"test": "data"}
		resp, err := request.Patch(server.URL+"/test", body, WithResponse(&result))
		if err != nil {
			t.Fatalf("PATCH request failed: %v", err)
		}
		if resp.StatusCode != 200 {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}
		if result["method"] != "PATCH" {
			t.Errorf("Expected method PATCH, got %s", result["method"])
		}
	})
}
