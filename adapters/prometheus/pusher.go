package prometheus

import (
	"context"
	"fmt"
	"time"

	"github.com/axieinfinity/bridge-core/adapters"

	"github.com/ethereum/go-ethereum/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/push"
)

type Pusher struct {
	counters   map[string]prometheus.Counter
	gauges     map[string]prometheus.Gauge
	histograms map[string]prometheus.Histogram
	registry   *prometheus.Registry
	pusher     *push.Pusher
	labels     map[string]string
}

func (p *Pusher) AddCounter(name string, description string) *Pusher {
	if _, ok := p.counters[name]; ok {
		return p
	}

	counter := prometheus.NewCounter(prometheus.CounterOpts{
		Name:        name,
		Help:        description,
		ConstLabels: p.labels,
	})
	p.counters[name] = counter
	p.pusher.Collector(counter)
	return p
}

func (p *Pusher) AddCounterWithLable(name string, description string, labels map[string]string) *Pusher {
	if _, ok := p.counters[name]; ok {
		return p
	}
	for k, v := range p.labels {
		labels[k] = v
	}
	counter := prometheus.NewCounter(prometheus.CounterOpts{
		Name:        name,
		Help:        description,
		ConstLabels: labels,
	})
	p.counters[name] = counter
	p.pusher.Collector(counter)
	return p
}

func (p *Pusher) IncrCounter(name string, value int) error {
	if _, ok := p.counters[name]; !ok {
		return fmt.Errorf("counter %v was not initialized", name)
	}
	p.counters[name].Add(float64(value))
	return nil
}

func (p *Pusher) AddGauge(name string, description string) *Pusher {
	if _, ok := p.gauges[name]; ok {
		return p
	}

	gauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        name,
		Help:        description,
		ConstLabels: p.labels,
	})
	p.gauges[name] = gauge
	p.pusher.Collector(gauge)
	return p
}

func (p *Pusher) AddGaugeWithLabel(name string, description string, labels map[string]string) *Pusher {
	if _, ok := p.gauges[name]; ok {
		return p
	}

	for k, v := range p.labels {
		labels[k] = v
	}

	gauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        name,
		Help:        description,
		ConstLabels: labels,
	})
	p.gauges[name] = gauge
	p.pusher.Collector(gauge)
	return p
}

func (p *Pusher) IncrGauge(name string, value int) error {
	if _, ok := p.gauges[name]; !ok {
		return fmt.Errorf("gauge %v was not initialized", name)
	}

	p.gauges[name].Add(float64(value))
	return nil
}

func (p *Pusher) SetGauge(name string, value int) error {
	if _, ok := p.gauges[name]; !ok {
		return fmt.Errorf("gauge %v was not initialized", name)
	}

	p.gauges[name].Set(float64(value))
	return nil
}

func (p *Pusher) AddHistogram(name string, description string) *Pusher {
	if _, ok := p.histograms[name]; ok {
		return p
	}

	histogram := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:        name,
		Help:        description,
		ConstLabels: p.labels,
	})
	p.histograms[name] = histogram
	p.pusher.Collector(histogram)
	return p
}

func (p *Pusher) AddHistogramWithLabels(name string, description string, labels map[string]string) *Pusher {
	if _, ok := p.histograms[name]; ok {
		return p
	}
	for k, v := range p.labels {
		labels[k] = v
	}
	histogram := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:        name,
		Help:        description,
		ConstLabels: labels,
	})
	p.histograms[name] = histogram
	p.pusher.Collector(histogram)
	return p
}

func (p *Pusher) ObserveHistogram(name string, value int) error {
	if _, ok := p.histograms[name]; !ok {
		return fmt.Errorf("histogram %v was not initialized", name)
	}

	p.histograms[name].Observe(float64(value))
	return nil
}

func (p *Pusher) Push() error {
	return p.pusher.Push()
}

func (p *Pusher) Start(ctx context.Context) {
	ticker := time.NewTicker(time.Second * time.Duration(adapters.AppConfig.Prometheus.PushInterval))
	for {
		select {
		case <-ticker.C:
			if err := p.Push(); err != nil {
				log.Error("Push metrics got error", err.Error())
			}
		case <-ctx.Done():
			ticker.Stop()
			return
		}
	}

}

func NewPusher() *Pusher {
	pusher := push.New(adapters.AppConfig.Prometheus.PushURL, adapters.AppConfig.Prometheus.PushJob)
	return &Pusher{
		pusher:     pusher,
		counters:   make(map[string]prometheus.Counter),
		gauges:     make(map[string]prometheus.Gauge),
		histograms: make(map[string]prometheus.Histogram),
		labels: map[string]string{
			"instance": adapters.AppConfig.Prometheus.InstanceName,
		},
	}
}
