package lumberjack

import "time"

type LumberjackLogRecord struct {
	Timestamp time.Time
	Message   string
	Namespace LumberjackNameSpace
}

type LumberjackNameSpace struct {
	A_dc       string
	B_app      string
	C_type     string
	D_feature  string
	E_instance string
}
