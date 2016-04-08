package main // import "github.com/jmccarty3/kube2consul"

import (
	//"k8s.io/kubernetes/pkg/api"
	kapi "k8s.io/kubernetes/pkg/api"
)

//KubeWorkAction string
type KubeWorkAction string

const (
	//KubeWorkAddNode add a node to kuberentes
	KubeWorkAddNode KubeWorkAction = "AddNode"
	//KubeWorkRemoveNode remove a node from kubernetes
	KubeWorkRemoveNode KubeWorkAction = "RemoveNode"
	//KubeWorkUpdateNode update a nodes status
	KubeWorkUpdateNode KubeWorkAction = "UpdateNode"
	//KubeWorkAddService add a service to kubernetes
	KubeWorkAddService KubeWorkAction = "AddService"
	//KubeWorkRemoveService remove a service from kuberentes
	KubeWorkRemoveService KubeWorkAction = "RemoveService"
	//KubeWorkUpdateService update a service representation
	KubeWorkUpdateService KubeWorkAction = "UpdateService"
	//KubeWorkSync syncronize current kubernetes entries
	KubeWorkSync KubeWorkAction = "Sync"
)

//KubeWork Unit of work to be done by the Kubernetes Workers
//TODO: Consider just taking the api.Node Object
type KubeWork struct {
	Action  KubeWorkAction
	Node    *kapi.Node
	Service *kapi.Service
}

//ConsulWorkAction String
type ConsulWorkAction string

const (
	//ConsulWorkAddDNS Add DNS Entry
	ConsulWorkAddDNS ConsulWorkAction = "AddDNS"
	//ConsulWorkRemoveDNS Remove a DNS entry
	ConsulWorkRemoveDNS ConsulWorkAction = "RemoveDNS"
	//ConsulWorkSyncDNS consul should syncronzie its service entries
	ConsulWorkSyncDNS ConsulWorkAction = "SyncDNS"
)

//DNSInfo Information to use for DNS actions
type DNSInfo struct {
	BaseID    string
	IPAddress string
}

//ConsulWork Work to be done by the ConsulWorkers
type ConsulWork struct {
	Action  ConsulWorkAction
	Service *kapi.Service
	Config  DNSInfo
}
