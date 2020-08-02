package lumberjack

import (
	"bytes"
	"container/ring"
	b64 "encoding/base64"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	"github.com/elastic/beats/libbeat/common"
	"github.com/elastic/beats/libbeat/outputs/lumberjack/protobufs"
	"github.com/elastic/beats/libbeat/publisher"
	"github.com/golang/protobuf/proto"
)

var (
	ingestLogsParallelism = 10
	apps                  = make(map[string]string)
	instances             = make(map[string]string)
	namespaceLogSizes     = make(map[string]int)
)

func PrintMetrics() {
	metricsTicker := time.NewTicker(60 * time.Second)
	for _ = range metricsTicker.C {
		Info.Println("--------------------- namespaces log Sizes -------------------------")
		for namespace, logSize := range namespaceLogSizes {
			fmt.Println("namespace:", namespace, "=>", "log Size KB:", logSize/1000)
			namespaceLogSizes[namespace] = 0
		}
	}
}

func IngestLogs(batch publisher.Batch)  {
	events := batch.Events()
	namespaceLogRecords := make(map[string] []*LumberjackLogRecord)
	for i := range events {
		event := &events[i]
		if event != nil {
			logRecord, logRecordErr := getLogRecord(event)
			if logRecordErr != nil {
				Error.Println("Error occurred when creating Lumberjack Log Record from Beats Event", logRecordErr)
			} else {
				logRecordNamespace := logRecord.Namespace
				apps[logRecordNamespace.B_app] = logRecordNamespace.B_app
				instances[logRecordNamespace.E_instance] = logRecordNamespace.E_instance

				namespace := getNamespaceMap(logRecordNamespace)
				namespaceKey := GetDelimitedNamespaceKey(namespace)
				logRecords, found := namespaceLogRecords[namespaceKey];
				if !found {
					logRecords = make([]*LumberjackLogRecord, 0)
				}
				logRecords = append(logRecords, &logRecord)
				namespaceLogRecords[namespaceKey] = logRecords
			}
		}
	}
	ingestLogRecords(namespaceLogRecords)
}

func ingestLogRecords(namespaceLogRecords map[string] []*LumberjackLogRecord) error {

	for namespaceKey, logRecords := range namespaceLogRecords {
		namespace := getNamespaceMapFromKey(namespaceKey)
		workerNodes, getWorkersErr := GetWorkersWithRetries(namespace)

		if workerNodes.Len() > 0 && getWorkersErr == nil {
			//TODO check if dual write is enabled through a setting
			//dualWriteEnabled := true
			error := sendIngestLogsToWorker(workerNodes, logRecords, namespace, namespaceKey, "primary")
			if error != nil {
				return error
			}
			/*if dualWriteEnabled {
				replicaNodes := workerNode.Replicas
				for _, replicaNode := range replicaNodes {
					sendIngestLogsToWorker(replicaNodes, logRecord, namespace, namespaceKey, "replica")
				}
			}*/
		} else {
			return errors.New("No workers available to ingest the log record")
		}
	}


	return nil
}

func sendIngestLogsToWorker(workerNodes *ring.Ring, logRecords []*LumberjackLogRecord, namespace map[string]string, namespaceKey string, nodeType string) error {
	workerNode := workerNodes.Value.(Node)
	workerIpPort := workerNode.IpPort
	ingestLogsAPI := fmt.Sprintf("http://%s:%d/", workerIpPort.Ip, workerIpPort.Port)
	//ingestLogsAPI := fmt.Sprintf("http://%s:%d/", "host.docker.internal", workerIpPort.Port)

	cmdType := protobufs.CommandType_WRITE_LOG
	metaData := protobufs.Metadata{Namespace: namespace}

	logLines := make([]*protobufs.LogLine, 0)
	for _,logRecord := range logRecords {
		timestamp := logRecord.Timestamp.UnixNano() / 1000000
		content := logRecord.Message

		logMsgBytes := len(logRecord.Message)
		if logSize, ok := namespaceLogSizes[namespaceKey]; ok {
			namespaceLogSizes[namespaceKey] = logSize+logMsgBytes
		}else{
			namespaceLogSizes[namespaceKey] = logMsgBytes
		}

		line := protobufs.LogLine{TimeStamp: &timestamp, Content: &content}
		logLines = append(logLines, &line)
	}
	commsEnvelop := protobufs.CommsEnvelop{
		CommandType: &cmdType,
		LogLineSet:  &protobufs.LogLineSet{LogLines: logLines, Metadata: &metaData},
	}

	data, err := proto.Marshal(&commsEnvelop)
	if err != nil {
		return err
	}

	postErr := postLogRecord(ingestLogsAPI, data, len(logLines), workerNodes, namespaceKey)
	if postErr != nil {
		Error.Println("Error occurred when posting log record ", postErr)
	}

	if postErr != nil {
		time.Sleep(500 * time.Millisecond)
		postErr = postLogRecord(ingestLogsAPI, data, len(logLines), workerNodes, namespaceKey)
		if postErr != nil {
			time.Sleep(500 * time.Millisecond)
			postErr = postLogRecord(ingestLogsAPI, data, len(logLines), workerNodes, namespaceKey)
			workerNodes = moveToNextWorker(namespaceKey, workerNodes)
			nextWorker := workerNodes.Value.(Node)
			Error.Println("Worker Down!!...Switching from [", workerIpPort, "]to next worker[", nextWorker.IpPort, "] for namespace ["+namespaceKey+"] ")
		}
	}
	return nil
}

