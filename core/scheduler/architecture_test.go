package scheduler

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This guards the architectural boundary rather than mirroring a particular
// implementation: runtime and API sources cannot reach Redis keys or commands.
func TestRedisImplementationBoundary(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	constructors := map[string]bool{"client.go": true, "worker.go": true, "client_state.go": true, "runtime.go": true}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		if strings.HasPrefix(name, "redis_") {
			t.Errorf("Redis implementation remains in public package: %s", name)
		}
		f, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			if strings.Contains(imp.Path.Value, "internal/redisproto") {
				t.Errorf("old protocol package in %s", name)
			}
			if strings.Contains(imp.Path.Value, "github.com/redis/go-redis") && !constructors[name] {
				t.Errorf("Redis client dependency outside composition boundary: %s", name)
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok {
				switch sel.Sel.Name {
				case "Keys", "HGet", "HSet", "HGetAll", "XReadGroup", "XAutoClaim", "XAckDel", "XNack", "Pipeline", "Eval", "EvalSha", "NewNamespaceKeys", "Do", "NewScript":
					t.Errorf("Redis detail %s leaked into %s", sel.Sel.Name, name)
				}
			}
			return true
		})
	}
	for _, name := range []string{"client.go", "task.go", "worker.go", "registry.go", "jobs.go", "schedules.go", "lifecycle.go", "heartbeat.go", "dispatch.go", "recovery.go", "maintenance.go", "internal/store/jobs.go", "internal/store/execution.go", "internal/store/schedules.go", "internal/store/keys.go", "internal/store/decode.go", "internal/store/lua/claim.lua"} {
		if _, err := os.Stat(filepath.FromSlash(name)); err != nil {
			t.Error(err)
		}
	}
}
