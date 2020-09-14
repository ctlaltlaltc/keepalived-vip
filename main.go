/*
Copyright 2015 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"flag"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/golang/glog"
	"github.com/spf13/pflag"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

var (
	flags = pflag.NewFlagSet("", pflag.ContinueOnError)

	apiserverHost = flags.String("apiserver-host", "", `The address of the Kubernetes Apiserver
		to connect to in the format of protocol://address:port, e.g.,
		http://localhost:8080. If not specified, the assumption is that the binary runs inside a
		Kubernetes cluster and local discovery is attempted.`)

	kubeConfigFile = flags.String("kubeconfig", "", `Path to kubeconfig file with authorization
        and master location information.`)

	watchAllNamespaces = flags.Bool("watch-all-namespaces", false, `If true, watch for services in all the namespaces`)

	useUnicast = flags.Bool("use-unicast", false, `use unicast instead of multicast for communication
		with other keepalived instances`)

	vrrpVersion = flags.Int("vrrp-version", 3, `Which VRRP version to use (2 or 3)`)

	configMapName = flags.String("services-configmap", "",
		`Name of the ConfigMap that contains the definition of the services to expose.
		The key in the map indicates the external IP to use. The value is the name of the
		service with the format namespace/serviceName and the port of the service could be a number or the
		name of the port.`)

	// sysctl changes required by keepalived
	sysctlAdjustments = map[string]int{
		// allows processes to bind() to non-local IP addresses
		"net/ipv4/ip_nonlocal_bind": 1,
		// enable connection tracking for LVS connections
		"net/ipv4/vs/conntrack": 1,
	}

	vrid = flags.Int("vrid", 50,
		`The keepalived VRID (Virtual Router Identifier, between 0 and 255 as per
			RFC-5798), which must be different for every Virtual Router (ie. every
			keepalived sets) running on the same network.`)
)

func main() {
	var clientConfig *rest.Config

	flags.AddGoFlagSet(flag.CommandLine)
	flags.Parse(os.Args)

	var err error
	var kubeClient  *kubernetes.Clientset

	if *configMapName == "" {
		glog.Fatalf("Please specify --services-configmap")
	}
	if *apiserverHost != "" && *kubeConfigFile == "" {
		glog.Fatalf("Please specify --kubeconfig")
	}
	clientConfig, err = clientcmd.BuildConfigFromFlags(*apiserverHost, *kubeConfigFile)
	if err != nil {
		glog.Fatalf("unexpected error: %v", err)
	}
	if kubeClient, err = kubernetes.NewForConfig(clientConfig); err != nil {
		glog.Fatalf("Failed to create client: %v", err)
	}
	namespace := getCtlRunNamespace()
	if err != nil {
		glog.Fatalf("unexpected error: %v", err)
	}

	if *watchAllNamespaces {
		namespace = metav1.NamespaceAll
		glog.Info("watching all namespaces")
	} else {
		glog.Infof("watching namespace: '%v'", namespace)
	}

	err = loadIPVModule()
	if err != nil {
		glog.Fatalf("unexpected error: %v", err)
	}

	err = changeSysctl()
	if err != nil {
		glog.Fatalf("unexpected error: %v", err)
	}

	err = resetIPVS()
	if err != nil {
		glog.Fatalf("unexpected error: %v", err)
	}

	glog.Info("starting LVS configuration")
	if *useUnicast {
		glog.Info("keepalived will use unicast to sync the nodes")
	}
	ipvsc := newIPVSController(kubeClient, namespace, *useUnicast, *configMapName, *vrid, *vrrpVersion)
	go ipvsc.serviceInformer.Run(wait.NeverStop)
	go ipvsc.endpointInformer.Run(wait.NeverStop)

	if !cache.WaitForCacheSync(wait.NeverStop, ipvsc.serviceInformer.HasSynced, ipvsc.endpointInformer.HasSynced) {
		return
	}

	go ipvsc.syncQueue.run(time.Second, ipvsc.stopCh)

	go handleSigterm(ipvsc)

	glog.Info("starting keepalived to announce VIPs")
	ipvsc.keepalived.Start()
}

func handleSigterm(ipvsc *ipvsControllerController) {
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGTERM)
	<-signalChan
	glog.Infof("Received SIGTERM, shutting down")

	exitCode := 0
	if err := ipvsc.Stop(); err != nil {
		glog.Infof("Error during shutdown %v", err)
		exitCode = 1
	}

	glog.Infof("Exiting with %v", exitCode)
	os.Exit(exitCode)
}
