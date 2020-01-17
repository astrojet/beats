package lumberjack

import (
	"os"
	"strconv"
)

func ReadConfig() (*lumberjackConfig, error) {
	Info.Println("************************ Lumberjack Configuration **********************")
	postMetricsEnv := os.Getenv("postMetrics")
	masterNodeEnv := os.Getenv("masterNode")
	masterPortEnv := os.Getenv("masterPort")
	getWorkersMaxRetriesEnv := os.Getenv("getWorkersMaxRetries")
	getWorkersFailWaitMinsEnv := os.Getenv("getWorkersFailWaitMins")

	Info.Println("Post Metrics: ", postMetricsEnv)
	Info.Println("Master Node: ", masterNodeEnv)
	Info.Println("Master Port: ", masterPortEnv)
	Info.Println("Get Workers Max Retries: ", getWorkersMaxRetriesEnv)
	Info.Println("Get Workers Wait after Max Retries: ", getWorkersFailWaitMinsEnv)
	Info.Println("************************************************************************")
	c := defaultConfig()
	if masterNodeEnv != "" {
		c.MasterNode = masterNodeEnv
	}
	if masterPortEnv != "" {
		c.MasterPort = masterPortEnv
	}
	if postMetricsEnv != "" && postMetricsEnv == "1" {
		c.PostMetrics = true
	} else {
		c.PostMetrics = false
	}
	if getWorkersMaxRetriesEnv != "" {
		getWorkersMaxRetriesInt, err := strconv.Atoi(getWorkersMaxRetriesEnv)
		if err == nil {
			c.GetWorkersMaxRetries = getWorkersMaxRetriesInt
		} else {
			Error.Println("Incorrect config: [getWorkersMaxRetries]")
		}
	}

	if getWorkersFailWaitMinsEnv != "" {
		getWorkersFailWaitMinsInt, err := strconv.Atoi(getWorkersFailWaitMinsEnv)
		if err == nil {
			c.GetWorkersFailWaitMins = getWorkersFailWaitMinsInt
		} else {
			Error.Println("Incorrect config: [getWorkersFailWaitMins]")
		}
	}
	return &c, nil
}
