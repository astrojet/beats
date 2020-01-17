package lumberjack

import (
	"bytes"
	b64 "encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/elastic/beats/libbeat/outputs/lumberjack/protobufs"
	"github.com/golang/protobuf/proto"
	"io/ioutil"
	"net/http"
	"time"
	"errors"
)

var (
	MasterNode string
	MasterPort string
	workerNodes = make(map[string][]Node, 0)
	currentWorkerIndexes = make(map[string]int, 0)
	markdownNamespaces = make([]string, 0)
)

func GetWorkersWithRetries(namespace map[string]string) ([]Node, error){
	namespaceKey := GetDelimitedNamespaceKey(namespace)
	if nodes, ok :=  workerNodes[namespaceKey] ; ok {
		return nodes,nil
	}

	workers, err := GetWorkers(namespace)
	if err != nil {
		time.Sleep(2 * time.Second)
		workers, err = GetWorkers(namespace)
	}
	return workers,err
}

func GetWorkers(namespace map[string]string) ([]Node, error){
	namespaceKey := GetDelimitedNamespaceKey(namespace)
	namespaceWorkers, getWorkersError := GetWorkersForNamespace(LumberConfig, namespace)
	if getWorkersError != nil {
		Error.Println("Error occurred when getting workers from Lumberjack for namespace ["+namespaceKey+"]:", getWorkersError)
		return nil,getWorkersError
	} else {
		if len(namespaceWorkers) > 0 {
			workerNodes[namespaceKey] = namespaceWorkers
			Info.Println("----- Lumberjack workers for namespace [" + namespaceKey + "] ----")
			Info.Println(namespaceWorkers)
			Info.Println("--------------------------------------------------------------")
			return namespaceWorkers,nil
		} else {
			msg := "Error occurred when getting workers from Lumberjack for namespace [" + namespaceKey + "]: Got Empty Worker List!"
			Error.Println(msg)
			return nil,errors.New(msg)
		}
	}
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
	res, connectErr := http.DefaultClient.Do(req)
	if connectErr != nil {
		//Error.Println(errors.New("Unable to get worker nodes form lumberjack. May be the cluster is not up and running...Skipping for now: "+connectErr.Error()));
		return nil, connectErr
	}
	defer res.Body.Close()
	//Debug.Println("get workers response: ", res)
	resJson, readErr := ioutil.ReadAll(res.Body)
	if readErr != nil{
		//Error.Println("Invalid response form lumberjack when getting worker nodes.: "+readErr.Error());
		return nil, readErr
	}
	//Debug.Println("get workers response json: ", resJson)
	getworkersResponse, decodeErr := b64.URLEncoding.DecodeString(string(resJson))

	if decodeErr != nil{
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

func GetDelimitedNamespaceKey(namespace map[string]string) string{
	namespaceKey := namespace["a_dc"]+"_"+namespace["b_app"]+"_"+namespace["c_type"]+"_"+namespace["d_feature"]+"_"+namespace["e_instance"];
	return namespaceKey;
}

type Node struct {
	Id           int    `json:"id"`
	NodeType     string `json:"type"`
	IpPort       IpPort `json:"ipPort"`
	PublicIpPort IpPort `json:"publicIpPort"`
	ParentIds    []int  `json:"parentIds"`
	ReplicaIds   [] int `json:"replicaIds"`
	ReadOnly     bool   `json:"readOnly"`
}

type IpPort struct {
	Ip   string `json:"ip"`
	Port int `json:"port"`
}
