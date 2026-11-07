package scheduler

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// Runs only when explicitly selected by the parent process test.
func TestNodeFailureWorkerHelper(t *testing.T) {
	mode := os.Getenv("SCHEDULER_TEST_NODE_MODE")
	if mode == "" {
		return
	}
	client := redis.NewClient(&redis.Options{ContextTimeoutEnabled: true, Addr: os.Getenv("REDIS_ADDR"), Password: os.Getenv("REDIS_PASSWORD")})
	defer client.Close()
	s, err := NewWorker(client, WorkerConfig{Namespace: os.Getenv("SCHEDULER_TEST_NODE_NAMESPACE"), Queues: []QueueConfig{{Name: DefaultQueue, Concurrency: 1}}, Execution: ExecutionConfig{PollTimeout: 20 * time.Millisecond, LeaseDuration: 3 * time.Second, LeaseRenewInterval: 100 * time.Millisecond}, Retention: RetentionConfig{Succeeded: time.Minute}})
	if err != nil {
		t.Fatal(err)
	}
	task, err := NewTask[int]("node.failure", WithTimeout(30*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Register(task, func(ctx context.Context, _ int) error {
		fmt.Println("NODE_STARTED")
		if mode == "hold" {
			<-ctx.Done()
			return ctx.Err()
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := s.Run(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestKilledNodeTaskRecoversOnReplacement(t *testing.T) {
	client := testRedis(t)
	namespace := fmt.Sprintf("killed-node-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := NewClient(client, ClientConfig{Namespace: namespace})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	task, err := NewTask[int]("node.failure", WithTimeout(30*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	job, err := s.Enqueue(context.Background(), task, 1)
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	start := func(mode string) (*exec.Cmd, <-chan struct{}) {
		command := exec.Command(executable, "-test.run=^TestNodeFailureWorkerHelper$", "-test.timeout=20s")
		command.Env = append(os.Environ(), "REDIS_ADDR="+client.Options().Addr, "REDIS_PASSWORD="+client.Options().Password, "SCHEDULER_TEST_NODE_NAMESPACE="+namespace, "SCHEDULER_TEST_NODE_MODE="+mode)
		command.Stderr = io.Discard
		output, err := command.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		started := make(chan struct{}, 1)
		go func() {
			scanner := bufio.NewScanner(output)
			for scanner.Scan() {
				if scanner.Text() == "NODE_STARTED" {
					started <- struct{}{}
				}
			}
		}()
		return command, started
	}
	old, started := start("hold")
	oldWaited := false
	defer func() {
		if !oldWaited {
			_ = old.Process.Kill()
			_ = old.Wait()
		}
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("original worker did not start")
	}
	if err := old.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = old.Wait()
	oldWaited = true
	replacement, _ := start("finish")
	defer func() { _ = replacement.Process.Signal(os.Interrupt); _ = replacement.Wait() }()
	eventually(t, 8*time.Second, func() bool {
		stored, err := s.Jobs().Get(context.Background(), job.ID)
		return err == nil && stored.State == JobSucceeded && stored.Attempt == 2
	})
}
