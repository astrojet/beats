package lumberjack

import (
	"bytes"
	"container/ring"
	b64 "encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"
	"strings"
	"github.com/elastic/beats/libbeat/outputs/lumberjack/protobufs"
	"github.com/golang/protobuf/proto"
)

var (
	MasterNode              string
	MasterPort              string
	workerNodeRingsAllNamespaces         = make(map[string]*ring.Ring)
	markdownNamespaces      = make([]string, 0)
	masterConnectFailures   = 0
	masterConnectLastFailed = time.Now()
)

func GetWorkersWithRetries(namespace map[string]string) (*ring.Ring, error) {
	namespaceKey := GetDelimitedNamespaceKey(namespace)
	if nodes, ok := workerNodeRingsAllNamespaces[namespaceKey]; ok {
		return nodes, nil
	}

	if masterConnectFailures < 3 {
		workers, err := GetWorkers(namespace)
		if err != nil {
			time.Sleep(100 * time.Millisecond)
			workers, err = GetWorkers(namespace)
		}
		return workers, err
	} else {
		elapsedMins := time.Since(masterConnectLastFailed).Minutes()
		if elapsedMins > 1 {
			masterConnectFailures = 0
			workers, err := GetWorkers(namespace)
			if err != nil {
				time.Sleep(100 * time.Millisecond)
				workers, err = GetWorkers(namespace)
			}
			return workers, err
		} else {
			msg := "Master node looks to be down...last retry less than 5 mins ago...standing down"
			return nil, errors.New(msg)
		}
	}
}

func GetWorkers(namespace map[string]string) (*ring.Ring, error) {
	namespaceKey := GetDelimitedNamespaceKey(namespace)
	namespaceWorkers, getWorkersError := GetWorkersForNamespace(LumberConfig, namespace)
	if getWorkersError != nil {
		Error.Println("Error occurred when getting workers from Lumberjack for namespace ["+namespaceKey+"]:", getWorkersError)
		return nil, getWorkersError
	} else {
		if len(namespaceWorkers) > 0 {
			namespaceWorkers = getReplicaNodes(namespaceWorkers)
			r := ring.New(len(namespaceWorkers))
			for _, workerNode := range namespaceWorkers {
				r.Value = workerNode
				r = r.Next()
			}
			workerNodeRingsAllNamespaces[namespaceKey] = r
			Info.Println("----- Lumberjack workers for namespace [" + namespaceKey + "] ----")
			for _, workerNode := range namespaceWorkers {
				Info.Println(workerNode.IpPort)
			}
			Info.Println("--------------------------------------------------------------")
			return r, nil
		} else {
			msg := "Error occurred when getting workers from Lumberjack for namespace [" + namespaceKey + "]: Got Empty Worker List!"
			Error.Println(msg)
			return nil, errors.New(msg)
		}
	}
}

func getReplicaNodes(workerNodes []Node) []Node {
	workerNodeMap := make(map[int]Node, 0)
	for _, workerNode := range workerNodes {
		workerNodeMap[workerNode.Id] = workerNode
	}
	workerNodesWithReplicas := make([]Node, 0)

	for _, workerNode := range workerNodes {
		workerReplicas := make([]Node, 0)
		replicaIds := workerNode.ReplicaIds
		for _, replicaId := range replicaIds {
			replicaNode := workerNodeMap[replicaId]
			workerReplicas = append(workerReplicas, replicaNode)
		}
		workerNode.Replicas = workerReplicas
		workerNodesWithReplicas = append(workerNodesWithReplicas, workerNode)
	}
	return workerNodesWithReplicas
}

