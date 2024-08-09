package config

import (
	"log"

	"rabbitmq-smpp-relay/pkg/utils"

	"github.com/ilyakaznacheev/cleanenv"
)

type Config struct {
	Env        string `yaml:"env"`
	Rabbitmq   `yaml:"rabbitmq"`
	HTTPServer `yaml:"http_server"`
	SMPP       `yaml:"smpp"`
}

type Rabbitmq struct {
	URL         string `yaml:"url"`
	Exchange    string `yaml:"exchange"`
	Queue       string `yaml:"queue"`
	Routing_key string `yaml:"routing_key"`
}

type HTTPServer struct {
	Address string `yaml:"address"`
}

type SMPP struct {
	Addr string `yaml:"address"`
	User string `yaml:"user"`
	Pass string `yaml:"password"`
}

func LoadConfig() *Config {
	configPath := "config.yaml"

	if configPath == "" {
		log.Fatalf("config path is not set or config file does not exist")
	}

	var cfg Config

	if err := cleanenv.ReadConfig(configPath, &cfg); err != nil {
		log.Fatalf("Cannot read config: %v", utils.Err(err))
	}

	return &cfg
}
