package metrics

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
)

type Registry struct {
	mu       sync.RWMutex
	counters map[string]float64
	gauges   map[string]float64
}

func NewRegistry() *Registry {
	return &Registry{
		counters: map[string]float64{},
		gauges:   map[string]float64{},
	}
}

func (r *Registry) IncCounter(name string) {
	r.AddCounter(name, 1)
}

func (r *Registry) AddCounter(name string, delta float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.counters[name] += delta
}

func (r *Registry) EnsureCounter(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.counters[name]; !ok {
		r.counters[name] = 0
	}
}

func (r *Registry) SetGauge(name string, value float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gauges[name] = value
}

func (r *Registry) EnsureGauge(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.gauges[name]; !ok {
		r.gauges[name] = 0
	}
}

func (r *Registry) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		_, _ = w.Write([]byte(r.Render()))
	})
}

func (r *Registry) Render() string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	lines := make([]string, 0, len(r.counters)+len(r.gauges))
	counterNames := make([]string, 0, len(r.counters))
	for name := range r.counters {
		counterNames = append(counterNames, name)
	}
	sort.Strings(counterNames)
	for _, name := range counterNames {
		lines = append(lines, fmt.Sprintf("# TYPE %s counter", name))
		lines = append(lines, fmt.Sprintf("%s %g", name, r.counters[name]))
	}

	gaugeNames := make([]string, 0, len(r.gauges))
	for name := range r.gauges {
		gaugeNames = append(gaugeNames, name)
	}
	sort.Strings(gaugeNames)
	for _, name := range gaugeNames {
		lines = append(lines, fmt.Sprintf("# TYPE %s gauge", name))
		lines = append(lines, fmt.Sprintf("%s %g", name, r.gauges[name]))
	}

	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}
