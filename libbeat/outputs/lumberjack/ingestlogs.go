package lumberjack

import (
	"bytes"
	b64 "encoding/base64"
	"fmt"
	"github.com/elastic/beats/libbeat/common"
	"github.com/elastic/beats/libbeat/outputs/lumberjack/protobufs"
	"github.com/elastic/beats/libbeat/publisher"
	"github.com/golang/protobuf/proto"
	"io/ioutil"
	"net/http"
	"strings"
	"time"
	"errors"
)

var(
	ingestLogsParallelism = 10
)

func IngestLogs(batch publisher.Batch) int {
	dropped := 0
	events := batch.Events()
	for i := range events {
		event := &events[i]
		if event != nil && LumberConfig.PostMetrics == true{
			lumberjackLogRecord, logRecordErr := getLogRecord(event)
			if logRecordErr != nil {
				Error.Println("Error occurred when creating Lumberjack Log Record from Beats Event", logRecordErr)
			} else {
				ingestLogRecord(&lumberjackLogRecord)
			}
		}
	}
	return dropped
}

func ingestLogRecord(logRecord *LumberjackLogRecord) error {
	logRecordNamespace := logRecord.Namespace

	namespace := make(map[string]string, 0)
	namespace["a_dc"] = logRecordNamespace.A_dc
	namespace["b_app"] = logRecordNamespace.B_app
	namespace["c_type"] = logRecordNamespace.C_type
	namespace["d_feature"] = logRecordNamespace.D_feature
	namespace["e_instance"] = logRecordNamespace.E_instance

	workerNodes, getWorkersErr := GetWorkersWithRetries(namespace)

	if len(workerNodes) > 0 && getWorkersErr == nil{
		namespaceKey := GetDelimitedNamespaceKey(namespace)
		currentWorkerIndex := currentWorkerIndexes[namespaceKey]

		if currentWorkerIndex >= len(workerNodes) {
			currentWorkerIndex = 0
		}
		currentWorkerIndexes[namespaceKey] = currentWorkerIndex
		workerNode := workerNodes[currentWorkerIndex]
		workerIpPort := workerNode.IpPort

		ingestLogsAPI := fmt.Sprintf("http://%s:%d/", workerIpPort.Ip, workerIpPort.Port)
		//ingestLogsAPI := fmt.Sprintf("http://%s:%d/", "host.docker.internal", workerIpPort.Port)

		cmdType := protobufs.CommandType_WRITE_LOG
		metaData := protobufs.Metadata{Namespace: namespace}
		timestamp := logRecord.Timestamp.UnixNano() / 1000000
		content := logRecord.Message
		line := protobufs.LogLine{TimeStamp: &timestamp, Content: &content}
		loglines := make([]*protobufs.LogLine, 0)
		loglines = append(loglines, &line)
		commsEnvelop := protobufs.CommsEnvelop{
			CommandType: &cmdType,
			LogLineSet:  &protobufs.LogLineSet{LogLines: loglines, Metadata: &metaData},
		}

		data, err := proto.Marshal(&commsEnvelop)
		if err != nil {
			return err
		}

		postErr := postLogRecord(ingestLogsAPI, data, workerNode, namespaceKey, currentWorkerIndex)

		if postErr != nil {
			time.Sleep(2 * time.Second)
			postErr = postLogRecord(ingestLogsAPI, data, workerNode, namespaceKey, currentWorkerIndex)
		}
		return postErr
	}else{
		return errors.New("No workers available to ingest the log record")
	}
}

func postLogRecord(ingestLogsAPI string, data []byte, workerNode Node, namespaceKey string, currentWorkerIndex int) error{
	inputArr := b64.URLEncoding.EncodeToString(data)
	req, _ := http.NewRequest("POST", ingestLogsAPI, bytes.NewBuffer([]byte(inputArr)))
	req.Header.Add("Content-Type", "application/protobuf")
	res, httpErr := http.DefaultClient.Do(req)

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

					} else if *responseCmdType == protobufs.CommandType_BLOCK_FULL {
						currentWorkerIndexes[namespaceKey] = currentWorkerIndex+1
						Error.Println("Worker Block Full!!...Switching to next worker for namespace ["+namespaceKey+"] ")
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
}

func getLogRecord(event *publisher.Event) (LumberjackLogRecord, error) {
	var lumberjackRecord LumberjackLogRecord
	content := event.Content
	time := content.Timestamp

	//Debug.Println("getLogRecord: time: ", time)
	message, messageErr := content.Fields.GetValue("message")
	if messageErr != nil {
		//Error.Println("Error getting message from log: ", messageErr)
	}

	messageStr := fmt.Sprintf("%v", message)
	log, logErr := content.Fields.GetValue("log")
	//Debug.Println("getLogRecord: log: ", log)
	//Debug.Println("getLogRecord: logErr: ", logErr)

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
	//Debug.Println("getLogRecord: LumberjackLogRecord: time: ", time)
	//Debug.Println("getLogRecord: LumberjackLogRecord: messageStr: ", messageStr)
	//Debug.Println("getLogRecord: LumberjackLogRecord: namespace: ", namespace)
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
