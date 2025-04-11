package mcpserver

import (
	"fmt"
	"net/http"
	"os"
	"sync"

	"github.com/gorilla/handlers"
	"github.com/nats-io/nats.go"
	"github.com/refunc/refunc/pkg/env"
	rfinformers "github.com/refunc/refunc/pkg/generated/informers/externalversions"
	rfv1beta3 "github.com/refunc/refunc/pkg/generated/listers/refunc/v1beta3"
	"github.com/refunc/refunc/pkg/operators/triggers/httptrigger/mmux"
	"github.com/refunc/refunc/pkg/utils/cmdutil/sharedcfg"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sinformers "k8s.io/client-go/informers"
	k8sv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

type RefuncMCPServer struct {
	addr                  string
	refuncInformerFactory rfinformers.SharedInformerFactory
	secretInformerFactory k8sinformers.SharedInformerFactory
	triggerLister         rfv1beta3.TriggerLister
	funcdefLister         rfv1beta3.FuncdefLister
	secretLister          k8sv1.SecretLister
	router                *mmux.MutableRouter
	endpoints             sync.Map
	mcps                  sync.Map
	natsConn              *nats.Conn
}

const (
	MCPSecretLabel     = "mcp.refunc.io/secret-type"
	MCPSecretLabelType = "token"
	MCPTriggerType     = "mcp"
)

func NewRefuncMCPServer(sc sharedcfg.Configs, addr string, stopC <-chan struct{}) (*RefuncMCPServer, error) {
	// init informer
	refuncInformerFactory := sc.RefuncInformers()
	triggerLister := refuncInformerFactory.Refunc().V1beta3().Triggers().Lister()
	funcdefLister := refuncInformerFactory.Refunc().V1beta3().Funcdeves().Lister()
	labelOptions := k8sinformers.WithTweakListOptions(func(opts *metav1.ListOptions) {
		opts.LabelSelector = fmt.Sprintf("%s=%s", MCPSecretLabel, MCPSecretLabelType)
	})
	secretInformerFactory := k8sinformers.NewSharedInformerFactoryWithOptions(sc.KubeClient(), sharedcfg.FullyResyncPeriod,
		k8sinformers.WithNamespace(sc.Namespace()), labelOptions)
	secretLister := secretInformerFactory.Core().V1().Secrets().Lister()

	//connect to nats
	hostname, err := os.Hostname()
	if err != nil {
		klog.Fatalf("get hostname error %v", err)
	}
	natsConn, err := env.NewNatsConn(nats.Name("mcp-server/" + hostname))
	if err != nil {
		klog.Fatalf("connect to nats error %v", err)
	}

	return &RefuncMCPServer{
		addr:                  addr,
		secretInformerFactory: secretInformerFactory,
		refuncInformerFactory: refuncInformerFactory,
		funcdefLister:         funcdefLister,
		triggerLister:         triggerLister,
		secretLister:          secretLister,
		natsConn:              natsConn,
		router:                mmux.NewMutableRouter(),
	}, nil
}

func (rcs *RefuncMCPServer) Run(stopC <-chan struct{}) {
	wantedInformers := []cache.InformerSynced{
		rcs.refuncInformerFactory.Refunc().V1beta3().Triggers().Informer().HasSynced,
		rcs.refuncInformerFactory.Refunc().V1beta3().Funcdeves().Informer().HasSynced,
		rcs.secretInformerFactory.Core().V1().Secrets().Informer().HasSynced,
	}

	updateHandler := func(fn func(interface{})) func(o, c interface{}) {
		return func(oldObj, curObj interface{}) {
			old, _ := meta.Accessor(oldObj)
			cur, _ := meta.Accessor(curObj)
			if old.GetResourceVersion() == cur.GetResourceVersion() {
				return
			}
			fn(curObj)
		}
	}

	rcs.secretInformerFactory.Core().V1().Secrets().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    rcs.handleSecretChange,
		UpdateFunc: updateHandler(rcs.handleSecretChange),
		DeleteFunc: rcs.handleSecretDelete,
	})

	rcs.refuncInformerFactory.Refunc().V1beta3().Triggers().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    rcs.handleTriggerChange,
		UpdateFunc: updateHandler(rcs.handleTriggerChange),
		DeleteFunc: rcs.handleTriggerDelete,
	})

	go rcs.secretInformerFactory.Start(stopC) //self managed informer manual start
	go func() {
		if !cache.WaitForCacheSync(stopC, wantedInformers...) {
			klog.Fatalln("Fail wait for cache sync")
		}
		klog.Infoln("success sync informer cache")
	}()

	klog.Infof("Listen and serving on %s\n", rcs.addr)
	if err := rcs.listenAndServe(); err != nil {
		klog.Fatalf("mcp server exit with error %v", err)
	}
}

func (rcs *RefuncMCPServer) listenAndServe() error {
	var handler http.Handler = rcs.router

	// logging
	handler = handlers.LoggingHandler(GlogWriter{}, handler)

	// handle proxy
	handler = handlers.ProxyHeaders(handler)

	server := &http.Server{
		Addr:    rcs.addr,
		Handler: handler,
	}
	return server.ListenAndServe()
}
