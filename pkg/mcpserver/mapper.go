package mcpserver

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/gorilla/mux"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	rfv1beta3 "github.com/refunc/refunc/pkg/apis/refunc/v1beta3"
	"github.com/refunc/refunc/pkg/operators/triggers/httptrigger/mmux"
	"k8s.io/klog/v2"
)

// ns token socpe mcp handlers
type entryHandler struct {
	basePath string
	router   *mmux.MutableRouter
	entrys   sync.Map
	rcs      *RefuncMCPServer
}

type mcpConfig struct {
	key   string
	ns    string
	fn    string
	Token string       `json:"token"` //same with secret name
	Tools []toolConfig `json:"tools"`
}

type toolConfig struct {
	Name   string          `json:"name"`
	Desc   string          `json:"desc"`
	Schema json.RawMessage `json:"schema"`
}

// popluateEntrys refresh token socpe all mcp handler router
func (entry *entryHandler) popluateEntrys() {
	router := mux.NewRouter()
	var mcps sync.Map
	entry.entrys.Range(func(_, value interface{}) bool {
		cfg := value.(mcpConfig)
		c, _ := mcps.LoadOrStore(cfg.fn, server.NewMCPServer(cfg.fn, cfg.ns))
		mcpserver := c.(*server.MCPServer)
		for idx, item := range cfg.Tools {
			if item.Name == "" {
				klog.Warningf("%s index %d tool name is empty", cfg.key, idx)
				continue
			}
			tool := mcp.NewToolWithRawSchema(item.Name, item.Desc, item.Schema)
			mcpserver.AddTool(tool, createMCPHandler(entry.rcs, "tool", item.Name, cfg.ns, cfg.fn))
		}
		return true
	})
	mcps.Range(func(fn, value any) bool {
		entryPath, mcpserver := fmt.Sprintf("%s/%s", entry.basePath, fn), value.(*server.MCPServer)
		sseServer := server.NewSSEServer(mcpserver, server.WithBasePath(entryPath))
		router.PathPrefix(entryPath).HandlerFunc(sseServer.ServeHTTP)
		return true
	})
	klog.Infof("update %s mcp servers", entry.basePath)
	entry.router.UpdateRouter(router)
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
		klog.Errorf("mcp entry %s not found", mcpKey)
		return
	}
	mcpEntry := c.(*entryHandler)
	mcpEntry.entrys.Store(config.key, config)
	klog.Infof("update %s mcp handler", config.key)
	mcpEntry.popluateEntrys()
}

func (rcs *RefuncMCPServer) handleTriggerDelete(obj interface{}) {
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
		klog.Errorf("mcp entry %s not found", mcpKey)
		return
	}
	mcpEntry := c.(*entryHandler)
	mcpEntry.entrys.Delete(config.key)
	klog.Infof("delete %s mcp handler", config.key)
	mcpEntry.popluateEntrys()
	return
}

func triggerForToolConfig(trigger *rfv1beta3.Trigger) (string, mcpConfig, error) {
	var config mcpConfig
	if err := json.Unmarshal(trigger.Spec.Common.Args, &config); err != nil {
		klog.Errorf("unmarshal %s/%s tool config error %v", trigger.Namespace, trigger.Name, err)
		return "", config, err
	}
	config.key, config.ns, config.fn = mcpEntryKey(trigger), trigger.Namespace, trigger.Spec.FuncName
	mcpKey := fmt.Sprintf("%s/%s", trigger.Namespace, config.Token)
	return mcpKey, config, nil
}

func mcpEntryKey(trigger *rfv1beta3.Trigger) string {
	return fmt.Sprintf("%s/%s/%s", trigger.Namespace, trigger.Name, trigger.Spec.FuncName)
}
