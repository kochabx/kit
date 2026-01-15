package ioc

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// MockComponent implements the Component interface for testing
type MockComponent struct {
	name          string
	initCalled    bool
	destroyCalled bool
	order         int
}

func NewMockComponent(name string) *MockComponent {
	return &MockComponent{
		name: name,
	}
}

func (m *MockComponent) Name() string {
	return m.name
}

func (m *MockComponent) Init(ctx context.Context) error {
	m.initCalled = true
	return nil
}

func (m *MockComponent) Destroy(ctx context.Context) error {
	m.destroyCalled = true
	return nil
}

func (m *MockComponent) Order() int {
	return m.order
}

func (m *MockComponent) SetOrder(order int) {
	m.order = order
}

// MockBaseComponent implements BaseComponent interface for testing
type MockBaseComponent struct {
	*MockComponent
}

func NewMockBaseComponent(name string) *MockBaseComponent {
	return &MockBaseComponent{
		MockComponent: NewMockComponent(name),
	}
}

// MockDependencyProvider implements DependencyProvider interface for testing
type MockDependencyProvider struct {
	*MockComponent
	config map[string]any
}

func NewMockDependencyProvider(name string, config map[string]any) *MockDependencyProvider {
	return &MockDependencyProvider{
		MockComponent: NewMockComponent(name),
		config:        config,
	}
}

func (m *MockDependencyProvider) ProvidesDependency() string {
	return "config_provider"
}

func (m *MockDependencyProvider) GetDependency() any {
	return m
}

// MockDependencyConsumer implements DependencyConsumer interface for testing
type MockDependencyConsumer struct {
	*MockComponent
	dependencies []string
	injected     map[string]any
}

func NewMockDependencyConsumer(name string, dependencies []string) *MockDependencyConsumer {
	return &MockDependencyConsumer{
		MockComponent: NewMockComponent(name),
		dependencies:  dependencies,
		injected:      make(map[string]any),
	}
}

func (m *MockDependencyConsumer) RequiredDependencies() []string {
	return m.dependencies
}

func (m *MockDependencyConsumer) SetDependency(typeID string, dependency any) error {
	m.injected[typeID] = dependency
	return nil
}

func (m *MockDependencyConsumer) GetInjected(typeID string) any {
	return m.injected[typeID]
}

// TestStore tests the basic store functionality
func TestStore(t *testing.T) {
	// Create a new application container
	container := NewApplicationContainer(WithApplicationVerbose())

	// Test component registration
	mockConfig := NewMockComponent("mock-config")
	if err := container.RegisterConfig(mockConfig); err != nil {
		t.Fatalf("Failed to register config component: %v", err)
	}

	mockBaseComponent := NewMockBaseComponent("mock-base-component")
	if err := container.RegisterBaseComponent(mockBaseComponent, 10); err != nil {
		t.Fatalf("Failed to register base component: %v", err)
	}

	mockController := NewMockComponent("mock-controller")
	if err := container.RegisterController(mockController); err != nil {
		t.Fatalf("Failed to register controller component: %v", err)
	}

	// Test component retrieval
	retrievedConfig := container.GetConfig("mock-config")
	if retrievedConfig == nil {
		t.Fatal("Failed to retrieve config component")
	}

	retrievedBaseComponent := container.GetBaseComponent("mock-base-component")
	if retrievedBaseComponent == nil {
		t.Fatal("Failed to retrieve base component")
	}

	// Test container initialization
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := container.Initialize(ctx); err != nil {
		t.Fatalf("Failed to initialize container: %v", err)
	}

	// Verify components were initialized
	if !mockConfig.initCalled {
		t.Error("Config component was not initialized")
	}

	if !mockBaseComponent.initCalled {
		t.Error("Base component was not initialized")
	}

	// Test container state
	if state := container.GetState(); state != StateRunning {
		t.Errorf("Expected container state to be running, got %s", state)
	}

	// Test health check
	healthReport := container.HealthCheck(ctx)
	if !healthReport.Healthy {
		t.Errorf("Expected container to be healthy, got: %s", healthReport.Message)
	}

	// Test metrics
	metrics := container.GetMetrics(ctx)
	if metrics.TotalComponents != 3 {
		t.Errorf("Expected 3 components, got %d", metrics.TotalComponents)
	}

	// Test container shutdown
	if err := container.Shutdown(ctx); err != nil {
		t.Fatalf("Failed to shutdown container: %v", err)
	}

	// Verify components were destroyed
	if !mockConfig.destroyCalled {
		t.Error("Config component was not destroyed")
	}

	if !mockBaseComponent.destroyCalled {
		t.Error("Base component was not destroyed")
	}

	// Test final state
	if state := container.GetState(); state != StateStopped {
		t.Errorf("Expected container state to be stopped, got %s", state)
	}

	t.Log("All store tests passed")
}

