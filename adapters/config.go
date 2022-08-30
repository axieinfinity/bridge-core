package adapters

import "github.com/kelseyhightower/envconfig"

var AppConfig Config

type Config struct {
	Prometheus Prometheus
}

type Prometheus struct {
	PushURL      string `default:"localhost:9091" envconfig:"PUSH_URL"`
	PushJob      string `default:"bridge-monitoring" envconfig:"PUSH_JOB"`
	PushInterval int    `default:"15" envconfig:"PUSH_INTERVAl"`
	TurnOn       bool   `envconfig:"PROMETHEUS_TURN_ON"`
	InstanceName string `envconfig:"PUSH_LABEL_INSTANCE_NAME"`
}

func New() (*Config, error) {
	if err := envconfig.Process("", &AppConfig); err != nil {
		return nil, err
	}
	return &AppConfig, nil
}
