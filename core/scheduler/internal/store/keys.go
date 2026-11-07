package store

// NamespaceKeys owns immutable entities and namespace-wide uniqueness.
type NamespaceKeys struct{ prefix string }

func NewNamespaceKeys(namespace string) NamespaceKeys {
	return NamespaceKeys{prefix: "scheduler:{" + namespace + "}:v1:"}
}

func (k NamespaceKeys) Clock() string { return k.prefix + "clock" }

func (k NamespaceKeys) Job(id string) string { return k.prefix + "job:" + id }

func (k NamespaceKeys) JobPrefix() string { return k.prefix + "job:" }

func (k NamespaceKeys) Schedule(id string) string { return k.prefix + "schedule:" + id }

func (k NamespaceKeys) SchedulePrefix() string { return k.prefix + "schedule:" }

func (k NamespaceKeys) Unique(value string) string { return k.prefix + "unique:" + value }

func (k NamespaceKeys) UniquePlaceholder() string { return k.Unique("none") }

func (k NamespaceKeys) Queue(name string) QueueKeys { return QueueKeys{NamespaceKeys: k, Name: name} }

// QueueKeys scopes mutable indexes and streams to exactly one queue. All keys
// retain the namespace hash tag so each transition remains single-slot atomic.
type QueueKeys struct {
	NamespaceKeys
	Name string
}

func (k QueueKeys) prefix() string { return k.NamespaceKeys.prefix + "queue:" + k.Name + ":" }

func (k QueueKeys) Scheduled() string { return k.prefix() + "scheduled" }

func (k QueueKeys) Ready() string { return k.prefix() + "ready" }

func (k QueueKeys) Dead() string { return k.prefix() + "dead" }

func (k QueueKeys) Running() string { return k.prefix() + "running" }

func (k QueueKeys) Schedules() string { return k.prefix() + "schedules" }

func (k QueueKeys) ScheduleCatalog() string { return k.prefix() + "schedule:catalog" }

func (k QueueKeys) TerminalSchedules() string { return k.prefix() + "schedule:terminal" }

func (k QueueKeys) Group() string { return "workers" }

func (k QueueKeys) MaintenanceLease() string { return k.prefix() + "maintenance:lease" }

func (k NamespaceKeys) Submission(id string) string { return k.prefix + "submission:" + id }
func (k NamespaceKeys) Receipt(id, token string) string {
	return k.prefix + "receipt:" + id + ":" + token
}

func (k NamespaceKeys) History(id string) string { return k.prefix + "history:" + id }
