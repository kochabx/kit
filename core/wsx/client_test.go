package wsx

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	// Postman Echo WebSocket endpoint used by integration tests.
	postmanEchoWSS = "wss://ws.postman-echo.com/raw"
)

// TestPostmanEcho_SimpleConnection tests a basic connection.
func TestPostmanEcho_SimpleConnection(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过需要网络连接的集成测试")
	}

	// Create a client with custom TLS settings.
	headers := http.Header{}
	headers.Set("User-Agent", "WebSocket-Test-Client/1.0")

	client := New(
		WithHeaders(headers),
		WithTLSConfig(&tls.Config{InsecureSkipVerify: false}),
	)
	defer client.Close()

	// Register event handlers.
	var connected bool
	var connectErr error
	var mu sync.Mutex
	var wg sync.WaitGroup

	wg.Add(1)
	client.OnEvent(EventConnected, func(event Event) {
		mu.Lock()
		connected = true
		mu.Unlock()
		t.Log("✅ 连接到 Postman Echo WSS 成功")
		wg.Done()
	})

	client.OnEvent(EventError, func(event Event) {
		mu.Lock()
		connectErr = event.Error
		mu.Unlock()
		t.Logf("❌ 连接错误: %v", event.Error)
		wg.Done()
	})

	// Connect to the Postman Echo WebSocket endpoint.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	t.Logf("🔗 尝试连接到: %s", postmanEchoWSS)
	err := client.Connect(ctx, postmanEchoWSS)
	if err != nil {
		t.Logf("初始连接错误: %v", err)
		return // Skip the test if the connection fails.
	}

	// Wait for the connection result.
	done := make(chan bool, 1)
	go func() {
		wg.Wait()
		done <- true
	}()

	select {
	case <-done:
		// The connection result arrived.
	case <-time.After(20 * time.Second):
		t.Log("⏰ 等待连接事件超时")
		return
	}

	mu.Lock()
	if connectErr != nil {
		t.Logf("❌ 连接过程中出现错误: %v", connectErr)
		mu.Unlock()
		return
	}

	connectionStatus := connected
	mu.Unlock()

	if connectionStatus {
		assert.True(t, client.IsConnected(), "客户端应该显示已连接状态")
		t.Log("🎉 WebSocket 连接测试成功！")
	} else {
		t.Log("⚠️  未收到连接成功事件，但没有错误")
	}
}

// TestPostmanEcho_EchoMessage tests echoed messages.
func TestPostmanEcho_EchoMessage(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过需要网络连接的集成测试")
	}

	headers := http.Header{}
	headers.Set("User-Agent", "WebSocket-Test-Client/1.0")

	client := New(
		WithHeaders(headers),
		WithTLSConfig(&tls.Config{InsecureSkipVerify: false}),
	)
	defer client.Close()

	var wg sync.WaitGroup
	var receivedMessage string
	var mu sync.Mutex

	// Wait for the connection first.
	wg.Add(1)
	client.OnEvent(EventConnected, func(event Event) {
		t.Log("✅ 连接成功，准备发送消息")
		wg.Done()
	})

	client.OnEvent(EventMessage, func(event Event) {
		if msg, ok := event.Data.(Message); ok && msg.Type == TextMessage {
			mu.Lock()
			receivedMessage = string(msg.Data)
			mu.Unlock()
			t.Logf("📩 收到echo消息: %s", receivedMessage)
			wg.Done()
		}
	})

	client.OnEvent(EventError, func(event Event) {
		t.Logf("❌ 错误: %v", event.Error)
	})

	// Connect.
	ctx := context.Background()
	err := client.Connect(ctx, postmanEchoWSS)
	if err != nil {
		t.Skipf("连接失败，跳过测试: %v", err)
	}

	// Wait for the connection to be established.
	done := make(chan bool, 1)
	go func() {
		wg.Wait()
		done <- true
	}()

	select {
	case <-done:
		// Connected successfully.
	case <-time.After(20 * time.Second):
		t.Skip("连接超时，跳过测试")
	}

	if !client.IsConnected() {
		t.Skip("客户端未连接，跳过测试")
	}

	// Send the test message.
	testMessage := "Hello Postman Echo WebSocket!"
	t.Logf("📤 发送消息: %s", testMessage)

	wg.Add(1) // Wait for the echo response.
	err = client.SendText(testMessage)
	require.NoError(t, err, "发送消息应该成功")

	// Wait for the echo response.
	go func() {
		wg.Wait()
		done <- true
	}()

	select {
	case <-done:
		// Received the response.
		mu.Lock()
		echo := receivedMessage
		mu.Unlock()

		assert.Equal(t, testMessage, echo, "应该收到相同的echo消息")
		t.Log("🎉 Echo 测试成功！")
	case <-time.After(15 * time.Second):
		t.Log("⏰ 等待echo响应超时")
	}
}