func GetWorkersForNamespace(c lumberjackConfig, namespace map[string]string) ([]Node, error) {
	workers := make([]Node, 0)
	getWorkersAPI := fmt.Sprintf("http://%s:%s/GetAllWorkers", c.MasterNode, c.MasterPort)

	cmdType := protobufs.CommandType_GetAllWorkers
	metaData := protobufs.Metadata{Namespace: namespace}
	commsEnvelop := protobufs.CommsEnvelop{
		CommandType: &cmdType,
		LogLineSet:  &protobufs.LogLineSet{Metadata: &metaData},
	}
	data, marshalErr := proto.Marshal(&commsEnvelop)
	if marshalErr != nil {
		//Error.Println("Marshalling error occurred when creating Get Workers API request: ", marshalErr)
		return nil, marshalErr
	}
	inputArr := b64.URLEncoding.EncodeToString(data)
	req, _ := http.NewRequest("POST", getWorkersAPI, bytes.NewBuffer([]byte(inputArr)))
	req.Header.Add("Content-Type", "application/protobuf")
	client := &http.Client{Timeout: 200 * time.Millisecond}
	res, connectErr := client.Do(req)

	if connectErr != nil {
		masterConnectFailures = masterConnectFailures + 1
		masterConnectLastFailed = time.Now()
		//Error.Println(errors.New("Unable to get worker nodes form lumberjack. May be the cluster is not up and running...Skipping for now: "+connectErr.Error()));
		return nil, connectErr
	}
	masterConnectFailures = 0
	defer res.Body.Close()
	//Debug.Println("get workers response: ", res)
	resJson, readErr := ioutil.ReadAll(res.Body)
	if readErr != nil {
		//Error.Println("Invalid response form lumberjack when getting worker nodes.: "+readErr.Error());
		return nil, readErr
	}
	//Debug.Println("get workers response json: ", resJson)
	getworkersResponse, decodeErr := b64.URLEncoding.DecodeString(string(resJson))

	if decodeErr != nil {
		//Error.Println(errors.New("Error occurred when decoding get workers response from lumberjack.: "+decodeErr.Error()));
		return nil, decodeErr
	}
	//Debug.Println("get workers decoded response: ", getworkersResponse)
	respEnvelop := &protobufs.CommsEnvelop{}
	unmarshalErr := proto.Unmarshal(getworkersResponse, respEnvelop)
	if unmarshalErr != nil {
		//fmt.Println("Unmarshalling error occurred when reading get workers response API: ", unmarshalErr)
		return nil, unmarshalErr
	}
	//Debug.Println("replyMessage: ", *respEnvelop.ReplyMessage)
	json.Unmarshal([]byte(*respEnvelop.ReplyMessage), &workers)
	//Debug.Println("worker nodes: ", workers)
	return workers, nil
}

func GetDelimitedNamespaceKey(namespace map[string]string) string {
	namespaceKey := namespace["a_dc"] + "_" + namespace["b_app"] + "_" + namespace["c_type"] + "_" + namespace["d_feature"] + "_" + namespace["e_instance"]
	return namespaceKey
}

func getNamespaceMapFromKey(namespaceKey string) map[string]string{
	namespaceParts := strings.Split(namespaceKey, "_")

	namespace := make(map[string]string, 0)
	namespace["a_dc"] = namespaceParts[0]
	namespace["b_app"] = namespaceParts[1]
	namespace["c_type"] = namespaceParts[2]
	namespace["d_feature"] = namespaceParts[3]
	namespace["e_instance"] = namespaceParts[4]
	return namespace
}


func getNamespaceMap(logRecordNamespace LumberjackNameSpace) map[string]string{
	namespace := make(map[string]string, 0)
	namespace["a_dc"] = logRecordNamespace.A_dc
	namespace["b_app"] = logRecordNamespace.B_app
	namespace["c_type"] = logRecordNamespace.C_type
	namespace["d_feature"] = logRecordNamespace.D_feature
	namespace["e_instance"] = logRecordNamespace.E_instance
	return namespace
}

type Node struct {
	Id           int    `json:"id"`
	NodeType     string `json:"type"`
	IpPort       IpPort `json:"ipPort"`
	PublicIpPort IpPort `json:"publicIpPort"`
	ParentIds    []int  `json:"parentIds"`
	ReplicaIds   []int  `json:"replicaIds"`
	ReadOnly     bool   `json:"readOnly"`
	Replicas     []Node
}

type IpPort struct {
	Ip   string `json:"ip"`
	Port int    `json:"port"`
}
