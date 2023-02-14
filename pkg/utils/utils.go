package utils

import (
	"encoding/json"
	"fmt"
	"github.com/kubernetes-csi/csi-lib-utils/connection"
	"google.golang.org/grpc"
	"time"
)

func DebugMessage(printMessageBefore string, v interface{}) string {
	jsons, errs := json.Marshal(v)
	if errs != nil {
		return fmt.Sprintf("%s", errs.Error())
	}
	return fmt.Sprintf("\n### %s %s", printMessageBefore, string(jsons))
}

func Connect(address string) (*grpc.ClientConn, error) {
	return connection.Connect(address, connection.OnConnectionLoss(connection.ExitOnConnectionLoss()))
}

func Probe(conn *grpc.ClientConn, singleCallTimeout time.Duration) error {
	return connection.ProbeForever(conn, singleCallTimeout)
}
