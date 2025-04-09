package mcpserver

import (
	"fmt"

	"github.com/gorilla/mux"
	"github.com/mark3labs/mcp-go/server"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"
)

type httpHandler struct {
	ns    string
	name  string
	token string
	rcs   *RefuncMCPServer
}

func (t *httpHandler) setupHTTPEndpoints(router *mux.Router) {
	secret, err := t.rcs.secretLister.Secrets(t.ns).Get(t.name)
	if err != nil {
		klog.Errorf("get %s secret error %v", t.key(), err)
		return
	}
	// endpoint url
	endpoint := fmt.Sprintf("/%s/%s", t.ns, t.name)
	if t.token != "" {
		endpoint = fmt.Sprintf("%s/%s", endpoint, t.token)
	}
	// endpoint sse handler
	c, loaded := t.rcs.mcps.LoadOrStore(t.key(), server.NewMCPServer(
		t.key(),
		secret.GetResourceVersion(),
	))
	if !loaded {
		// rebuild mcp server with existed tools
		triggers, err := t.rcs.triggerLister.Triggers(t.ns).List(labels.Everything())
		if err != nil {
			t.rcs.mcps.Delete(t.key())
			klog.Errorf("rebuild mcp server for %s error %v", t.key(), err)
			return
		}
		for _, trigger := range triggers {
			if trigger.Spec.Type != MCPTriggerType {
				continue
			}
			mcpKey, _, err := triggerForToolConfig(trigger)
			if err != nil || mcpKey != t.key() {
				continue
			}
			t.rcs.handleTriggerChange(trigger)
		}
		klog.Infof("rebuild mcp server for %s as endpoint %s", t.key(), endpoint)
	}
	mcp := c.(*server.MCPServer)
	sseServer := server.NewSSEServer(mcp, server.WithBasePath(endpoint))
	// register handler
	router.PathPrefix(endpoint).HandlerFunc(sseServer.ServeHTTP)
}

func (t *httpHandler) key() string {
	return fmt.Sprintf("%s/%s", t.ns, t.name)
}

func (rcs *RefuncMCPServer) handleSecretChange(obj interface{}) {
	origin, ok := obj.(*corev1.Secret)
	if !ok {
		klog.Errorf("obj %v not is a secret", obj)
		return
	}
	secret := origin.DeepCopy()
	tokenBts, ok := secret.Data["token"]
	token := "" // default token is empty
	if ok {
		token = string(tokenBts)
	}
	key := k8sKey(secret)
	// on delete
	if !secret.DeletionTimestamp.IsZero() {
		if _, ok := rcs.endpoints.Load(key); ok {
			rcs.endpoints.Delete(key)
			rcs.mcps.Delete(key)
			rcs.popluateEndpoints()
		}
		return
	}
	// on upsert
	c, loaded := rcs.endpoints.LoadOrStore(key, &httpHandler{
		ns:    secret.Namespace,
		name:  secret.Name,
		token: token,
		rcs:   rcs,
	})
	if !loaded {
		rcs.popluateEndpoints()
	} else {
		current := c.(*httpHandler)
		if current.token != token {
			rcs.endpoints.Store(key, &httpHandler{
				ns:    secret.Namespace,
				name:  secret.Name,
				token: token,
				rcs:   rcs,
			})
			rcs.popluateEndpoints()
		}
	}
	return
}

func (rcs *RefuncMCPServer) popluateEndpoints() {
	router := mux.NewRouter()
	rcs.endpoints.Range(func(_, value interface{}) bool {
		value.(*httpHandler).setupHTTPEndpoints(router)
		return true
	})
	rcs.router.UpdateRouter(router)
	klog.Infof("update router endpoints")
}

func k8sKey(o metav1.Object) string {
	return o.GetNamespace() + "/" + o.GetName()
}
