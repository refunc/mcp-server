package mcpserver

import (
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	rfv1beta3 "github.com/refunc/refunc/pkg/apis/refunc/v1beta3"
	"k8s.io/klog/v2"
)

type toolConfig struct {
	ToolSet string          `json:"toolset"` // same with secret name
	Desc    string          `json:"desc"`
	Schema  json.RawMessage `json:"schema"`
}

func (rcs *RefuncMCPServer) handleTriggerChange(obj interface{}) {
	trigger, ok := obj.(*rfv1beta3.Trigger)
	if !ok {
		klog.Errorf("obj %v not is a trigger", obj)
		return
	}
	if trigger.Spec.Type != MCPTriggerType {
		return
	}
	mcpKey, config, err := triggerForToolConfig(trigger)
	if err != nil {
		return
	}
	c, loaded := rcs.mcps.Load(mcpKey)
	if !loaded {
		klog.Errorf("mcp server %s not found", mcpKey)
		return
	}
	mcpServer := c.(*server.MCPServer)
	// delete tool
	if !trigger.DeletionTimestamp.IsZero() {
		mcpServer.DeleteTools(trigger.Spec.FuncName)
		klog.Infof("Delete tool %s func %s", mcpKey, trigger.Spec.FuncName)
		return
	}
	// upsert tool
	tool := mcp.NewToolWithRawSchema(trigger.Spec.FuncName, config.Desc, config.Schema)
	mcpServer.DeleteTools(trigger.Spec.FuncName)
	mcpServer.AddTool(tool, createToolHandler(rcs, trigger))
	klog.Infof("update tool %s func %s", mcpKey, trigger.Spec.FuncName)
}

func triggerForToolConfig(trigger *rfv1beta3.Trigger) (string, toolConfig, error) {
	var config toolConfig
	if err := json.Unmarshal(trigger.Spec.Common.Args, &config); err != nil {
		klog.Errorf("unmarshal %s/%s tool config error %v", trigger.Namespace, trigger.Name, err)
		return "", config, err
	}
	mcpKey := fmt.Sprintf("%s/%s", trigger.Namespace, config.ToolSet)
	return mcpKey, config, nil
}