func postLogRecord(ingestLogsAPI string, data []byte, noOfLogLines int, workerNodes *ring.Ring, namespaceKey string) error {
	workerNode := workerNodes.Value.(Node)
	inputArr := b64.URLEncoding.EncodeToString(data)
	req, _ := http.NewRequest("POST", ingestLogsAPI, bytes.NewBuffer([]byte(inputArr)))
	req.Header.Add("Content-Type", "application/protobuf")
	client := &http.Client{Timeout: 500 * time.Millisecond}
	res, httpErr := client.Do(req)

	if httpErr != nil {
		Error.Println("Error connecting to lumberjack worker [", workerNode, "] for namespace [", namespaceKey, "]: ", httpErr)
		return httpErr
	} else {
		defer res.Body.Close()
		resJson, readErr := ioutil.ReadAll(res.Body)
		if readErr != nil {
			Error.Println("Error reading lumberjack response from worker [", workerNode, "] for namespace [", namespaceKey, "]: ", readErr)
			return readErr
		} else {
			outputArr, err := b64.URLEncoding.DecodeString(string(resJson))
			respEnvelop := &protobufs.CommsEnvelop{}
			err = proto.Unmarshal(outputArr, respEnvelop)
			if err != nil {
				Error.Println("Unmarshalling error occurred in Ingest Logs API call: ", err)
				return err
			}
			if respEnvelop != nil {
				responseCmdType := respEnvelop.CommandType
				executionStatus := respEnvelop.ExecutionStatus
				if responseCmdType != nil && executionStatus != nil {
					if (*responseCmdType == protobufs.CommandType_ACK) && *executionStatus {
						Debug.Println("Sent [",noOfLogLines,"] log lines for  namespace [",namespaceKey,"] to worker [",workerNode.IpPort,"]")
					} else if *responseCmdType == protobufs.CommandType_BLOCK_FULL {
						workerNodes = moveToNextWorker(namespaceKey, workerNodes)
						nextWorker := workerNodes.Value.(Node)
						Error.Println("Worker Block Full!!...Switching from [", workerNode.IpPort, "] to next worker [", nextWorker.IpPort, "] for namespace ["+namespaceKey+"] ")
						workerNode = nextWorker
					} else {
						Error.Println("Unknown Ingest Logs Response. Got Response Command: ", responseCmdType, " for namespace ["+namespaceKey+"]")
					}
				} else {
					Error.Println("Unknown Ingest Logs Response. responseCmdType and executionStatus is nil", " for namespace ["+namespaceKey+"]")
				}
				return nil
			} else {
				Error.Println("Unknown Ingest Logs Response. respEnvelop is nil", " for namespace ["+namespaceKey+"]")
				return nil
			}
		}
	}
	return nil
}

func moveToNextWorker(namespaceStr string, workerNodes *ring.Ring) *ring.Ring {
	workerNodes = workerNodes.Next()
	workerNodeRingsAllNamespaces[namespaceStr] = workerNodes
	return   workerNodes
}

func getLogRecord(event *publisher.Event) (LumberjackLogRecord, error) {
	var lumberjackRecord LumberjackLogRecord
	content := event.Content
	time := content.Timestamp
	message, _ := content.Fields.GetValue("message")

	messageStr := fmt.Sprintf("%v", message)
	log, logErr := content.Fields.GetValue("log")

	var namespace LumberjackNameSpace
	if logErr == nil && log != "" {
		logFileInfo := log.(common.MapStr)
		fileObj, fileErr := logFileInfo.GetValue("file")
		if fileErr == nil {
			fileMap := fileObj.(common.MapStr)
			filePath, filePathErr := fileMap.GetValue("path")
			if filePathErr == nil {
				logFilePath := filePath.(string)
				ns, getNamespaceErr := getNamespaceFromFilePath(logFilePath)
				if getNamespaceErr != nil {
					//Error.Println("Error occurred when getting  namespace info from log file path. This is needed for namespace info to push logs!", getNamespaceErr)
					return lumberjackRecord, getNamespaceErr
				} else {
					namespace = ns
				}
			} else {
				//Error.Println("Log Event doesnt contain log file name and path info. This is needed for namespace info to push logs!")
			}
		} else {
			//Error.Println("Log Event doesnt contain log file name and path info. This is needed for namespace info to push logs!")
		}
	} else {
		//Error.Println("Log Event doesnt contain log file name and path info. This is needed for namespace info to push logs!")
		return lumberjackRecord, logErr
	}
	lumberjackRecord = LumberjackLogRecord{time, messageStr, namespace}
	return lumberjackRecord, nil
}

func getNamespaceFromFilePath(logPath string) (LumberjackNameSpace, error) {
	var lumberjackNamespace LumberjackNameSpace
	filePathArr := strings.Split(logPath, "/")
	if filePathArr != nil && len(filePathArr) > 0 {
		fileNameWithExt := filePathArr[len(filePathArr)-1]
		if fileNameWithExt != "" {
			fileNameWithExtArr := strings.Split(fileNameWithExt, ".")
			if fileNameWithExtArr != nil && len(fileNameWithExtArr) > 0 {
				fileName := fileNameWithExtArr[0]
				namespaceArr := strings.Split(fileName, "_")
				if namespaceArr != nil && len(namespaceArr) >= 5 {
					lumberjackNamespace = LumberjackNameSpace{namespaceArr[0], namespaceArr[1], namespaceArr[2], namespaceArr[3], namespaceArr[4]}
				} else {
					//Error.Println("Error occurred when getting Namespace info from log file name: ", logPath)
				}
			} else {
				//Error.Println("Error occurred when getting Namespace info from log file name: ", logPath)
			}
		} else {
			//Error.Println("Error occurred when getting Namespace info from log file name: ", logPath)
		}
	} else {
		//Error.Println("Error occurred when getting Namespace info from log file name: ", logPath)
	}
	return lumberjackNamespace, nil
}
