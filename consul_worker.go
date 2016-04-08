package main // import "github.com/jmccarty3/kube2consul"

import (
	"strconv"
	"strings"

	"github.com/golang/glog"

	consulapi "github.com/hashicorp/consul/api"
	kapi "k8s.io/kubernetes/pkg/api"
)

//ConsulWorker Interface for interacting with a Consul object
type ConsulWorker interface {
	AddDNS(baseID string, service *kapi.Service)
	RemoveDNS(baseID string)
	SyncDNS()
}

//ConsulAgentWorker ConsulWorker with a connection to a Consul agent
type ConsulAgentWorker struct {
	ConsulWorker

	ids   map[string][]*consulapi.AgentServiceRegistration
	agent *consulapi.Client
}

func isServiceNameValid(name string) bool {
	if strings.Contains(name, ".") == false {
		return true
	}

	glog.Infof("Names containing '.' are not supported: %s\n", name)
	return false
}

func isServiceValid(service *kapi.Service) bool {
	if isServiceNameValid(service.Name) {
		if kapi.IsServiceIPSet(service) {
			if service.Spec.Type == kapi.ServiceTypeNodePort {
				return true // Service is valid
			}
			//Currently this is only for NodePorts.
			glog.V(3).Infof("Skipping non-NodePort service: %s\n", service.Name)
		} else {
			// if ClusterIP is not set, do not create a DNS records
			glog.Infof("Skipping dns record for headless service: %s\n", service.Name)
		}
	}

	return false
}

func createAgentServiceCheck(config DNSInfo, port *kapi.ServicePort) *consulapi.AgentServiceCheck {
	glog.V(3).Info("Creating service check for: ", config.IPAddress, " on Port: ", port.NodePort)
	return &consulapi.AgentServiceCheck{
		TCP:      config.IPAddress + ":" + strconv.Itoa(port.NodePort),
		Interval: "60s",
	}
}

func createAgentServiceReg(config DNSInfo, name string, service *kapi.Service, port *kapi.ServicePort) *consulapi.AgentServiceRegistration {
	labels := []string{"Kube", string(port.Protocol)}
	asrID := config.BaseID + port.Name

	if name == "" {
		if len(port.Name) > 0 {
			name = service.Name + "-" + port.Name
		} else {
			name = service.Name + "-" + strconv.Itoa(port.Port)
		}
	}

	return &consulapi.AgentServiceRegistration{
		ID:      asrID,
		Name:    name,
		Address: config.IPAddress,
		Port:    port.NodePort,
		Tags:    labels,
	}
}

//NewConsulAgentWorker Creates a new ConsulAgentWorker connected to a client
func NewConsulAgentWorker(client *consulapi.Client) *ConsulAgentWorker {
	return &ConsulAgentWorker{
		agent: client,
		ids:   make(map[string][]*consulapi.AgentServiceRegistration),
	}
}

//AddDNS Adds the DNS information to consul
func (client *ConsulAgentWorker) AddDNS(config DNSInfo, service *kapi.Service) {
	glog.V(3).Info("Starting Add DNS for: ", config.BaseID)

	if config.IPAddress == "" || config.BaseID == "" {
		glog.Error("DNS Info is not valid for AddDNS")
	}

	//Validate Service
	if !isServiceValid(service) {
		return
	}
	//Check Port Count & Determine DNS Entry Name
	var serviceName string

	if len(service.Spec.Ports) == 1 {
		serviceName = service.Name
	} else {
		serviceName = ""
	}

	for _, port := range service.Spec.Ports {
		asr := createAgentServiceReg(config, serviceName, service, &port)

		if *argChecks && port.Protocol == "TCP" {
			//Create Check if neeeded
			asr.Check = createAgentServiceCheck(config, &port)
		}

		if client.agent != nil {
			//Registers with DNS
			if err := client.agent.Agent().ServiceRegister(asr); err != nil {
				glog.Error("Error creating service record: ", asr.ID)
			}
		}

		//Add to IDS
		client.ids[config.BaseID] = append(client.ids[config.BaseID], asr)
	}

	//Exit
}

//RemoveDNS Removes the DNS information requested from Consul
func (client *ConsulAgentWorker) RemoveDNS(config DNSInfo) {
	if ids, ok := client.ids[config.BaseID]; ok {
		for _, asr := range ids {
			if client.agent != nil {
				if err := client.agent.Agent().ServiceDeregister(asr.ID); err != nil {
					glog.Error("Error removing service: ", err)
				}
			}
		}
		delete(client.ids, config.BaseID)
	} else {
		glog.Error("Requested to remove non-existant BaseID DNS of:", config.BaseID)
	}
}

func containsServiceID(id string, services map[string]*consulapi.AgentService) bool {
	for _, service := range services {
		if service.ID == id {
			return true
		}
	}

	return false
}

//SyncDNS Verifies that all requested services are actually registered
func (client *ConsulAgentWorker) SyncDNS() {
	if client.agent != nil {
		if services, err := client.agent.Agent().Services(); err == nil {
			for _, registered := range client.ids {
				for _, service := range registered {
					if !containsServiceID(service.ID, services) {
						glog.Info("Regregistering missing service ID: ", service.ID)
						client.agent.Agent().ServiceRegister(service)
					}
				}
			}
		} else {
			glog.Info("Error retreiving services from consul during sync: ", err)
		}
	}
}

//RunConsulWorker Runs the ConsulWorker while the queue is open
func RunConsulWorker(queue <-chan ConsulWork, client *consulapi.Client) {
	worker := NewConsulAgentWorker(client)

	for work := range queue {
		glog.V(4).Info("Consol Work Action: ", work.Action, " BaseID:", work.Config.BaseID)

		switch work.Action {
		case ConsulWorkAddDNS:
			worker.AddDNS(work.Config, work.Service)
		case ConsulWorkRemoveDNS:
			worker.RemoveDNS(work.Config)
		case ConsulWorkSyncDNS:
			worker.SyncDNS()
		default:
			glog.Error("Unsupported Action of: ", work.Action)
		}

	}
}