// TestDependencyInjection tests the dependency injection functionality
func TestDependencyInjection(t *testing.T) {
	// Create container with custom builder
	builder := NewContainerBuilder().
		WithDefaultNamespaces().
		WithShutdownTimeout(10 * time.Second)

	container := NewCustomApplicationContainer(builder)

	// Create a provider component
	provider := NewMockDependencyProvider("config-provider", map[string]any{
		"database_url": "localhost:5432",
		"api_key":      "secret123",
	})

	// Create a consumer component that depends on the provider
	consumerDeps := []string{"config_provider"}
	consumer := NewMockDependencyConsumer("service-consumer", consumerDeps)

	// Register components
	if err := container.Register(ConfigNamespace, provider); err != nil {
		t.Fatalf("Failed to register provider: %v", err)
	}

	if err := container.Register(BaseComponentNamespace, consumer); err != nil {
		t.Fatalf("Failed to register consumer: %v", err)
	}

	// Initialize container (this should resolve dependencies automatically)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := container.Initialize(ctx); err != nil {
		t.Fatalf("Failed to initialize container: %v", err)
	}

	// Verify dependency was injected
	injectedProvider := consumer.GetInjected("config_provider")
	if injectedProvider == nil {
		t.Error("Dependency was not injected")
	}

	// Verify the injected dependency is correct
	if configProvider, ok := injectedProvider.(*MockDependencyProvider); ok {
		config := configProvider.GetDependency()
		if configProvider, ok := config.(*MockDependencyProvider); ok {
			if configProvider.config["database_url"] != "localhost:5432" {
				t.Error("Injected config has incorrect values")
			}
		} else {
			t.Error("Injected config is not the expected type")
		}
	} else {
		t.Error("Injected dependency does not implement DependencyProvider")
	}

	// Cleanup
	if err := container.Shutdown(ctx); err != nil {
		t.Fatalf("Failed to shutdown container: %v", err)
	}

	t.Log("Dependency injection test passed")
}

// TestLifecycleHooks tests the lifecycle hooks functionality
func TestLifecycleHooks(t *testing.T) {
	var beforeInitCalled, afterInitCalled, beforeDestroyCalled, afterDestroyCalled bool

	// Create container with hooks
	builder := NewContainerBuilder().
		WithDefaultNamespaces().
		AddBeforeInitHook(func(ctx context.Context, store *Store) error {
			beforeInitCalled = true
			return nil
		}).
		AddAfterInitHook(func(ctx context.Context, store *Store) error {
			afterInitCalled = true
			return nil
		}).
		AddBeforeDestroyHook(func(ctx context.Context, store *Store) error {
			beforeDestroyCalled = true
			return nil
		}).
		AddAfterDestroyHook(func(ctx context.Context, store *Store) error {
			afterDestroyCalled = true
			return nil
		})

	container := NewCustomApplicationContainer(builder)

	// Register a simple component
	mockComponent := NewMockComponent("test-component")
	if err := container.RegisterBaseComponent(mockComponent); err != nil {
		t.Fatalf("Failed to register component: %v", err)
	}

	// Initialize container
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := container.Initialize(ctx); err != nil {
		t.Fatalf("Failed to initialize container: %v", err)
	}

	// Verify init hooks were called
	if !beforeInitCalled {
		t.Error("Before init hook was not called")
	}
	if !afterInitCalled {
		t.Error("After init hook was not called")
	}

	// Shutdown container
	if err := container.Shutdown(ctx); err != nil {
		t.Fatalf("Failed to shutdown container: %v", err)
	}

	// Verify destroy hooks were called
	if !beforeDestroyCalled {
		t.Error("Before destroy hook was not called")
	}
	if !afterDestroyCalled {
		t.Error("After destroy hook was not called")
	}

	t.Log("Lifecycle hooks test passed")
}

// TestErrorHandling tests error handling scenarios
func TestErrorHandling(t *testing.T) {
	container := NewApplicationContainer()

	// Test registering component with empty name
	emptyNameComponent := &MockComponent{name: ""}
	if err := container.RegisterBaseComponent(emptyNameComponent); err == nil {
		t.Error("Expected error when registering component with empty name")
	}

	// Test registering nil component
	if err := container.RegisterBaseComponent(nil); err == nil {
		t.Error("Expected error when registering nil component")
	}

	// Test duplicate registration
	mockComponent := NewMockComponent("duplicate-test")
	if err := container.RegisterBaseComponent(mockComponent); err != nil {
		t.Fatalf("Failed to register component first time: %v", err)
	}

	if err := container.RegisterBaseComponent(mockComponent); err == nil {
		t.Error("Expected error when registering duplicate component")
	}

	// Test getting non-existent component
	nonExistent := container.GetBaseComponent("non-existent")
	if nonExistent != nil {
		t.Error("Expected nil when getting non-existent component")
	}

	t.Log("Error handling test passed")
}

// BenchmarkContainerOperations benchmarks container operations
func BenchmarkContainerOperations(b *testing.B) {
	container := NewApplicationContainer()

	// Register components
	for i := 0; i < 100; i++ {
		component := NewMockComponent(fmt.Sprintf("component-%d", i))
		container.RegisterBaseComponent(component)
	}

	ctx := context.Background()
	container.Initialize(ctx)

	b.ResetTimer()

	b.Run("Get", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			container.GetBaseComponent("component-50")
		}
	})

	b.Run("HealthCheck", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			container.HealthCheck(ctx)
		}
	})

	b.Run("GetMetrics", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			container.GetMetrics(ctx)
		}
	})

	container.Shutdown(ctx)
}
