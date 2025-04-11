package mcpserver

import (
	"fmt"

	"github.com/gorilla/mux"
	"github.com/refunc/refunc/pkg/operators/triggers/httptrigger/mmux"
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
	_, err := t.rcs.secretLister.Secrets(t.ns).Get(t.name)
	if err != nil {
		klog.Errorf("get %s secret error %v", t.key(), err)
		return
	}
	// endpoint url /ns/secret-name/token/func-name
	endpoint := fmt.Sprintf("/%s/%s", t.ns, t.name)
	if t.token != "" {
		endpoint = fmt.Sprintf("%s/%s", endpoint, t.token)
	}
	c, loaded := t.rcs.mcps.LoadOrStore(t.key(), &entryHandler{
		ns:       t.ns,
		basePath: endpoint,
		router:   mmux.NewMutableRouter(),
		rcs:      t.rcs,
	})
	if !loaded {
		// rebuild mcp entry with existed triggers
		triggers, err := t.rcs.triggerLister.Triggers(t.ns).List(labels.Everything())
		if err != nil {
			t.rcs.mcps.Delete(t.key())
			klog.Errorf("rebuild mcp entry for %s error %v", t.key(), err)
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
		klog.Infof("rebuild mcp entry for %s as endpoint %s", t.key(), endpoint)
	}
	entry := c.(*entryHandler)
	// register handler
	router.PathPrefix(endpoint).HandlerFunc(entry.router.ServeHTTP)
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
	// on upsert
	c, loaded := rcs.endpoints.LoadOrStore(key, &httpHandler{
		ns:    secret.Namespace,
		name:  secret.Name,
		token: token,
		rcs:   rcs,
	})
	if !loaded {
		rcs.popluateEndpoints(fmt.Sprintf("add %s secret", key))
	} else {
		current := c.(*httpHandler)
		if current.token != token {
			rcs.endpoints.Store(key, &httpHandler{
				ns:    secret.Namespace,
				name:  secret.Name,
				token: token,
				rcs:   rcs,
			})
			rcs.popluateEndpoints(fmt.Sprintf("update %s secret", key))
		}
	}
	return
}

func (rcs *RefuncMCPServer) handleSecretDelete(obj interface{}) {
	origin, ok := obj.(*corev1.Secret)
	if !ok {
		klog.Errorf("obj %v not is a secret", obj)
		return
	}
	secret := origin.DeepCopy()
	key := k8sKey(secret)
	// on delete
	if _, ok := rcs.endpoints.Load(key); ok {
		rcs.endpoints.Delete(key)
		rcs.popluateEndpoints(fmt.Sprintf("delete %s secret", key))
	}
	if _, ok := rcs.mcps.Load(key); ok {
		rcs.mcps.Delete(key)
	}
	klog.Infof("delete mcp entry for %s", key)
	return
}

func (rcs *RefuncMCPServer) popluateEndpoints(event string) {
	router := mux.NewRouter()
	rcs.endpoints.Range(func(_, value interface{}) bool {
		value.(*httpHandler).setupHTTPEndpoints(router)
		return true
	})
	rcs.router.UpdateRouter(router)
	klog.Infof("update router endpoints on %s", event)
}

func k8sKey(o metav1.Object) string {
	return o.GetNamespace() + "/" + o.GetName()
}
