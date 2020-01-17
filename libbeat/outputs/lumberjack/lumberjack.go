// Licensed to Elasticsearch B.V. under one or more contributor
// license agreements. See the NOTICE file distributed with
// this work for additional information regarding copyright
// ownership. Elasticsearch B.V. licenses this file to you under
// the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package lumberjack

import (
	"fmt"
	"github.com/elastic/beats/libbeat/beat"
	"github.com/elastic/beats/libbeat/common"
	"github.com/elastic/beats/libbeat/outputs"
	"github.com/elastic/beats/libbeat/publisher"
	"io"
	"log"
	"os"
)

var (
	LumberConfig lumberjackConfig
	Info         *log.Logger
	Debug        *log.Logger
	Warning      *log.Logger
	Error        *log.Logger
)

func init() {
	fmt.Println("Initializing lumberjack output....")
	outputs.RegisterType("lumberjack", makeLumberjackout)
	InitLog(os.Stdout,os.Stdout, os.Stdout, os.Stderr)
	config, err := ReadConfig()
	if err != nil {
		Error.Println("Error reading Lumberjack Config from Env. Invalid Lumberjack config.")
	}
	LumberConfig = *config
}

type lumberjackOutput struct {
	postMetrics bool
	workerNodes []Node
	beat        beat.Info
	observer    outputs.Observer
}

func makeLumberjackout(
	_ outputs.IndexManager,
	beat beat.Info,
	observer outputs.Observer,
	cfg *common.Config,
) (outputs.Group, error) {
	Info.Println("Post metrics to Lumberjack Post Metrics is set to [", LumberConfig.PostMetrics, "] in Config.")
	lo := &lumberjackOutput{
		postMetrics: LumberConfig.PostMetrics,
		beat:        beat,
		observer:    observer,
	}
	return outputs.Success(-1, 0, lo)
}

func (out *lumberjackOutput) Close() error {
	return out.Close()
}

func (out *lumberjackOutput) Publish(
	batch publisher.Batch,
) error {
	defer batch.ACK()
	st := out.observer
	events := batch.Events()
	st.NewBatch(len(events))

	eventsDropped := 0
	if LumberConfig.PostMetrics == true {
		eventsDropped = IngestLogs(batch)
	}else{
		eventsDropped= len(batch.Events())
	}
	st.Dropped(eventsDropped)
	st.Acked(len(events) - eventsDropped)
	//PrintMemUsage()
	return nil
}

/*func PrintMemUsage() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	Info.Println("Alloc: "+strconv.FormatUint(bToMb(m.Alloc),10)+" MiB | TotalAlloc: "+strconv.FormatUint(bToMb(m.TotalAlloc),10)+" MiB | Sys: "+strconv.FormatUint(bToMb(m.Sys),10)+" MiB | Mallocs: "+strconv.FormatUint(m.Mallocs,10) +" | Frees: "+strconv.FormatUint(m.Frees,10) +" | LiveObjects: "+strconv.FormatUint(m.Mallocs-m.Frees,10) +" | PauseTotalNs: "+strconv.FormatUint(m.PauseTotalNs,10) +" | NumGC: "+strconv.FormatUint(uint64(m.NumGC),10) +" | NumGoroutines: "+strconv.Itoa(runtime.NumGoroutine() ) +")")
}

func bToMb(b uint64) uint64 {
	return b / 1024 / 1024
}*/

func (out *lumberjackOutput) String() string {
	return ""
}

type LogFileInfo struct {
	File   PathInfo `json:"file"`
	Offset string   `json:"offset"`
}

type PathInfo struct {
	Path string `json:"path"`
}

func InitLog(
	debugHandle io.Writer,
	infoHandle io.Writer,
	warningHandle io.Writer,
	errorHandle io.Writer) {
	Debug = log.New(debugHandle,
		"DEBUG: ",
		log.Ldate|log.Ltime|log.Lshortfile)

	Info = log.New(infoHandle,
		"INFO: ",
		log.Ldate|log.Ltime|log.Lshortfile)

	Warning = log.New(warningHandle,
		"WARNING: ",
		log.Ldate|log.Ltime|log.Lshortfile)

	Error = log.New(errorHandle,
		"ERROR: ",
		log.Ldate|log.Ltime|log.Lshortfile)
}