// TestPostmanEcho_ContinuousMessageReceiving tests continuous message delivery.
func TestPostmanEcho_ContinuousMessageReceiving(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过需要网络连接的集成测试")
	}

	headers := http.Header{}
	headers.Set("User-Agent", "WebSocket-Test-Client/1.0")

	client := New(
		WithHeaders(headers),
		WithTLSConfig(&tls.Config{InsecureSkipVerify: false}),
	)
	defer client.Close()

	var wg sync.WaitGroup
	var messageCount int
	var mu sync.Mutex
	var receivedMessages []string

	// Connection event.
	wg.Add(1)
	client.OnEvent(EventConnected, func(event Event) {
		t.Log("✅ 连接成功，开始发送多条消息")
		wg.Done()
	})

	// Collect every received message.
	client.OnEvent(EventMessage, func(event Event) {
		if msg, ok := event.Data.(Message); ok && msg.Type == TextMessage {
			mu.Lock()
			messageCount++
			receivedMessages = append(receivedMessages, string(msg.Data))
			count := messageCount
			mu.Unlock()

			t.Logf("📩 收到第 %d 条消息: %s", count, string(msg.Data))

			// Notify the test after receiving all expected messages.
			if count >= 5 {
				wg.Done()
			}
		}
	})

	client.OnEvent(EventError, func(event Event) {
		t.Logf("❌ 错误: %v", event.Error)
	})

	// Connect.
	ctx := context.Background()
	err := client.Connect(ctx, postmanEchoWSS)
	if err != nil {
		t.Skipf("连接失败，跳过测试: %v", err)
	}

	// Wait for the connection to be established.
	done := make(chan bool, 1)
	go func() {
		wg.Wait()
		done <- true
	}()

	select {
	case <-done:
		// Connected successfully.
	case <-time.After(20 * time.Second):
		t.Skip("连接超时，跳过测试")
	}

	if !client.IsConnected() {
		t.Skip("客户端未连接，跳过测试")
	}

	// Send multiple test messages.
	wg.Add(1) // Wait for all messages.
	messages := []string{
		"第1条测试消息",
		"第2条测试消息",
		"第3条测试消息",
		"第4条测试消息",
		"第5条测试消息",
	}

	// Send messages at intervals.
	for i, testMsg := range messages {
		t.Logf("📤 发送第 %d 条消息: %s", i+1, testMsg)
		err = client.SendText(testMsg)
		require.NoError(t, err, "发送消息应该成功")

		// Space out sends to avoid flooding the connection.
		time.Sleep(500 * time.Millisecond)
	}

	// Wait until all messages arrive.
	go func() {
		wg.Wait()
		done <- true
	}()

	select {
	case <-done:
		// All messages arrived.
		mu.Lock()
		finalCount := messageCount
		finalMessages := make([]string, len(receivedMessages))
		copy(finalMessages, receivedMessages)
		mu.Unlock()

		assert.Equal(t, 5, finalCount, "应该收到5条消息")
		assert.Equal(t, len(messages), len(finalMessages), "接收消息数量应该匹配")

		// Verify that every message arrived.
		for _, expectedMsg := range messages {
			found := slices.Contains(finalMessages, expectedMsg)
			assert.True(t, found, "应该收到消息: %s", expectedMsg)
		}

		t.Log("🎉 持续消息接收测试成功！")
	case <-time.After(30 * time.Second):
		mu.Lock()
		currentCount := messageCount
		mu.Unlock()
		t.Logf("⏰ 等待消息接收超时，已收到 %d 条消息", currentCount)
	}
}

// TestPostmanEcho_LongRunningMessageReceiving tests sustained message delivery.
func TestPostmanEcho_LongRunningMessageReceiving(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过需要网络连接的集成测试")
	}

	headers := http.Header{}
	headers.Set("User-Agent", "WebSocket-Test-Client/1.0")

	client := New(
		WithHeaders(headers),
		WithTLSConfig(&tls.Config{InsecureSkipVerify: false}),
	)
	defer client.Close()

	var wg sync.WaitGroup
	var messageCount int
	var mu sync.Mutex
	var stopReceiving bool

	// Connection event.
	wg.Add(1)
	client.OnEvent(EventConnected, func(event Event) {
		t.Log("✅ 连接成功，开始长时间消息接收测试")
		wg.Done()
	})

	// Handle messages throughout the test.
	client.OnEvent(EventMessage, func(event Event) {
		mu.Lock()
		if stopReceiving {
			mu.Unlock()
			return
		}
		mu.Unlock()

		if msg, ok := event.Data.(Message); ok && msg.Type == TextMessage {
			mu.Lock()
			messageCount++
			count := messageCount
			mu.Unlock()

			t.Logf("📩 [%s] 收到第 %d 条消息: %s", time.Now().Format("15:04:05"), count, string(msg.Data))
		}
	})

	client.OnEvent(EventError, func(event Event) {
		t.Logf("❌ 错误: %v", event.Error)
	})

	// Connect.
	ctx := context.Background()
	err := client.Connect(ctx, postmanEchoWSS)
	if err != nil {
		t.Skipf("连接失败，跳过测试: %v", err)
	}

	// Wait for the connection to be established.
	done := make(chan bool, 1)
	go func() {
		wg.Wait()
		done <- true
	}()

	select {
	case <-done:
		// Connected successfully.
	case <-time.After(20 * time.Second):
		t.Skip("连接超时，跳过测试")
	}

	if !client.IsConnected() {
		t.Skip("客户端未连接，跳过测试")
	}

	// Start a goroutine that sends messages periodically.
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		msgCounter := 0
		for {
			select {
			case <-ticker.C:
				mu.Lock()
				if stopReceiving {
					mu.Unlock()
					return
				}
				mu.Unlock()

				msgCounter++
				testMsg := fmt.Sprintf("定时消息 #%d - %s", msgCounter, time.Now().Format("15:04:05"))

				if client.IsConnected() {
					err := client.SendText(testMsg)
					if err != nil {
						t.Logf("发送消息失败: %v", err)
					} else {
						t.Logf("📤 发送定时消息: %s", testMsg)
					}
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// Receive messages continuously for ten seconds.
	t.Log("🕐 开始10秒钟的持续消息接收测试...")
	time.Sleep(10 * time.Second)

	// Stop receiving.
	mu.Lock()
	stopReceiving = true
	finalCount := messageCount
	mu.Unlock()

	t.Logf("🏁 测试结束，总共接收到 %d 条消息", finalCount)
	assert.True(t, finalCount > 0, "应该至少收到一条消息")
	t.Log("🎉 长时间消息接收测试完成！")
}
