package cx

import "sort"

// Default group names and their initialization priority order.
// Lower order values are started first and stopped last.
const (
	ConfigGroup     = "config"     // order: -100
	DatabaseGroup   = "database"   // order: -80
	ServiceGroup    = "service"    // order: -60
	HandlerGroup    = "handler"    // order: -40
	ControllerGroup = "controller" // order: -20
)

// defaultGroupOrders defines the built-in groups available in every Container.
var defaultGroupOrders = map[string]int{
	ConfigGroup:     -100,
	DatabaseGroup:   -80,
	ServiceGroup:    -60,
	HandlerGroup:    -40,
	ControllerGroup: -20,
}

// Descriptor holds the registration metadata for a single component.
type Descriptor struct {
	Name     string
	Instance Component
	Order    int
	Group    string
}

// group is the internal collection of components sharing the same priority level.
type group struct {
	name       string
	order      int
	components map[string]*Descriptor

	// sorted cache — invalidated when a new component is added
	sorted []*Descriptor
	dirty  bool
}

func newGroup(name string, order int) *group {
	return &group{
		name:       name,
		order:      order,
		components: make(map[string]*Descriptor),
		dirty:      true,
	}
}

// add registers a descriptor into the group. Caller must hold an appropriate lock.
func (g *group) add(d *Descriptor) {
	g.components[d.Name] = d
	g.dirty = true
}

// getSorted returns a stable, order-sorted slice of all descriptors.
// Caller must hold an appropriate lock.
func (g *group) getSorted() []*Descriptor {
	if !g.dirty {
		return g.sorted
	}
	if cap(g.sorted) >= len(g.components) {
		g.sorted = g.sorted[:0]
	} else {
		g.sorted = make([]*Descriptor, 0, len(g.components))
	}
	for _, d := range g.components {
		g.sorted = append(g.sorted, d)
	}
	sort.Slice(g.sorted, func(i, j int) bool {
		return g.sorted[i].Order < g.sorted[j].Order
	})
	g.dirty = false
	return g.sorted
}
