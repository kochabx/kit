package scheduler_test

import (
	"context"
	"time"

	"github.com/kochabx/kit/core/scheduler"
	"github.com/redis/go-redis/v9"
)

func Example() {
	r := redis.NewClient(&redis.Options{Addr: "localhost:6379", ContextTimeoutEnabled: true})
	defer r.Close()
	c, err := scheduler.NewClient(r, scheduler.ClientConfig{Namespace: "app"})
	if err != nil {
		panic(err)
	}
	defer c.Close()
	w, err := scheduler.NewWorker(r, scheduler.WorkerConfig{Namespace: "app"})
	if err != nil {
		panic(err)
	}
	defer w.Close()
	type Payload struct{ ID string }
	task, err := scheduler.NewTask[Payload]("send", scheduler.WithTimeout(time.Minute))
	if err != nil {
		panic(err)
	}
	if err := w.Register(task, func(ctx context.Context, p Payload) error { return nil }); err != nil {
		panic(err)
	}
	ctx := context.Background()
	_, err = c.Enqueue(ctx, task, Payload{ID: "business-id"}, scheduler.WithJobID("request-id"))
	if err != nil {
		panic(err)
	}
	_, err = c.Schedules().Create(ctx, task, Payload{ID: "daily"}, scheduler.CronSpec{Expression: "0 9 * * *", TimeZone: "Asia/Shanghai"})
	if err != nil {
		panic(err)
	}
	if err := w.Run(ctx); err != nil {
		panic(err)
	}
}
