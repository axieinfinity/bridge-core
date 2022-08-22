package prometheus

import (
	"context"
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
}

func (p *Pusher) AddCounter(name string, description string) *Pusher {
	if _, ok := p.counters[name]; ok {
		return p
	}

	counter := prometheus.NewCounter(prometheus.CounterOpts{
		Name: name,
		Help: description,
	})
	p.counters[name] = counter
	p.pusher.Collector(counter)
	return p
}

func (p *Pusher) AddCounterWithLable(name string, description string, labels map[string]string) *Pusher {
	if _, ok := p.counters[name]; ok {
		return p
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

func (p *Pusher) IncrCounter(name string, value int) {
	if _, ok := p.counters[name]; !ok {
		return
	}
	p.counters[name].Add(float64(value))
}

func (p *Pusher) AddGauge(name string, description string) *Pusher {
	if _, ok := p.gauges[name]; ok {
		return p
	}
	gauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: name,
		Help: description,
	})
	p.gauges[name] = gauge
	p.pusher.Collector(gauge)
	return p
}

func (p *Pusher) AddGaugeWithLabel(name string, description string, labels map[string]string) *Pusher {
	if _, ok := p.gauges[name]; ok {
		return p
	}
	gauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        name,
		Help:        description,
		ConstLabels: labels,
	})
	p.pusher.Collector(gauge)
	return p
}

func (p *Pusher) IncrGauge(name string, value int) {
	if _, ok := p.gauges[name]; !ok {
		return
	}

	p.gauges[name].Add(float64(value))
}

func (p *Pusher) SetGauge(name string, value int) {
	if _, ok := p.gauges[name]; !ok {
		return
	}

	p.gauges[name].Set(float64(value))
}

func (p *Pusher) AddHistogram(name string, description string) *Pusher {
	if _, ok := p.histograms[name]; ok {
		return p
	}

	histogram := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name: name,
		Help: description,
	})
	p.histograms[name] = histogram
	p.pusher.Collector(histogram)
	return p
}

func (p *Pusher) ObserveHistogram(name string, value int) {
	if _, ok := p.histograms[name]; !ok {
		return
	}

	p.histograms[name].Observe(float64(value))
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
				log.Error("Push metrics got error", err)
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
		pusher:   pusher,
		counters: make(map[string]prometheus.Counter),
		gauges:   make(map[string]prometheus.Gauge),
	}
}
