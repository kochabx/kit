package scheduler

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestNewTaskClientQuickstart(t *testing.T) {
	client := testRedis(t)
	namespace := fmt.Sprintf("quickstart-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s := queueTestScheduler(t, client, namespace, RoleCombined, "mail")
	type email struct {
		To string `json:"to"`
	}
	task, err := newTaskClient[email](s, "email.send", WithQueue("mail"), WithVersion("2"))
	if err != nil {
		t.Fatal(err)
	}
	if _, registered := s.lookupHandler("mail", "email.send"); registered {
		t.Fatal("constructor registered a handler")
	}
	received := make(chan email, 1)
	if err := task.Handle(func(_ context.Context, payload email) error { received <- payload; return nil }); err != nil {
		t.Fatal(err)
	}
	runQueueTestScheduler(t, s)
	want := email{To: "user@example.com"}
	job, err := task.Enqueue(context.Background(), want)
	if err != nil {
		t.Fatal(err)
	}
	if job.Queue != "mail" || job.TaskName != "email.send" {
		t.Fatalf("job: %+v", job)
	}
	select {
	case got := <-received:
		if got != want {
			t.Fatalf("got=%+v want=%+v", got, want)
		}
	case <-time.After(time.Second):
		t.Fatal("quickstart task was not executed")
	}
}
