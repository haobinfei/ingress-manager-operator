package main

import (
	"log"

	"github.com/haobinfei/ingress-manager-operator/pkg"

	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {
	// config
	config, err := clientcmd.BuildConfigFromFlags("", clientcmd.RecommendedHomeFile)

	if err != nil {
		inClusterConfig, err := rest.InClusterConfig()
		if err != nil {
			log.Fatalln("cat not get config")
		}
		config = inClusterConfig
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalln("cat not create client")
	}

	sif := informers.NewSharedInformerFactory(clientset, 0)
	serviceInformer := sif.Core().V1().Services()
	ingressInformer := sif.Networking().V1().Ingresses()

	controller := pkg.NewControlle(clientset, serviceInformer, ingressInformer)

	stopCh := make(chan struct{})
	sif.Start(stopCh)
	sif.WaitForCacheSync(stopCh)

	controller.Run(stopCh)

}
