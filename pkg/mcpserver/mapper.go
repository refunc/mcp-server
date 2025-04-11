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
	ns       string
	basePath string
	router   *mmux.MutableRouter
	configs  sync.Map
	mcps     sync.Map
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

// refresh token socpe all mcp handler router
func (entry *entryHandler) popluateConfigs() {
	router := mux.NewRouter()
	tools := map[string][]server.ServerTool{}
	entry.configs.Range(func(_, value interface{}) bool {
		cfg := value.(mcpConfig)
		for idx, item := range cfg.Tools {
			if item.Name == "" {
				klog.Warningf("%s index %d tool name is empty", cfg.key, idx)
				continue
			}
			if _, ok := tools[cfg.fn]; !ok {
				tools[cfg.fn] = []server.ServerTool{}
			}
			tool := server.ServerTool{
				Tool:    mcp.NewToolWithRawSchema(item.Name, item.Desc, item.Schema),
				Handler: createMCPHandler(entry.rcs, "tool", item.Name, cfg.ns, cfg.fn),
			}
			tools[cfg.fn] = append(tools[cfg.fn], tool)
		}
		return true
	})
	for fn, items := range tools {
		c, _ := entry.mcps.LoadOrStore(fn, server.NewMCPServer(fn, entry.ns)) //reuse mcpserver to send notify for clients
		mcpserver := c.(*server.MCPServer)
		mcpserver.SetTools(items...)
		fnPath := fmt.Sprintf("%s/%s", entry.basePath, fn)
		sseServer := server.NewSSEServer(mcpserver, server.WithBasePath(fnPath))
		router.PathPrefix(fnPath).HandlerFunc(sseServer.ServeHTTP)
	}
	gcfns := []string{}
	entry.mcps.Range(func(key, _ any) bool {
		fnKey := key.(string)
		if _, ok := tools[fnKey]; !ok {
			gcfns = append(gcfns, fnKey)
		}
		return true
	})
	for _, fn := range gcfns {
		entry.mcps.Delete(fn)
		klog.Infof("delete func %s/%s mcp server", entry.ns, fn)
	}
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
	mcpEntry.configs.Store(config.key, config)
	klog.Infof("update %s mcp handler", config.key)
	mcpEntry.popluateConfigs()
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
	mcpEntry.configs.Delete(config.key)
	klog.Infof("delete %s mcp handler", config.key)
	mcpEntry.popluateConfigs()
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
