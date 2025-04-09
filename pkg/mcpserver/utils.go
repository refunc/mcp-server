package mcpserver

import "k8s.io/klog/v2"

type GlogWriter struct{}

func (writer GlogWriter) Write(data []byte) (n int, err error) {
	klog.Info(string(data))
	return len(data), nil
}
