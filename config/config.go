package config

import (
	"encoding/json"
	"io/ioutil"
)

type Config struct {
	Token string `json:"token"`
}

func Read(fileName string) (*Config, error) {
	jsonData, err := ioutil.ReadFile(fileName)
	if err != nil {
		return nil, err
	}

	config := &Config{}
	if err := json.Unmarshal(jsonData, config); err != nil {
		return nil, err
	}

	return config, nil
}

func Write(fileName string, config *Config) error {
	jsonData, err := json.Marshal(config)
	if err != nil {
		return err
	}

	if err := ioutil.WriteFile(fileName, jsonData, 0600); err != nil {
		return err
	}

	return nil
}
