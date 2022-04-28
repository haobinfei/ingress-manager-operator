package pkg

import (
	"context"
	"reflect"
	"time"

	ing "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	machinery "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	svcInformer "k8s.io/client-go/informers/core/v1"
	netInformer "k8s.io/client-go/informers/networking/v1"
	"k8s.io/client-go/kubernetes"
	svcLister "k8s.io/client-go/listers/core/v1"
	netLister "k8s.io/client-go/listers/networking/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

const (
	workNum  = 10
	maxRetry = 10
)

type controller struct {
	client        kubernetes.Interface
	ingressLister netLister.IngressLister
	serviceLister svcLister.ServiceLister
	queue         workqueue.RateLimitingInterface
}

func (c *controller) Run(stopCh chan struct{}) {

	for i := 0; i < workNum; i++ {
		go wait.Until(c.worker, time.Minute, stopCh)
	}

	<-stopCh
}

func NewControlle(client kubernetes.Interface, serviceInformer svcInformer.ServiceInformer, ingressInformer netInformer.IngressInformer) controller {
	c := controller{
		client:        client,
		ingressLister: ingressInformer.Lister(),
		serviceLister: serviceInformer.Lister(),
		queue:         workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "ingressManager"),
	}

	serviceInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addService,
		UpdateFunc: c.updateService,
	})

	ingressInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		DeleteFunc: c.deleteIngress,
	})

	return c
}

func (c *controller) addService(newobj interface{}) {
	c.enqueue(newobj)
}

func (c *controller) updateService(oldObj, newObj interface{}) {
	if reflect.DeepEqual(oldObj, newObj) {
		return
	}
	c.enqueue(newObj)
}

func (c *controller) deleteIngress(obj interface{}) {
	ingress := obj.(*ing.Ingress)
	service := machinery.GetControllerOf(ingress)

	if service == nil {
		return
	}
	if service.Kind != "Service" {
		return
	}

	c.queue.Add(ingress.Namespace + "/" + ingress.Name)

}

func (c *controller) enqueue(obj interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		return
	}
	c.queue.Add(key)
}

func (c *controller) worker() {
	for c.processNextItem() {

	}
}

func (c *controller) processNextItem() bool {

	itme, shutdown := c.queue.Get()
	if shutdown {
		return false
	}
	key := itme.(string)

	err := c.syncService(key)
	if err != nil {
		c.handleError(key, err)
	}
	return true
}

func (c *controller) handleError(key string, err error) {
	if c.queue.NumRequeues(key) <= maxRetry {
		c.queue.AddRateLimited(key)
		return
	}
	runtime.HandleError(err)
	c.queue.Forget(key)
}

func (c *controller) syncService(key string) error {
	namespaceKey, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}

	service, err := c.serviceLister.Services(namespaceKey).Get(name)
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	_, ok := service.GetAnnotations()["ingress/http"]
	ingress, err := c.ingressLister.Ingresses(namespaceKey).Get(name)

	if ok && errors.IsNotFound(err) {
		ig := c.constructIngress(namespaceKey, name)
		_, err = c.client.NetworkingV1().Ingresses(namespaceKey).Create(context.TODO(), ig, machinery.CreateOptions{})
		if err != nil {
			return err
		}
	} else if !ok && ingress != nil {
		err = c.client.NetworkingV1().Ingresses(namespaceKey).Delete(context.TODO(), name, machinery.DeleteOptions{})
		if err != nil {
			return err
		}
	} else {
		return err
	}
	return nil
}

func (c *controller) constructIngress(namespaceKey, name string) *ing.Ingress {

	pathType := ing.PathTypeExact
	ingress := ing.Ingress{}
	ingress.Name = name
	ingress.Namespace = namespaceKey
	ingress.Spec = ing.IngressSpec{
		Rules: []ing.IngressRule{
			{
				Host: "",
				IngressRuleValue: ing.IngressRuleValue{
					HTTP: &ing.HTTPIngressRuleValue{
						Paths: []ing.HTTPIngressPath{
							{
								Path:     "/",
								PathType: &pathType,
								Backend: ing.IngressBackend{
									Service: &ing.IngressServiceBackend{
										Name: name,
										Port: ing.ServiceBackendPort{
											Name:   "http",
											Number: 80,
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	return &ingress
}
