package lumberjack

import "fmt"

type lumberjackConfig struct {
PostMetrics            bool   `config:"postMetrics"`
MasterNode             string `config:"masterNode"`
MasterPort             string `config:"masterPort"`
GetWorkersMaxRetries   int    `config:"getWorkersMaxRetries"`
GetWorkersFailWaitMins int    `config:"getWorkersFailWaitMins"`
}

func defaultConfig() lumberjackConfig {
return lumberjackConfig{
MasterNode:             "localhost",
MasterPort:             "3030",
GetWorkersMaxRetries:   3,
GetWorkersFailWaitMins: 5,
}
}

func (c *lumberjackConfig) Validate() error {
	if c.MasterNode == "" {
		return fmt.Errorf("Lumberjack Master Node is nil or empty in config")
	}else if c.MasterPort == "" {
		return fmt.Errorf("Lumberjack Master Port is nil or empty in config")
	}
	return nil
}
